# Server (Go)

Modulo `intercom` (`server/go.mod`). Dipendenze: `coder/websocket`, `grandcat/zeroconf` (mDNS), `x/crypto/bcrypt`.

## Pacchetti

| Percorso | Ruolo |
|---|---|
| `main.go` | Avvio: carica la configurazione, apre UDP e HTTP, mDNS, pulizia delle sessioni scadute ogni 2 s |
| `internal/config` | Modello (`Channel`, `User`, `Assignment`, `Settings`) e `Store` con scrittura atomica |
| `internal/hub` | Sessioni, permessi, broadcast di `users` e `talking`, inoltro UDP (`hub.go`); decisione dei destinatari (`route.go`) |
| `internal/proto` | Formato dei messaggi JSON e dei pacchetti UDP |
| `internal/web` | Router HTTP (`server.go`), WebSocket dei client (`ws.go`), API admin (`admin.go`), pagina in `static/` |
| `internal/discovery` | Annuncio mDNS `_intercom._tcp`, TXT `udp=<porta>` `name=<nome>` |
| `cmd/testclient` | Client da riga di comando per provare il server senza telefono |

## Configurazione

File JSON: `intercom-config.json` nella cartella corrente, oppure il percorso passato con `-config <file>`.
Se il file non esiste, si parte dai valori predefiniti. Il file è escluso da git.

```json
{
  "settings": { "serverName": "Intercom", "httpPort": 8080, "udpPort": 8081, "adminHash": "<bcrypt>" },
  "channels": [{ "id": 1, "name": "Regia", "color": "#e8a33d" }],
  "users": [{
    "id": 2, "name": "anna", "passwordHash": "<bcrypt>", "enabled": true,
    "channels": [{ "channelId": 1, "talk": true, "listen": true }],
    "allCall": false, "privateCall": true
  }],
  "nextId": 3
}
```

- `Store.Update` lavora su una copia e la salva in modo atomico (file temporaneo, fsync, rename);
  la copia diventa attiva solo se il salvataggio riesce.
- Utenti e canali condividono un unico contatore di id (`nextId`); gli id liberati non vengono riusati.
- Le password di utenti e admin sono salvate con bcrypt. Il nome utente non distingue maiuscole e minuscole.
- Il cambio delle porte vale solo dopo il riavvio. Il server annuncia sempre le porte effettivamente in uso.

## Sessioni (`internal/hub`)

- Ogni login crea una `Session` con id casuale uint32 e un canale `Out` (64 messaggi) verso il WebSocket.
- Un solo lock (`Hub.mu`) protegge tutto. Il controllo bcrypt avviene fuori dal lock.
- L'inoltro UDP avviene sotto il lock (marcato `ponytail:`): va bene per una rete locale,
  da spostare in una coda se il lock diventa un collo di bottiglia.
- `ConfigChanged()` riapplica la configurazione a tutti i client dopo ogni modifica dell'admin.

## API admin

Autenticazione con cookie di sessione HttpOnly. I token stanno in memoria, quindi un riavvio del server richiede un nuovo login.

| Metodo e percorso | Auth | Uso |
|---|---|---|
| `GET /` | — | pagina admin (`static/index.html`, `app.js`, `style.css`) |
| `GET /ws` | — | WebSocket dei client |
| `GET /api/status` | — | dice se la password admin esiste già e se si è autenticati |
| `POST /api/setup` | — | primo avvio: crea la password admin |
| `POST /api/login`, `POST /api/logout` | — | sessione admin |
| `GET /api/config` | sì | configurazione completa (senza gli hash delle password) |
| `PUT /api/settings` | sì | nome server, porte |
| `POST /api/password` | sì | cambio della password admin |
| `POST /api/channels`, `PUT/DELETE /api/channels/{id}` | sì | crea, modifica ed elimina canali |
| `PUT /api/channels/order` | sì | riordina i canali |
| `POST /api/users`, `PUT/DELETE /api/users/{id}` | sì | crea, modifica ed elimina utenti |
| `GET /api/live` | sì | panoramica in tempo reale: online, chi parla e dove |
| `POST /api/kick/{id}` | sì | disconnette un utente |

La pagina admin è HTML e JavaScript senza framework, incorporata nel binario con `embed`.

## Avvio, build e Docker

Vedi [server/README.md](../server/README.md) e [sviluppo.md](sviluppo.md).
