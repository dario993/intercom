# Intercom Regia (Windows) Implementation Plan

> Steps use checkbox (`- [ ]`) syntax for tracking. Tasks are ordered risk-first: Task 1–2 answer the questions that can stop the project before the bulk of the work.

**Goal:** Client desktop per il PC della regia (Windows 10/11 x64): stesse funzioni dell'app Android tranne "schermo sempre acceso", tasto PARLA sulla tastiera (tieni premuto / tocco singolo per agganciare), scelta schede audio, distribuito come cartella zip con Java incluso.

**Architecture:** Tre build Gradle indipendenti legate da *composite build*:
`shared/` (Kotlin/JVM puro: protocollo, connessione, logica PARLA, jitter/mixer/VOX), `android/` (app esistente, ora dipende da `shared`) e `desktop/` (Compose for Desktop). Così `desktop/` si costruisce senza Android SDK (la VM Windows non ce l'ha) e `android/` resta la radice dei comandi Android di oggi.
Il desktop ha un suo `DesktopController` (stesso ruolo e stessa `UiState` di `IntercomController`, ma senza `Context`, servizio e permessi Android), un `DesktopAudioEngine` su Java Sound + Concentus, preferenze in `%APPDATA%\Intercom`, password cifrata con DPAPI, ricerca server con jmDNS.

**Tech Stack:** Kotlin 2.2.20, Compose Multiplatform (plugin `org.jetbrains.compose` + `org.jetbrains.kotlin.plugin.compose`), Gradle 9.5 (wrapper copiato da `android/`), OkHttp 4.12, `org.json`, Concentus (Opus in Java puro), jmDNS, JNA `jna-platform` (DPAPI), Java Sound. Test: JUnit 4. Runtime nello zip: Temurin JRE 17+ **Windows x64**.

**Spec:** `docs/superpowers/specs/2026-10-17-intercom-regia-windows-design.md` (approvata 2026-10-17).

## Global Constraints

- Server e protocollo invariati. Audio 48 kHz mono 16 bit, frame 20 ms (960 campioni), Opus ~32 kbit/s.
- Ambiente di sviluppo: VM Parallels **Windows 11 ARM64**. Il JDK e Go installati qui sono ARM64; lo zip per la regia contiene invece un JRE **x64** e le parti native Compose **windows-x64** (Windows 11 ARM le esegue in emulazione, quindi lo zip si può provare anche nella VM).
- **I ritardi audio misurati nella VM non sono attendibili** (audio virtualizzato): servono solo a vedere che la catena funziona. La misura che decide (≤ ~150 ms bocca-orecchio) va fatta sul PC della regia.
- Testi visibili in italiano; codice, commenti e commit in inglese; commit `feat(desktop): …`, `refactor(shared): …`.
- Dopo il Task 3 `cd android && ./gradlew test assembleDebug` deve passare (da verificare sul Mac: la VM non ha l'Android SDK).
- Grafica originale nello stile di `android/.../ui/Theme.kt`, niente di Unity Intercom.
- Versioni delle librerie (Compose MP, Concentus, jmDNS, JNA): scegliere l'ultima stabile pubblicata da almeno due settimane e compatibile con Kotlin 2.2.20 / Gradle 9.5; annotarle in Task 1.

## Review Focus

1. Tasto bloccato / finestra che perde il focus col tasto premuto → microfono chiuso (test `focusLostWhileHeldCloses`, Task 4; prova a mano con Alt+Tab, Task 8).
2. Autorepeat della tastiera → nessun aggancio/sgancio spurio (test `autorepeatIgnored`, Task 4).
3. Prima sillaba: il microfono si apre al **down**, non all'up (test `downOpensImmediately`, Task 4).
4. Concentus ↔ MediaCodec Android in entrambe le direzioni (Task 2, prova con un telefono vero).
5. Scheda audio salvata sparita → si usa quella predefinita e lo si dice (Task 5).
6. App Android invariata dopo lo spostamento in `shared/` (Task 3).

---

### Task 0: Strumenti nella VM

- [ ] Installare con `winget`: `EclipseAdoptium.Temurin.17.JDK` (o 21) e `GoLang.Go` (per far girare il server in locale).
- [ ] `git config --global --add safe.directory` per `//Mac/Home/Documents/APP/Intercom` (la cartella condivisa di Parallels risulta di un altro proprietario).
- [ ] Verificare `java -version`, `go version`, `cd server && go test ./... && go run .` → pagina admin su `http://localhost:8080`.
- [ ] Annotare in `docs/sviluppo.md` la sezione "Sviluppo da Windows".

### Task 1: Scheletro `shared/` + `desktop/` (build e avvio)

**Files:**
- Create: `shared/settings.gradle.kts`, `shared/build.gradle.kts` (plugin `org.jetbrains.kotlin.jvm`, `jvmToolchain(17)`)
- Create: `desktop/settings.gradle.kts` (`includeBuild("../shared")`), `desktop/build.gradle.kts`, `desktop/gradle.properties`, wrapper copiato da `android/`
- Create: `desktop/src/main/kotlin/app/intercom/desktop/Main.kt` (finestra vuota "Intercom")
- Modify: `.gitignore`

- [ ] `shared` dipendenze: `api("com.squareup.okhttp3:okhttp:4.12.0")`, `compileOnly("org.json:json:20240303")` (su Android `org.json` è di sistema: includerlo nell'APK darebbe classi duplicate), `testImplementation` junit, org.json, mockwebserver.
- [ ] `desktop` dipendenze: `implementation("app.intercom:shared")`, `implementation("org.json:json:20240303")`, `compose.desktop.windows_x64` (non `currentOs`, così lo zip ha le parti native giuste anche costruito su ARM64 o Mac), `compose.material3`, `compose.materialIconsExtended`, Concentus, jmDNS, `jna-platform`.
- [ ] Per lo sviluppo nella VM: profilo che usa `compose.desktop.currentOs` (windows-arm64) quando `-PdevNative` è passato, altrimenti windows-x64.
- [ ] `cd desktop && ./gradlew run` apre una finestra; `./gradlew :shared:test` gira (task dell'included build).
- [ ] Commit `build: add shared and desktop Gradle builds`.

### Task 2: Prototipo dei rischi audio (Concentus + Java Sound)

**Files:**
- Create: `desktop/src/main/kotlin/app/intercom/desktop/audio/Opus.kt` — `OpusEncoder`/`OpusDecoder` su Concentus, stessa interfaccia di quelli Android (`encode(ShortArray): List<ByteArray>`, `decode(ByteArray): ShortArray?`, `AutoCloseable`)
- Create: `desktop/src/main/kotlin/app/intercom/desktop/audio/SoundDevices.kt` — elenco schede di ingresso/uscita per nome (`AudioSystem.getMixerInfo()` filtrate per `TargetDataLine`/`SourceDataLine` 48 kHz mono 16 bit)
- Create: `desktop/src/main/kotlin/app/intercom/desktop/proto/LatencyProbe.kt` — `./gradlew runProbe`: mic → Concentus encode → decode → cuffia, con buffer da 20/40/60 ms; stampa i ms di buffer in/out e il carico CPU di encode+decode per frame
- Test: `desktop/src/test/kotlin/app/intercom/desktop/audio/OpusTest.kt` — sinusoide 1 kHz encode→decode, energia conservata, 960 campioni per frame

- [ ] Test Concentus in loopback (unitario).
- [ ] `runProbe` nella VM: la catena funziona, nessun xrun con buffer da 40 ms. (Il ritardo stampato qui **non** conta.)
- [ ] Compatibilità con Android: server in locale nella VM, un telefono con l'app Android sulla stessa rete, un mini-client di prova nel probe (login WebSocket + UDP via `Connection` copiato temporaneamente) che riceve la voce dal telefono e la riproduce, e trasmette un tono che il telefono deve sentire pulito. Entrambe le direzioni OK → si prosegue; altrimenti fermarsi e tornare dall'utente.
- [ ] Annotare i risultati in fondo alla spec (sezione "Rischi").
- [ ] Commit `feat(desktop): Opus via Concentus and audio latency probe`.

### Task 3: Spostare la logica pura in `shared/`

**Files (move, package `app.intercom.android.X` → `app.intercom.core.X`):**
- `proto/Messages.kt`, `proto/Packets.kt`, `net/Connection.kt`, `talk/TalkButton.kt`, `talk/Transmit.kt`, `talk/TalkingTracker.kt`, `audio/JitterBuffer.kt`, `audio/Mixer.kt`, `audio/Vox.kt`
- e i test corrispondenti da `android/app/src/test/...` a `shared/src/test/kotlin/app/intercom/core/...`
- Modify: `android/settings.gradle.kts` (`includeBuild("../shared")`), `android/app/build.gradle.kts` (`implementation("app.intercom:shared")`, rimuovere `testImplementation` ora inutili), tutti gli import in `android/app/src/main`
- Modify: `android/` task `test` dipende da `gradle.includedBuild("shared").task(":test")`

- [ ] `git mv` per conservare la storia; poi correggere package e import.
- [ ] `cd desktop && ./gradlew :shared:test` verde nella VM.
- [ ] Sul Mac: `cd android && ./gradlew test assembleDebug` verde e APK provato sul telefono (chiedere all'utente).
- [ ] Aggiornare `docs/android.md`, `docs/architettura.md`, `CLAUDE.md` (struttura).
- [ ] Commit `refactor(shared): move platform-free logic into shared module`.

### Task 4: Gesto del tasto (`KeyTalkButton`)

**Files:**
- Create: `shared/src/main/kotlin/app/intercom/core/talk/KeyTalkButton.kt`
- Test: `shared/src/test/kotlin/app/intercom/core/talk/KeyTalkButtonTest.kt`

**Interface:** stessa forma di `TalkButton` (`mode: TalkButton.Mode`, `down(now)`, `up(now)`, `reset()`) più `focusLost(now)`. Così `chooseTarget` non cambia.

Regole (spec, "Gesto del tocco singolo"):
- `down` da IDLE → HOLD subito. `down` mentre il tasto è già giù (autorepeat) → ignorato.
- `up`: se non era agganciato al down: durata ≤ 250 ms → LATCHED, altrimenti IDLE.
  Se era agganciato al down: tocco → IDLE; pressione lunga → resta LATCHED.
- `up` senza `down` precedente → ignorato.
- `focusLost` col tasto giù = `up` di una pressione lunga (HOLD → IDLE, LATCHED resta LATCHED).

- [ ] Test: `downOpensImmediately`, `holdClosesOnRelease`, `tapLatches`, `tapWhileLatchedUnlatches`, `longPressWhileLatchedKeepsLatch`, `autorepeatIgnored`, `upWithoutDownIgnored`, `focusLostWhileHeldCloses`, `focusLostWhileLatchedKeepsLatch`, `resetClearsLatch`.
- [ ] Commit `feat(shared): single-tap latch gesture for keyboard talk key`.

### Task 5: Motore audio desktop

**Files:**
- Create: `desktop/src/main/kotlin/app/intercom/desktop/audio/DesktopAudioEngine.kt`

Stessa interfaccia e stessa struttura di `android/.../audio/AudioEngine.kt` (costruttore con `capture`, `onFrame`, `sendVoice`, `gain`, `onCaptureLost`; `vox`; `onPacket`; `start`/`stop`), con:
- `TargetDataLine`/`SourceDataLine` aperte sulla scheda scelta per nome; buffer scelto in Task 2.
- Se la scheda salvata non c'è: predefinita di Windows + callback `onDeviceFallback(nome)` per avvisare l'utente.
- Un `OpusDecoder` + `JitterBuffer` per chi parla, `Mixer` per il mix, guadagno per canale.
- Linea di cattura che si chiude da sola → `onCaptureLost`.

- [ ] Verifica a mano nella VM contro il server locale e il telefono.
- [ ] Commit `feat(desktop): Java Sound audio engine`.

### Task 6: Controller, preferenze, credenziali, ricerca server

**Files:**
- Create: `desktop/.../DesktopController.kt` — porta di `IntercomController`: stessa `UiState` (senza `keepScreenOn`, con `inputDevice`, `outputDevice`, `talkKey`, `deviceWarning`), `Dispatchers.Main` (Swing) al posto di `Handler`, `System.nanoTime()/1e6` come orologio, `KeyTalkButton` al posto di `TalkButton`, `focusLost()`.
- Create: `desktop/.../Prefs.kt` — `java.util.Properties` in `%APPDATA%\Intercom\settings.properties` (host, porta, utente, password cifrata, volumi, openMic, soglia VOX, schede, tasto, finestra).
- Create: `desktop/.../SecretBox.kt` — `Crypt32Util.cryptProtectData/UnprotectData` + Base64; se la decifratura fallisce → `null` (password da reinserire).
- Create: `desktop/.../Discovery.kt` — jmDNS su `_intercom._tcp.local.`, `StateFlow<List<Found>>` come su Android.

- [ ] Commit `feat(desktop): controller, prefs, DPAPI credentials, mDNS discovery`.

### Task 7: Interfaccia

**Files:**
- Create: `desktop/.../ui/Theme.kt` (colori di `android/.../ui/Theme.kt`), `ui/ConnectScreen.kt`, `ui/MainScreen.kt` (layout "A" della spec), `ui/Panels.kt` (mixer, impostazioni come pannelli sopra la schermata), `Main.kt`.

- [ ] Intestazione (stato, utente, server, orologio, Esci); canali a sinistra; utenti a destra con PARLA privato; PARLA grande con indicazione del tasto; TUTTI / BLOCCA / MIXER / IMPOSTAZIONI.
- [ ] Tasto: `onPreviewKeyEvent` sulla finestra → `talkDown/talkUp`; `windowFocusListener` → `focusLost()`. Clic sul PARLA = stesso gesto.
- [ ] Impostazioni: "Assegna tasto" (prossimo tasto premuto, Esc annulla; predefinito F9), VOX/sempre aperto + soglia, schede di ingresso e uscita, info server e versione.
- [ ] Fascia rossa per chiamata privata in arrivo; avviso se la scheda audio è stata sostituita.
- [ ] Finestra ingrandita all'avvio, ricorda dimensione e posizione, F11 schermo intero. Autoconnessione con "ricorda credenziali".
- [ ] Commit `feat(desktop): main window`.

### Task 8: Cartella pronta (zip) e prova end-to-end

**Files:**
- Modify: `desktop/build.gradle.kts` — task `regiaZip`: `packageUberJarForCurrentOS` (con le parti native windows-x64 del Task 1) + JRE Temurin **Windows x64** scaricato e messo in cache in `desktop/build/jre-cache` + `Intercom.bat` (`start "" "%~dp0jre\bin\javaw.exe" -jar "%~dp0app\intercom-regia.jar"`) → `desktop/build/dist/Intercom-Regia.zip`.

- [ ] Estrarre lo zip in una cartella nuova nella VM e avviarlo con doppio clic (gira in emulazione x64): login, canali, PARLA col tasto, tocco, Alt+Tab col tasto premuto, BLOCCA, MIXER, chiamata a tutti e privata con il telefono.
- [ ] Accettare il messaggio del firewall e verificare la ricerca server mDNS.
- [ ] Consegnare lo zip all'utente per la **misura vera del ritardo sul PC della regia** (`Intercom.bat --probe` lancia il probe del Task 2).
- [ ] Commit `build(desktop): portable Windows x64 zip`.

### Task 9: Documentazione

- [ ] Nuovo `docs/desktop.md`; voce in `docs/README.md`; `CLAUDE.md` (struttura e comandi desktop); `docs/sviluppo.md` (costruire lo zip, installarlo in regia); `docs/architettura.md` (modulo condiviso, client desktop nella roadmap).
- [ ] Commit `docs: desktop client for the control room`.
