# Intercom — Tappa 1: nucleo intercom (server + Android)

Data: 2026-10-17
Stato: in revisione

## Obiettivo

Sistema intercom proprietario per rete locale, con funzionalità equivalenti a Unity Intercom
ma codice e grafica originali. La tappa 1 consegna:

- un **server** multipiattaforma (macOS, Windows, Linux; anche in Docker su Linux) con pagina web di configurazione;
- un'**app Android** client.

Successo = due o più telefoni Android collegati al server parlano e ascoltano su canali,
con chiamata a tutti, chiamate private, VOX, mixer e lucchetto funzionanti in rete locale.

## Roadmap complessiva (per contesto)

| Tappa | Contenuto | Stato |
|---|---|---|
| 1 | Nucleo intercom (questo documento) | in corso |
| 2 | Gruppi di canali | futuro |
| 3 | Program feed (L/R cuffia), I/O verso scheda audio del server (mix lato server) | futuro |
| 4 | Tally e GPIO | **escluso** |
| 5 | Cifratura AES-256-GCM, backup configurazione | futuro |
| 6 | App iPhone (Swift) | futuro |

Fuori dalla tappa 1: tutto quanto sopra, cifratura del traffico (la rete locale è considerata fidata
fino alla tappa 5), licenze/acquisti, aggiornamenti automatici.

## Struttura del repository

```
server/    server Go (binario unico, pagina web inclusa via embed)
android/   app Kotlin + Jetpack Compose
docs/      specifiche e piani
```

## 1. Architettura

### Server (Go)

Binario unico, Go puro (nessuna dipendenza cgo), cross-compilabile per macOS/Windows/Linux.

Componenti:

- **HTTP** (porta predefinita `8080`): pagina web admin, API REST admin (`/api/...`), WebSocket di controllo per i client (`/ws`).
- **UDP audio** (porta predefinita `8081`): ricezione e inoltro pacchetti voce.
- **Store**: configurazione in un file JSON (`intercom-config.json` accanto al binario o percorso da flag `-config`). Scrittura atomica (file temporaneo + rename).
- **Router audio**: decide a chi inoltrare ogni pacchetto. **Non decodifica né mixa** l'audio.
- **Discovery**: annuncio mDNS/DNS-SD del servizio `_intercom._tcp` con TXT `udp=<porta>`, `name=<nome server>`.

Password utenti e admin salvate con bcrypt.

### Flusso audio

- Formato: Opus, 48 kHz, mono, frame da 20 ms, ~32 kbit/s.
- Il server **inoltra** (non mixa): ogni pacchetto di chi parla viene inviato a ciascun ascoltatore idoneo, escluso chi parla.
- Ogni telefono mixa localmente le voci ricevute e applica i volumi del proprio mixer.
- Il mix lato server verrà introdotto solo nella tappa 3 per l'I/O audio esterno.

**Destinatari di un pacchetto** in base al target corrente di chi parla:

| Target | Chi riceve |
|---|---|
| `channels` | utenti online che hanno almeno uno dei canali "parla" attivi di chi parla tra i propri canali in "ascolta" (e permesso di ascolto su quel canale) |
| `all` (chiamata a tutti) | tutti gli utenti online, indipendentemente dai canali |
| `user` (chiamata privata) | solo l'utente indicato, indipendentemente dai canali |

Deduplica: un ascoltatore riceve **una sola copia** di ogni pacchetto. Se lo ascolta su più canali,
il pacchetto è etichettato con il primo canale corrispondente nell'ordine dei canali dell'ascoltatore.

Il server accetta audio da un utente solo se il target è consentito dai suoi permessi
(canale con permesso "parla", `allCall`, `privateCall`); altrimenti scarta.

## 2. Protocollo

### Controllo: WebSocket `/ws`, messaggi JSON `{ "type": ..., ... }`

Client → server:

| type | campi | note |
|---|---|---|
| `hello` | `username`, `password`, `device` | primo messaggio obbligatorio |
| `state` | `talk: [channelId]`, `listen: [channelId]` | canali attivi per parlare/ascoltare |
| `ptt` | `active: bool`, `target: {kind: "channels"\|"all"\|"user", userId?}` | inizio/fine trasmissione |
| `ping` | — | ogni 3 s |

Server → client:

| type | campi | note |
|---|---|---|
| `welcome` | `userId`, `session` (uint32), `udpPort`, `serverName`, `channels`, `perms` | dopo login riuscito |
| `config` | `channels`, `perms` | inviato quando l'admin modifica l'utente o i canali |
| `users` | `[{id, name, online, talking}]` | inviato a ogni cambiamento |
| `talking` | `userId`, `name`, `active`, `target`, `channelIds` | chi sta parlando e dove |
| `kicked` | `reason` (`"replaced"`, `"admin"`, `"disabled"`) | poi il server chiude |
| `error` | `code` (`"bad_credentials"`, `"disabled"`), `message` | poi il server chiude |
| `pong` | — | |

`channels` = `[{id, name, color, canTalk, canListen}]` nell'ordine assegnato all'utente.
`perms` = `{allCall: bool, privateCall: bool}`.

Login duplicato: vince l'ultimo; la sessione precedente riceve `kicked{reason:"replaced"}`.
Un client senza ping/pacchetti per 10 s è considerato offline.

### Audio: UDP, interi big-endian

Client → server:

```
u8  kind      1 = voce, 2 = keepalive
u32 session   dal messaggio welcome
u16 seq
... payload Opus (solo kind=1)
```

Il server associa l'indirizzo UDP del client alla sessione dal primo pacchetto valido
(il client invia un keepalive subito dopo `welcome` e poi ogni 5 s).

Server → client:

```
u8  kind      1 = voce
u16 talkerId
u16 channelId 0 = chiamata a tutti, 0xFFFF = chiamata privata, altrimenti id canale
u16 seq       sequenza originale di chi parla
... payload Opus
```

## 3. App Android

- Kotlin, Jetpack Compose, minSdk 29 (Android 10: necessario per l'encoder Opus di `MediaCodec`).
- Codec: `MediaCodec` Opus (encoder e decoder di sistema), nessuna libreria nativa.
- Cattura: `AudioRecord` con sorgente `VOICE_COMMUNICATION` + `AcousticEchoCanceler` / `NoiseSuppressor` se disponibili.
- Riproduzione: `AudioTrack` a bassa latenza, mono 48 kHz; mix locale in PCM a 16 bit con saturazione.
- **Servizio in primo piano** (tipo `microphone`) con notifica fissa: l'audio continua a schermo spento.
- Discovery: `NsdManager` per `_intercom._tcp`.
- Grafica originale: tema scuro ad alto contrasto. Non riprodurre layout, colori o asset di Unity Intercom.

### Schermata di connessione

- Lista dei server trovati via mDNS + campo IP/porta manuale.
- Nome utente, password, "ricorda credenziali" (salvate in `EncryptedSharedPreferences`).
- Riconnessione automatica all'avvio se le credenziali sono salvate.

### Schermata principale

- Intestazione: nome utente, pallino di stato (verde connesso, giallo riconnessione, rosso errore), logout, orologio.
- Griglia dei canali assegnati (numero libero, scorrevole). Ogni scheda:
  - nome e colore del canale;
  - interruttore **Parla** (nascosto/disabilitato se `canTalk=false`);
  - interruttore **Ascolta** (nascosto/disabilitato se `canListen=false`);
  - indicatore attività con il nome di chi sta parlando su quel canale.
- Pulsante **PARLA** grande:
  - pressione mantenuta → trasmette finché premuto;
  - doppio tocco → aggancia la modalità scelta nelle impostazioni: **VOX** (predefinito) o **sempre aperto**; il pulsante cambia colore;
  - tocco singolo mentre agganciato → sgancia.
  - Trasmette sui canali con Parla attivo; se nessuno è attivo, il pulsante è disabilitato.
- Barra inferiore: Chiamata a tutti · Lucchetto · Mixer · Utenti · Impostazioni.

### Funzioni

- **Chiamata a tutti**: visibile solo con `perms.allCall`; pressione mantenuta → `ptt` con target `all`.
- **Lucchetto**: blocca interruttori canali e barra; il pulsante PARLA resta attivo; sblocco con pressione lunga sul lucchetto.
- **Mixer**: cursore per ogni canale + volume generale; salvato sul telefono per server+utente. Chiamate a tutti e private usano il volume generale. I tasti volume fisici regolano il volume del flusso voce.
- **Utenti**: lista con online/offline e "sta parlando". Con `perms.privateCall`, pressione mantenuta su un utente online → `ptt` con target `user`. Chi riceve una chiamata privata vede un avviso con il nome del chiamante.
- **Impostazioni**:
  - modalità doppio tocco: VOX / sempre aperto;
  - sensibilità VOX (soglia di livello; trasmissione mantenuta 800 ms dopo l'ultima voce rilevata);
  - uscita audio: automatica / cuffia-Bluetooth / altoparlante;
  - schermo sempre acceso;
  - informazioni server e versione app.

### VOX

Calcolo del livello RMS per frame da 20 ms; sopra soglia → trasmette e prolunga di 800 ms.
In VOX il `ptt` viene inviato `active:true` all'apertura reale del microfono e `active:false`
alla chiusura, così gli altri vedono "sta parlando" solo quando c'è voce.

## 4. Pagina web del server

HTML + JavaScript senza framework, inclusa nel binario. Indirizzo `http://<ip-server>:8080`.

- **Primo avvio**: se non esiste la password admin, la pagina chiede di crearla.
- **Login admin** con sessione via cookie HttpOnly.
- **Panoramica**: nome e indirizzi del server, utenti collegati in tempo reale, chi parla e dove, pulsante "disconnetti".
- **Canali**: crea, rinomina, elimina, riordina, colore.
- **Utenti**: crea, modifica, elimina; nome, password, abilitato; canali assegnati (ordinati) con permesso parla/ascolta per ciascuno; permessi `allCall` e `privateCall`.
- **Impostazioni**: nome server (annunciato via mDNS), porta HTTP e UDP (richiedono riavvio), cambio password admin.

Ogni modifica si applica subito: i client interessati ricevono `config`; un utente disabilitato riceve
`kicked{reason:"disabled"}`; un canale rimosso viene tolto da `state` dei client.

## 5. Gestione degli errori

- **Rete persa (client)**: tentativi di riconnessione con attesa crescente (1, 2, 4… max 5 s); stato giallo
  "Riconnessione…"; al ritorno ripristina canali parla/ascolta; il microfono agganciato resta **chiuso**.
- **Jitter e perdite**: buffer per singola voce ~60 ms; pacchetto mancante → concealment del decoder Opus;
  voce senza pacchetti per 500 ms rimossa dal mix.
- **Login**: messaggi chiari per credenziali errate, utente disabilitato, sessione sostituita da altro dispositivo.
- **Permesso microfono negato**: spiegazione e collegamento alle impostazioni; l'app funziona in solo ascolto.
- **Server**: salvataggio configurazione atomico; errore esplicito se le porte sono occupate;
  client offline dopo 10 s senza segnali.

## 6. Test

### Server

- Test Go: login e sessioni (incluso login duplicato), permessi, router audio
  (chi riceve cosa, esclusione di chi parla, deduplica, chiamata a tutti, chiamata privata, scarto senza permesso),
  salvataggio/caricamento configurazione, API admin.
- **Client di prova** a riga di comando (`server/cmd/testclient`): login, invio di un tono come voce,
  ricezione e conteggio pacchetti, per verificare il server senza telefono.

### Android

- Test unitari della logica pura: gesti del pulsante PARLA (pressione, doppio tocco, tocco), VOX,
  mixer (somma e saturazione), backoff di riconnessione, codifica/decodifica dei pacchetti UDP.
- Prova manuale: due dispositivi (o dispositivo + emulatore) collegati al server sul Mac.
