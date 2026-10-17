# App Android

Kotlin + Jetpack Compose, package `app.intercom.android`, minSdk 29 (Android 10: serve per l'encoder Opus
di `MediaCodec`), target 36, versione `0.1.0`. Unica dipendenza di rete: OkHttp. Nessuna libreria nativa.

Sorgenti in `android/app/src/main/java/app/intercom/android/`.

## File

| File | Ruolo |
|---|---|
| `IntercomApp.kt` | `Application`: contiene l'unico `IntercomController` |
| `MainActivity.kt` | Permessi (microfono, notifiche), avvio e arresto della discovery, connessione automatica, tasti volume sul flusso voce |
| `IntercomController.kt` | **Cuore dell'app**: possiede connessione e audio, espone `UiState` come `StateFlow`, riceve le azioni della UI |
| `IntercomService.kt` | Servizio in primo piano (microphone + mediaPlayback) con notifica, wake lock e Wi-Fi lock: l'audio continua a schermo spento |
| `Prefs.kt` | Impostazioni in `SharedPreferences`; volumi per chiave `host:utente:canale` |
| `SecretBox.kt` | Cifra la password salvata con una chiave AES-GCM nell'Android Keystore |
| `Discovery.kt` | Ricerca dei server via `NsdManager` (`_intercom._tcp`) |
| `net/Connection.kt` | WebSocket + UDP, ping, keepalive, riconnessione, timeout |
| `proto/` | Messaggi JSON (`Messages.kt`) e pacchetti UDP (`Packets.kt`) |
| `audio/AudioEngine.kt` | Thread di cattura e di riproduzione, scelta dell'uscita audio |
| `audio/OpusCodec.kt` | Encoder e decoder Opus tramite `MediaCodec` |
| `audio/JitterBuffer.kt` | Riordino per singola voce, avvio dopo 60 ms, frame mancante → silenzio |
| `audio/Vox.kt` | Livello RMS in dBFS, soglia (predefinita −45 dB), tenuta di 800 ms (40 frame) |
| `audio/Mixer.kt` | Somma delle voci con guadagno e saturazione a 16 bit |
| `talk/TalkButton.kt` | Gesti del pulsante PARLA: pressione = HOLD, doppio tocco = LATCHED, tocco = sgancia |
| `talk/Transmit.kt` | `chooseTarget` (cosa trasmettere a ogni frame) e `TxDecider` (quando inviare `ptt`) |
| `talk/TalkingTracker.kt` | Chi parla su quale canale, avviso per le chiamate private in arrivo |
| `ui/` | Compose: `App`, `ConnectScreen`, `MainScreen`, `SettingsScreen`, `Sheets` (mixer, utenti), `Theme` |

## Thread

- **UI (main)**: Compose e `IntercomController`. Le callback della connessione vengono riportate qui con `Handler.post`.
- **audio-in**: ogni 20 ms legge il microfono, applica il VOX, chiama `onFrame()`. Se il frame va in onda,
  lo codifica e lo invia via UDP.
- **audio-out**: ogni 20 ms legge un frame dal jitter buffer di ogni voce, decodifica, mixa e scrive su `AudioTrack`.
  Il ritmo lo dà la scrittura bloccante.
- **udp-rx** e thread di OkHttp: ricezione di rete.
- **timer** di `Connection`: ping ogni 3 s, keepalive UDP ogni 5 s, controllo del silenzio, timeout del primo `welcome`.

`IntercomController.lock` protegge `TalkButton` e `TxDecider`, usati sia dal thread UI sia da audio-in.
`connGen` scarta le callback di una connessione già chiusa.

## Decisione di trasmissione

`chooseTarget` viene chiamata a ogni frame, in ordine di priorità:
1. chiamata a tutti tenuta premuta → `all`;
2. utente tenuto premuto nel foglio Utenti → `user`;
3. nessun canale Parla attivo → niente;
4. PARLA tenuto premuto → `channels`;
5. PARLA agganciato e (microfono sempre aperto **oppure** voce rilevata dal VOX) → `channels`.

`TxDecider` confronta il target con quello del frame precedente e genera i messaggi `ptt` di inizio e fine.
Così gli altri vedono "sta parlando" solo quando c'è davvero audio.

## Connessione

- Stati: `Connecting` → `Online` → `Reconnecting(n)` → `Online`, oppure `Failed(message)` come stato finale.
- Riconnessione con attesa di 1, 2, 4, poi 5 s. Avviene solo se la connessione era già riuscita almeno una volta;
  se il primo tentativo fallisce, si torna alla schermata di connessione con l'errore.
- Errore definitivo con credenziali errate, utente disabilitato o espulsione. Con `bad_credentials` la password salvata viene cancellata.
- Alla riconnessione i canali Parla e Ascolta vengono ripristinati, ma il microfono agganciato resta **chiuso**.
- Logout esplicito: cancella la password salvata, così al prossimo avvio non c'è il login automatico.

## Audio

- Cattura: `AudioRecord` con sorgente `VOICE_COMMUNICATION`, più `AcousticEchoCanceler` e `NoiseSuppressor` se presenti.
- Riproduzione: `AudioTrack` a bassa latenza, `USAGE_VOICE_COMMUNICATION`, `MODE_IN_COMMUNICATION`.
- Una voce senza pacchetti per 500 ms viene tolta dal mix. Se il decoder va in errore, viene ricreato.
- Volume: generale × canale. Chiamate a tutti e private usano solo il volume generale.
- Uscita: `auto` (cuffia o Bluetooth se presenti, altrimenti altoparlante), `headset`, `speaker`.
  Su Android 10-11 si commuta solo l'altoparlante.
- Senza permesso microfono, o se la cattura si interrompe, l'app funziona in **solo ascolto**.
  Al ritorno in primo piano (`onResume`) riprova.

## Interfaccia

- **Connessione**: server trovati via mDNS, IP e porta manuali, credenziali, "ricorda credenziali", pulsante Annulla durante il collegamento.
- **Principale**: intestazione con stato e logout, lista dei canali, uno per riga (interruttori Parla e Ascolta, chi sta parlando),
  pulsante PARLA, barra inferiore con chiamata a tutti, lucchetto, mixer, utenti e impostazioni.
- **Utenti**: stato di ciascun utente; con `privateCall` ogni riga ha un tasto PARLA da tenere premuto per la chiamata privata (attivo solo se l'utente è online).
- **Lucchetto**: blocca gli interruttori dei canali e la barra; PARLA resta attivo.
- **Impostazioni**: modalità del doppio tocco (VOX o sempre aperto), soglia VOX, uscita audio, schermo sempre acceso.
- Tema scuro ad alto contrasto, grafica originale: non copiare layout, colori o asset di Unity Intercom.

## Test

- `app/src/test/` (JVM, `./gradlew test`): JitterBuffer, Mixer, Vox, Connection (con MockWebServer), Messages,
  Packets, TalkButton, TalkingTracker, Transmit.
- `app/src/androidTest/` (dispositivo, `./gradlew connectedAndroidTest`): OpusCodec, SecretBox.
