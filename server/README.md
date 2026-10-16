# Intercom Server

Avvio:

```bash
go run .                      # oppure ./intercom-server
```

Apri l'indirizzo stampato all'avvio (es. `http://192.168.1.10:8080`), crea la password
amministratore, poi canali e utenti.

Opzioni: `-config <file>` (predefinito `intercom-config.json` nella cartella corrente).
Porte predefinite: 8080 TCP (pagina web e client), 8081 UDP (audio).

## Build

```bash
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o dist/intercom-server-mac .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/intercom-server.exe .
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o dist/intercom-server-linux .
```

## Docker (solo server Linux)

```bash
docker build -t intercom-server .
docker run -d --network host -v intercom-data:/data intercom-server
```

`--network host` serve per l'audio UDP e per la ricerca automatica (mDNS).

## Prova senza telefono

```bash
go run ./cmd/testclient -user bruno -pass pw -channel 1 -listen 10s &
go run ./cmd/testclient -user anna  -pass pw -channel 1 -talk 2s -listen 1s
```

Il primo client deve stampare circa 100 pacchetti ricevuti dall'utente di anna.
