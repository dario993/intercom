# Sviluppo

## Server

```bash
cd server
go run .                         # oppure go run . -config /percorso/config.json
go test ./...
```

Al primo avvio apri l'indirizzo stampato nel log (es. `http://192.168.4.17:8080`), crea la password admin,
poi crea canali e utenti.

Build multipiattaforma e Docker: vedi [server/README.md](../server/README.md).
Con Docker serve `--network host` per l'UDP e per mDNS.

### Prova senza telefono

```bash
cd server
go run ./cmd/testclient -user bruno -pass pw -channel 1 -listen 10s &
go run ./cmd/testclient -user anna  -pass pw -channel 1 -talk 2s -listen 1s
```

Il primo client deve ricevere circa 100 pacchetti da anna. I due utenti devono già esistere
e avere il canale 1 assegnato.

### Tally senza mixer

```bash
cd server
go run ./cmd/fakeatem -addr 127.0.0.1:19910     # finto ATEM: CAM 1–8 e MP1
go run . -atem-port 19910                       # in un altro terminale
```

Nella pagina admin, Impostazioni → Tally: abilitato, IP `127.0.0.1`. Poi nel terminale del finto ATEM:
`pgm 3` (program), `pvw 1` (preview), `mix 1 2` (due camere in onda), `ftb` (nero sì/no), `quit` (mixer sparito).
`-atem-port` serve solo se la 9910 è già occupata sul computer (per esempio da Unity Intercom); col mixer vero non si usa.

### Collaudo sul posto con l'ATEM vero

Il protocollo ATEM è ricostruito da progetti open source e non è mai stato provato su questo mixer. Al primo uso:

1. IP dell'ATEM nella pagina admin → stato "Connesso · ATEM Production Studio 4K" e nomi degli ingressi corretti.
2. Cambio di camera su program e preview → fascia rossa/verde sui telefoni assegnati entro un attimo.
3. Dissolvenza tra due camere → per circa un secondo `IN ONDA: CAM A + CAM B`, poi solo la nuova.
4. Fade to black → fascia grigia `NERO` solo a schermo del tutto nero.
5. Logo o grafica da media player sopra una camera → non deve comparire nella fascia nera.
6. Cavo di rete dell'ATEM staccato → `TALLY NON DISPONIBILE` entro ~5 s; ricollegato → torna da solo.
7. ATEM Software Control aperto insieme al server → entrambi funzionano.
8. Wi-Fi del telefono spento → `DISCONNESSO` in rosso entro ~3 s.

## App Android

```bash
cd android
./gradlew assembleDebug          # → app/build/outputs/apk/debug/app-debug.apk
./gradlew test                   # test unitari JVM (sono in shared/, inclusi come composite build)
./gradlew connectedAndroidTest   # test su dispositivo (Opus, Keystore)
```

`android/local.properties` (escluso da git) contiene il percorso dell'Android SDK.

### Installare sul telefono

1. Sul telefono: Impostazioni → Info telefono → tocca 7 volte "Numero build", poi Opzioni sviluppatore → **Debug USB**.
2. Collega il cavo e accetta "Consentire debug USB?" (spunta "Consenti sempre").
3. Esegui `adb install -r android/app/build/outputs/apk/debug/app-debug.apk`.

Senza cavo: copia l'APK sul telefono e aprilo, consentendo l'installazione da origini sconosciute.

### Collegare l'app

- Telefono e server sulla **stessa rete Wi-Fi**.
- Il server compare da solo nella lista (mDNS); altrimenti inserisci l'IP del Mac (`ipconfig getifaddr en0`) e la porta `8080`.
- Su macOS consenti le connessioni in entrata quando il firewall lo chiede.
- Nell'emulatore il Mac host è raggiungibile a `10.0.2.2`, ma l'audio UDP verso l'emulatore richiede un port forwarding:
  per le prove audio usa telefoni veri.

## Client desktop (regia)

```bash
cd desktop
./gradlew run                    # apre il client
./gradlew test                   # test di shared/ e desktop/
./gradlew runProbe --args="--list"   # prototipo audio: schede, --loop, --bench, --net HOST PORT USER PASS
```

Non serve l'Android SDK: `desktop/` include solo `shared/`.

### Sviluppo da Windows (VM Parallels)

- Strumenti: `winget install EclipseAdoptium.Temurin.17.JDK GoLang.Go`.
- Lancia Gradle dall'unità `Y:\` (`\\Mac\Home`), non dal percorso `\\Mac\...`: `cmd` non accetta percorsi UNC come cartella corrente.
- Git rifiuta la cartella condivisa ("dubious ownership"): `git config --global --add safe.directory '%(prefix)///Mac/Home/Documents/APP/Intercom'`.
- La VM è sulla rete interna di Parallels (`10.211.55.x`): il server sul Mac si raggiunge a `10.211.55.2`,
  mentre i telefoni non raggiungono un server avviato nella VM.

## Problemi comuni

| Sintomo | Causa probabile |
|---|---|
| `adb: device unauthorized` | Sul telefono non è stato accettato il messaggio "Consentire debug USB?" |
| "Il server non risponde" | La porta è sbagliata oppure il server non è avviato |
| "Server non raggiungibile" | IP sbagliato, reti Wi-Fi diverse o firewall del Mac |
| Connesso ma non si sente nessuno | UDP 8081 bloccato dal firewall, oppure nessun canale in Ascolta in comune con chi parla |
| Il server non compare nella lista | La rete blocca il multicast mDNS (comune su reti ospiti e aziendali): usa l'IP manuale |
| "porta HTTP/UDP non disponibile" all'avvio del server | Un'altra istanza è già in esecuzione: `lsof -nP -i :8080` |
