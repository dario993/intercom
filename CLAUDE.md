# Intercom

Sistema intercom proprietario per rete locale (stile Unity Intercom, ma codice e grafica originali).
Un **server Go** instrada la voce; i **telefoni Android** parlano e ascoltano su canali, con chiamata a tutti,
chiamate private, VOX, mixer e lucchetto.

Stato: **tappa 1** (nucleo server + Android) completata sul ramo `tappa1-android`. Roadmap in [docs/architettura.md](docs/architettura.md#roadmap).

## Struttura

```
server/    server Go: binario unico, pagina admin incorporata (embed), UDP audio, mDNS
android/   app Kotlin + Jetpack Compose (minSdk 29)
docs/      documentazione per settori (indice sotto) + specifiche e piani originali
```

## Comandi rapidi

```bash
# server
cd server && go run .                    # pagina admin su http://<ip>:8080, audio UDP su 8081
cd server && go test ./...

# android
cd android && ./gradlew assembleDebug    # APK: android/app/build/outputs/apk/debug/app-debug.apk
cd android && ./gradlew test             # test unitari JVM
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

## Documentazione

Indice: **[docs/README.md](docs/README.md)**

| Settore | File |
|---|---|
| Architettura, flusso audio, roadmap | [docs/architettura.md](docs/architettura.md) |
| Protocollo (WebSocket + UDP) | [docs/protocollo.md](docs/protocollo.md) |
| Server Go e API admin | [docs/server.md](docs/server.md) |
| App Android | [docs/android.md](docs/android.md) |
| Build, test, installazione, problemi comuni | [docs/sviluppo.md](docs/sviluppo.md) |

## Convenzioni

- Testi visibili all'utente (UI, messaggi d'errore, log del server) in **italiano**; codice, commenti e commit in **inglese**.
- Commit nello stile `feat(android): ...`, `fix(server): ...`, `docs: ...`.
- Il protocollo è condiviso: se cambi un messaggio o un pacchetto, aggiorna **entrambi** i lati
  (`server/internal/proto` e `android/.../proto`) e [docs/protocollo.md](docs/protocollo.md).
- Le scorciatoie volute sono marcate nel codice con un commento `ponytail:` che spiega il limite.
- Quando cambia un comportamento descritto in `docs/`, aggiorna il documento nello stesso commit.
