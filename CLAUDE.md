# Intercom

Sistema intercom proprietario per rete locale (stile Unity Intercom, ma codice e grafica originali).
Un **server Go** instrada la voce; i **telefoni Android** parlano e ascoltano su canali, con chiamata a tutti,
chiamate private, VOX, mixer e lucchetto.

Stato: **tappa 1** (nucleo server + Android) completata. Roadmap in [docs/architettura.md](docs/architettura.md#roadmap).

## Struttura

```
server/    server Go: binario unico, pagina admin incorporata (embed), UDP audio, mDNS
android/   app Kotlin + Jetpack Compose (minSdk 29)
shared/    logica comune Kotlin/JVM (protocollo, connessione, PARLA, jitter/mixer/VOX), inclusa da android/ e desktop/
desktop/   client Windows per la regia, Compose for Desktop (in costruzione)
docs/      documentazione per settori (indice sotto) + specifiche e piani originali
```

## Comandi rapidi

```bash
# server
cd server && go run .                    # pagina admin su http://<ip>:8080, audio UDP su 8081
cd server && go test ./...

# android
cd android && ./gradlew assembleDebug    # APK: android/app/build/outputs/apk/debug/app-debug.apk
cd android && ./gradlew test             # test unitari JVM (quelli di shared/)
adb install -r android/app/build/outputs/apk/debug/app-debug.apk

# desktop (anche da Windows, senza Android SDK)
cd desktop && ./gradlew run              # apre il client
cd desktop && ./gradlew test             # test di shared/ e desktop/
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
  (`server/internal/proto` e `shared/.../core/proto`) e [docs/protocollo.md](docs/protocollo.md).
- Le scorciatoie volute sono marcate nel codice con un commento `ponytail:` che spiega il limite.
- Quando cambia un comportamento descritto in `docs/`, aggiorna il documento nello stesso commit.

## Git

- Si lavora direttamente su `main`, senza rami di funzionalità. Remoto: https://github.com/dario993/intercom
