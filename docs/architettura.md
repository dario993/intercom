# Architettura

## Componenti

```
 Telefono A ──WebSocket /ws (controllo, JSON)──┐
            ──UDP 8081 (voce Opus)────────────┤
                                              ├── Server Go ── pagina admin (browser, :8080)
 Telefono B ──WebSocket /ws───────────────────┤      │
            ──UDP 8081────────────────────────┘      └── intercom-config.json
```

- **Server** (`server/`): binario Go unico, senza cgo, per macOS/Windows/Linux (anche Docker).
  Gestisce login, permessi e sessioni, **inoltra** i pacchetti voce e serve la pagina admin.
  Si annuncia in rete con mDNS (`_intercom._tcp`).
- **App Android** (`android/`): si collega via WebSocket per il controllo e via UDP per la voce.
  Codifica e decodifica Opus con `MediaCodec` e **mixa localmente** le voci che riceve.

La rete locale è considerata fidata: nella tappa 1 il traffico non è cifrato.

## Flusso audio

Formato: Opus, 48 kHz, mono, frame da 20 ms, ~32 kbit/s.

1. Il telefono cattura 20 ms di audio, il VOX misura il livello e il controller decide se il frame va in onda.
2. Se va in onda, il frame viene codificato in Opus e inviato al server via UDP.
3. Il server **non decodifica e non mixa**: inoltra una copia del pacchetto a ogni ascoltatore idoneo,
   escluso chi parla, etichettandola con il canale.
4. Ogni telefono mette i pacchetti in un jitter buffer per ciascuna voce (60 ms), li decodifica,
   applica il volume del canale dal mixer, somma le voci e le riproduce.

## Chi riceve un pacchetto

Dipende dal *target* corrente di chi parla (`server/internal/hub/route.go`):

| Target | Chi riceve | Etichetta canale |
|---|---|---|
| `channels` | utenti online che hanno in **Ascolta** almeno uno dei canali in **Parla** di chi parla | il primo canale corrispondente nell'ordine dell'ascoltatore |
| `all` (chiamata a tutti) | tutti gli utenti online | `0` |
| `user` (chiamata privata) | solo l'utente indicato | `0xFFFF` |

- Ogni ascoltatore riceve **una sola copia** di ogni pacchetto.
- Il server scarta l'audio se il target non è permesso: nessun canale Parla, `allCall` o `privateCall` assente,
  oppure destinatario offline o uguale a chi parla.
- Riceve solo chi ha già inviato almeno un pacchetto UDP: il keepalive serve a far conoscere al server il suo indirizzo.

## Permessi

Assegnati dall'admin per ciascun utente:
- per ogni canale assegnato: **parla** e/o **ascolta**;
- `allCall`: può fare la chiamata a tutti;
- `privateCall`: può chiamare un singolo utente.

Una modifica dell'admin si applica subito: i client ricevono `config` e le trasmissioni non più consentite vengono interrotte.

## Roadmap

| Tappa | Contenuto | Stato |
|---|---|---|
| 1 | Nucleo intercom: server + app Android | **fatta** (ramo `tappa1-android`) |
| 2 | Gruppi di canali | futura |
| 3 | Program feed (L/R in cuffia), I/O verso la scheda audio del server (mix lato server) | futura |
| 4 | Tally e GPIO | **esclusa** |
| 5 | Cifratura AES-256-GCM del traffico, backup della configurazione | futura |
| 6 | App iPhone (Swift) | futura |

Fuori da tutte le tappe pianificate: licenze e acquisti, aggiornamenti automatici.
