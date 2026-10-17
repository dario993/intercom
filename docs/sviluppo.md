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

## App Android

```bash
cd android
./gradlew assembleDebug          # → app/build/outputs/apk/debug/app-debug.apk
./gradlew test                   # test unitari JVM
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

## Problemi comuni

| Sintomo | Causa probabile |
|---|---|
| `adb: device unauthorized` | Sul telefono non è stato accettato il messaggio "Consentire debug USB?" |
| "Il server non risponde" | La porta è sbagliata oppure il server non è avviato |
| "Server non raggiungibile" | IP sbagliato, reti Wi-Fi diverse o firewall del Mac |
| Connesso ma non si sente nessuno | UDP 8081 bloccato dal firewall, oppure nessun canale in Ascolta in comune con chi parla |
| Il server non compare nella lista | La rete blocca il multicast mDNS (comune su reti ospiti e aziendali): usa l'IP manuale |
| "porta HTTP/UDP non disponibile" all'avvio del server | Un'altra istanza è già in esecuzione: `lsof -nP -i :8080` |
