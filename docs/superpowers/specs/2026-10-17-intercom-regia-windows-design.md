# Intercom — Client desktop per la regia (Windows)

Data: 2026-10-17
Stato: approvato (2026-10-17)

## Perché

In regia c'è un PC Windows dedicato solo all'intercom, con un monitor dedicato e una cuffia con microfono
collegata al jack. L'operatore deve stare sulla rete intercom con le stesse funzioni dei telefoni Android,
ma usando **un tasto della tastiera** per parlare e una schermata pensata per un monitor grande.
Sullo stesso PC gira anche il server, ma il client deve restare **un'app separata**.

Successo = dal PC della regia l'operatore parla e ascolta sui canali, chiama tutti o un singolo utente,
usa mixer e lucchetto, e parla con un tasto della tastiera con gli stessi gesti
(tieni premuto / tocco per agganciare), con un ritardo paragonabile ai telefoni.

## Obiettivi

- Tutte le funzioni dell'app Android (vedi [docs/android.md](../../android.md)), con l'eccezione di "schermo sempre acceso".
- Tasto PARLA sulla tastiera, assegnabile dalle impostazioni.
- Scelta della scheda audio di ingresso e di uscita.
- Distribuzione come cartella pronta da copiare, senza installazione.

## Non obiettivi

- Installer `.msi`. Arriverà più avanti, compilando da un PC Windows (decisione dell'utente).
- Avvio automatico con Windows: l'app la avvia sempre l'operatore (decisione dell'utente).
- Tasto PARLA globale: il tasto funziona **solo con la finestra dell'intercom in primo piano** (decisione dell'utente).
- Modifiche al server o al protocollo.
- Condividere le schermate (codice UI) con Android: la disposizione è diversa.
- macOS e Linux come piattaforme ufficiali. Se l'app gira anche lì, bene, ma non è richiesto.

## Anti-obiettivi (sarebbe un fallimento anche se funziona)

- L'operatore resta in onda senza volerlo: tasto bloccato, finestra che perde il primo piano mentre il tasto è premuto, crash della cattura.
- La prima sillaba persa perché il microfono si apre solo al rilascio del tasto.
- Rompere l'app Android o i suoi test spostando il codice nel modulo condiviso.
- Grafica copiata da Unity Intercom: deve restare originale, con lo stile dell'app Android.

## Vincoli

- Server e protocollo invariati (vedi [docs/protocollo.md](../../protocollo.md)): WebSocket `/ws` per il controllo, UDP per la voce, Opus 48 kHz mono, frame da 20 ms, ~32 kbit/s.
- Sviluppo e build su macOS. La cartella per Windows deve poter essere prodotta dal Mac.
- Sul PC della regia non si installa nulla: Java è incluso nella cartella.
- Testi visibili in italiano; codice, commenti e commit in inglese (convenzioni in `CLAUDE.md`).

## Cosa ha deciso l'utente

### Tasto PARLA

- Assegnabile nelle impostazioni ("Assegna tasto" → premi il tasto). Nessun tasto predefinito obbligatorio;
  *(mia scelta)* predefinito **F9**.
- **Tieni premuto**: il microfono si apre e si chiude al rilascio.
- **Tocco veloce (singolo)**: il microfono si apre e **resta aperto**; un altro tocco veloce lo chiude.
  Su PC è un **tocco singolo**, diverso dal doppio tocco di Android: l'utente lo ritiene più intuitivo su PC.
- Il tasto agisce come il pulsante PARLA grande: trasmette sui canali con PARLA attivo; senza canali PARLA attivi non fa nulla.
- Funziona solo con la finestra dell'intercom in primo piano.

### Schermata (opzione "A" scelta sulle bozze)

Finestra pensata per un monitor 16:9 dedicato.

```
┌──────────────────────────────────────────────────────────────────────┐
│ ● regia   Intercom                                     14:45   ⇥ Esci │
├───────────────────────────────────────────────┬──────────────────────┤
│ ● Teatro        ▶ Chiara      [PARLA][ASCOLTA]│ Utenti               │
│ ● Camere                      [PARLA][ASCOLTA]│ ● Chiara  sta parl. [PARLA]
│ ● Seguipersona                [PARLA][ASCOLTA]│ ● mattia            [PARLA]
│   (scorre se i canali sono tanti)             │ ● luca    offline   [PARLA]
├───────────────────────────────────────────────┴──────────────────────┤
│                              PARLA                                    │
│           tieni premuto F9 per parlare · tocco veloce = resta aperto  │
├─────────────────┬─────────────────┬─────────────────┬────────────────┤
│ 📢 TUTTI        │ 🔒 BLOCCA       │ 🎚 MIXER        │ ⚙ IMPOSTAZIONI │
│ tieni premuto   │ tieni per sbloc.│ volumi          │ tasto, audio   │
└─────────────────┴─────────────────┴─────────────────┴────────────────┘
```

- **Intestazione**: pallino di stato (verde collegato, giallo riconnessione, rosso errore), utente, nome server, orologio grande, Esci.
- **Canali** (sinistra): un canale per riga come su Android: pallino colorato, nome, chi sta parlando, PARLA e ASCOLTA affiancati; bordo evidenziato quando qualcuno parla sul canale.
- **Utenti** (destra, sempre visibili): stato online/offline/sta parlando e un tasto PARLA per la chiamata privata, tenuto premuto, attivo solo se l'utente è online e se si ha il permesso `privateCall`.
- **PARLA** grande in basso, con l'indicazione del tasto assegnato; cambia colore in onda e quando è agganciato, come su Android.
- **Sotto PARLA**: quattro pulsanti più piccoli nello stesso stile, ciascuno con una riga di spiegazione:
  TUTTI (tenuto premuto, visibile/attivo solo con `allCall`), BLOCCA (giallo quando attivo, sblocco con pressione lunga),
  MIXER, IMPOSTAZIONI.
- Colori e stile dell'app Android (`android/.../ui/Theme.kt`): sfondo scuro, ambra per PARLA, verde acqua per ASCOLTA, rosso per in onda.

### Impostazioni

- Tasto PARLA.
- Modo del microfono agganciato: VOX (predefinito) o sempre aperto; soglia VOX. Come su Android.
- Scheda audio di **ingresso** e di **uscita**, scelte per nome tra quelle di Windows.
- Informazioni su server e versione.

### Avvio e installazione

- Niente avvio automatico. Con "ricorda credenziali", l'app si collega da sola all'apertura, come Android.
- Cartella pronta `Intercom-Regia` (zip) con Java per Windows incluso e avvio con doppio clic; aggiornamento = sostituire la cartella.

## Cosa ho deciso io (l'utente può correggere)

- **Tecnologia**: Kotlin + Compose for Desktop (Compose Multiplatform), lo stesso linguaggio e modello di UI di Android.
- **Modulo condiviso**: il codice Android senza dipendenze da Android esce in un modulo Kotlin/JVM comune, usato da Android e desktop:
  `proto/` (Messages, Packets), `net/Connection.kt`, `talk/` (TalkButton, Transmit, TalkingTracker),
  `audio/JitterBuffer.kt`, `audio/Mixer.kt`, `audio/Vox.kt`, e i relativi test unitari.
  Oggi questi file importano solo `org.json` (incluso in Android; sul desktop va aggiunto come dipendenza) e OkHttp.
  Dopo lo spostamento l'app Android deve funzionare come prima e `./gradlew test` deve passare.
- **Gesto del tocco singolo**: il microfono si apre già alla **pressione** del tasto (niente prima sillaba persa).
  Al rilascio: pressione ≤ ~250 ms → tocco → resta agganciato; più lunga → si chiude.
  Se era già agganciato: tocco → sgancia; pressione lunga → resta agganciato (come Android).
  L'autorepeat della tastiera (eventi "giù" ripetuti mentre il tasto è tenuto) va ignorato.
  Va implementato come variante di `TalkButton` (o una classe sorella nel modulo condiviso) con i suoi test.
- **Il pulsante PARLA cliccato col mouse** usa lo stesso gesto del tasto (tocco singolo), per coerenza sul PC.
- **Perdita del primo piano**: se la finestra perde il focus mentre il tasto è premuto, si tratta come un rilascio
  di una pressione lunga (il microfono si chiude). Se il microfono era già agganciato resta aperto.
- **Riconnessione**: il microfono agganciato si chiude, come su Android.
- **Chiamata privata in arrivo**: fascia rossa in alto con il nome del chiamante, come su Android.
- **Mixer e impostazioni**: pannello sopra la schermata. Con il lucchetto si possono aprire, ma canali e barra restano bloccati come su Android.
- **Finestra**: si apre ingrandita, ricorda dimensione e posizione; F11 = schermo intero.
- **Server non avviato all'apertura**: errore come su Android; si riprova con "Connetti". Si può usare `localhost`.
- **Audio**: Java Sound (`javax.sound.sampled`): `TargetDataLine` per il microfono, `SourceDataLine` per la cuffia,
  48 kHz mono 16 bit, buffer piccoli. Scheda scelta per nome; se la scheda salvata non c'è più, si usa quella predefinita di Windows e lo si segnala.
  Nessuna cancellazione d'eco: in cuffia non serve.
- **Opus**: Concentus (porting Java puro di libopus), nessuna libreria nativa.
- **Ricerca server**: jmDNS per `_intercom._tcp`.
- **Credenziali**: impostazioni in un file nella cartella dati dell'utente (`%APPDATA%\Intercom`);
  password cifrata con DPAPI di Windows (JNA `Crypt32Util`), legata all'utente Windows.
- **Cartella pronta**: costruita sul Mac con un task Gradle che mette insieme il JAR dell'app (con le parti native
  di Compose per Windows x64), un JRE Windows x64 scaricato (es. Temurin 17+) e un lanciatore (`Intercom.exe` o `.bat`),
  e produce uno zip.
- **Volume**: mixer dell'app (generale × canale) più il volume di Windows.

## Rischi da verificare per primi

1. **Ritardo audio di Java Sound su Windows** con la cuffia sul jack. Prima di tutto il resto, un prototipo che
   cattura, codifica e decodifica con Concentus, riproduce e misura il ritardo. Obiettivo: non peggio dei telefoni
   (indicativamente ≤ 150 ms bocca-orecchio in rete locale). Se è troppo alto, si torna dall'utente prima di proseguire.
2. **Concentus**: compatibilità con l'Opus di `MediaCodec` Android in entrambe le direzioni, e costo in CPU.
3. **Build per Windows dal Mac**: verificare presto che lo zip prodotto sul Mac si avvii davvero su Windows.

### Esito delle prove (2026-10-17, VM Parallels Windows 11 ARM64, JDK Temurin 17 x64 in emulazione)

Prototipo: `cd desktop && gradlew runProbe --args="--bench | --loop | --net HOST PORT USER PASS --tone --cycle 120"`.

- **Concentus ↔ Android: OK in entrambe le direzioni.** PC → telefono: tono a 440 Hz sentito pulito sul telefono.
  Telefono → PC: 3303 pacchetti Opus di `MediaCodec` ricevuti, tutti decodificati, nessuno rifiutato.
- **CPU di Concentus: OK.** A regime ~1,2 ms per codificare e ~0,3 ms per decodificare un frame da 20 ms,
  anche con la JVM x64 emulata su ARM.
- **Riscaldamento della JVM:** nella VM servono ~25 s prima che la catena regga il tempo reale; nel frattempo si perde
  audio. Il motore audio dovrà scaldare Opus all'avvio (frame di silenzio in background) prima di aprire le linee.
  Da rimisurare sul PC della regia (x64 nativo).
- **Ritardo:** stima software nella VM ~95–110 ms (buffer delle linee Java Sound 40–60 ms). **Non attendibile**:
  la misura bocca-orecchio vera resta da fare sul PC della regia con lo zip del Task 8.

## Lasciato al costruttore

- Struttura Gradle: come organizzare modulo condiviso, app Android e app desktop (una build unica o più build),
  purché `android/` continui a compilare e i comandi in `CLAUDE.md` vengano aggiornati.
- Nome esatto dei moduli e dei package del desktop.
- Dimensioni dei buffer audio e il modo di misurare il ritardo nel prototipo.
- Tipo di lanciatore Windows (`.exe` generato o `.bat`).
- Test: logica pura nel modulo condiviso con test unitari (in particolare il nuovo gesto del tocco singolo e la perdita
  del focus); parti audio e Windows verificate a mano sul PC della regia.

## Dove eravamo rimasti (2026-10-17)

- **Approvato dall'utente** il 2026-10-17, comprese le scelte in "Cosa ho deciso io". Prossimo passo: il piano.
- PC della regia: Windows 10/11 **x64**. Il messaggio del firewall al primo avvio si può accettare (mDNS funziona).
- Il PC Windows da cui si sviluppa è una **macchina virtuale Parallels ARM64** sul Mac, non il PC della regia,
  e non ha Java né Go. Su scelta dell'utente il prototipo dei rischi gira lì: serve a verificare che l'app parta
  e che Concentus sia compatibile con Android, ma **il ritardo misurato nella VM non è attendibile**
  (audio virtualizzato). La misura vera del ritardo resta da fare sul PC della regia.
- Lo zip deve includere un JRE **Windows x64**, anche se la VM di sviluppo è ARM64.

## Documentazione da aggiornare

- `CLAUDE.md`: struttura del repository e comandi del client desktop.
- Nuovo `docs/desktop.md`, e voce in `docs/README.md`.
- `docs/android.md` e `docs/architettura.md` per il modulo condiviso.
- `docs/sviluppo.md`: come costruire lo zip e installarlo sul PC della regia.
