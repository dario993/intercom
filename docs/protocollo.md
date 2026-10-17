# Protocollo

È definito in due posti che devono restare allineati:
- server: `server/internal/proto/proto.go`
- Android: `android/app/src/main/java/app/intercom/android/proto/` (`Messages.kt` JSON, `Packets.kt` UDP)

## Controllo: WebSocket `ws://<host>:8080/ws`

Messaggi JSON `{ "type": ..., ... }`.

### Client → server

| type | campi | note |
|---|---|---|
| `hello` | `username`, `password`, `device` | primo messaggio, entro 10 s, altrimenti il server chiude |
| `state` | `talk: [channelId]`, `listen: [channelId]` | canali attivi; il server tiene solo quelli permessi |
| `ptt` | `active: bool`, `target: {kind: "channels"\|"all"\|"user", userId?}` | inizio/fine trasmissione |
| `ping` | — | ogni 3 s; il server risponde `pong` |

### Server → client

| type | campi | note |
|---|---|---|
| `welcome` | `userId`, `session` (uint32), `udpPort`, `serverName`, `channels`, `perms` | login riuscito |
| `config` | `channels`, `perms` | l'admin ha modificato l'utente o i canali |
| `users` | `users: [{id, name, online, talking}]` | a ogni cambiamento; solo utenti abilitati |
| `talking` | `userId`, `name`, `active`, `target`, `channelIds` | chi parla e dove; per le chiamate private lo riceve solo il destinatario |
| `kicked` | `reason`: `replaced` \| `admin` \| `disabled` | poi il server chiude |
| `error` | `code`: `bad_credentials` \| `disabled`, `message` | poi il server chiude |
| `pong` | — | risposta a `ping` |

- `channels` = `[{id, name, color, canTalk, canListen}]` nell'ordine assegnato all'utente.
- `perms` = `{allCall, privateCall}`.
- **Login duplicato**: vince l'ultimo; la sessione precedente riceve `kicked{reason:"replaced"}`.
- **Timeout**: il server chiude una sessione senza ping né pacchetti da più di 10 s (controllo ogni 2 s).
  Il client considera persa la connessione se non riceve nulla per 10 s, oppure se `welcome` non arriva entro 10 s.
- Coda di uscita di 64 messaggi per sessione: un client che resta indietro viene disconnesso.

## Audio: UDP (porta predefinita 8081)

Interi big-endian.

### Client → server

```
u8  kind      1 = voce, 2 = keepalive
u32 session   dal messaggio welcome
u16 seq       solo per la voce, cresce di 1 a ogni frame
... payload Opus (solo kind=1)
```

Il client invia un keepalive subito dopo `welcome` e poi ogni 5 s. Il server associa alla sessione
l'indirizzo sorgente dell'ultimo pacchetto valido, quindi sopporta i cambi di IP o di porta (NAT).

### Server → client

```
u8  kind      1 = voce
u16 talkerId  id utente di chi parla
u16 channelId 0 = chiamata a tutti, 0xFFFF = chiamata privata, altrimenti id canale
u16 seq       sequenza originale di chi parla
... payload Opus
```

Gli id `0` e `0xFFFF` sono riservati: il server non li assegna mai a utenti o canali.
