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
| `internal/tally` | Client ATEM (UDP 9910, sola lettura) con riconnessione, calcolo della fascia per utente, finto ATEM |
| `internal/discovery` | Annuncio mDNS `_intercom._tcp`, TXT `udp=<porta>` `name=<nome>` |
| `cmd/testclient` | Client da riga di comando per provare il server senza telefono |
| `cmd/fakeatem` | Finto mixer ATEM per provare il tally senza hardware (comandi `pgm`, `pvw`, `mix`, `ftb`) |

## Configurazione

File JSON: `intercom-config.json` nella cartella corrente, oppure il percorso passato con `-config <file>`.

Indirizzo di ascolto: `-ip <indirizzo>` sceglie l'IP su cui ascoltare (`-ip tutti` = tutti). Senza `-ip`, se il PC ha più IP, all'avvio compare un menu; senza risposta entro 15 s, oppure senza console (Docker, servizio), il server ascolta su tutti. Con un IP scelto, anche l'annuncio mDNS esce solo da quella scheda e `localhost` non risponde più.
Se il file non esiste, si parte dai valori predefiniti. Il file è escluso da git.

```json
{
  "settings": { "serverName": "Intercom", "httpPort": 8080, "udpPort": 8081, "adminHash": "<bcrypt>",
                "tally": { "enabled": true, "interface": "atem", "host": "192.168.1.240" } },
  "channels": [{ "id": 1, "name": "Regia", "color": "#e8a33d" }],
  "users": [{
    "id": 2, "name": "anna", "passwordHash": "<bcrypt>", "enabled": true,
    "channels": [{ "channelId": 1, "talk": true, "listen": true }],
    "allCall": false, "privateCall": true, "tallyInput": 3
  }],
  "nextId": 3
}
```

- `Store.Update` lavora su una copia e la salva in modo atomico (file temporaneo, fsync, rename);
  la copia diventa attiva solo se il salvataggio riesce.
- Utenti e canali condividono un unico contatore di id (`nextId`); gli id liberati non vengono riusati.
- Le password di utenti e admin sono salvate con bcrypt. Il nome utente non distingue maiuscole e minuscole.
- Il cambio delle porte vale solo dopo il riavvio. Il server annuncia sempre le porte effettivamente in uso.
- `tallyInput` è l'id della sorgente ATEM assegnata all'utente (assente = nessuna fascia tally sul telefono).
- L'indirizzo del mixer è `IP` (porta 9910) oppure `IP:porta`; la porta diversa serve solo per provare con il finto ATEM.

## Tally (`internal/tally`)

- Il server si collega all'ATEM come client UDP (porta 9910), in sola lettura: handshake, ACK dei pacchetti
  affidabili, lettura di `_pin` (modello), `InPr` (nomi ingressi), `TlSr` (tally program/preview per sorgente),
  `PrgI`, `FtbS`. Lo stato è valido dopo `InCm`. Nessun pacchetto per 5 s = ATEM non raggiungibile, nuovo tentativo ogni secondo.
- Ogni cambio arriva all'hub (`SetTally`), che calcola la fascia per ogni sessione (`tally.ForUser`) e la invia solo se cambiata.
- Nella fascia "in onda: …" contano solo gli ingressi esterni (camere); se in onda non ce n'è nessuno, il nome della sorgente sul program.

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
| `GET /api/tally` | sì | stato ATEM in tempo reale: connesso, modello, ingressi, program, preview, nero |
| `PUT /api/tally` | sì | impostazioni tally `{enabled, interface, host}` (host = IP) |
| `PUT /api/tally/users/{id}` | sì | assegna l'ingresso ATEM `{input}` a un utente (0 = nessuno) |
| `GET /api/export` | sì | scarica la configurazione completa (hash delle password inclusi) come `intercom-config.json` |
| `POST /api/import` | sì | sostituisce tutta la configurazione con un file esportato; la convalida e poi riapplica a client, mDNS e tally. Se il file non ha `adminHash` resta la password admin attuale |

La pagina admin è HTML e JavaScript senza framework, incorporata nel binario con `embed`.

## Avvio, build e Docker

Vedi [server/README.md](../server/README.md) e [sviluppo.md](sviluppo.md).
