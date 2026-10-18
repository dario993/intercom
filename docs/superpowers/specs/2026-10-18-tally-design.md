# Tally — design

Data: 2026-10-18 · Stato: in approvazione

## Perché

In regia c'è un mixer **Blackmagic ATEM Production Studio 4K**. Gli operatori di camera usano già l'app
Android dell'intercom: vogliamo che lo stesso telefono faccia da **tally**, cioè dica in modo
inequivocabile "sei in onda" / "stai per andare in onda", e a chi non è in onda dica chi lo è.

## Obiettivi

- Il server Go si collega all'ATEM e conosce in tempo reale program, preview, nomi degli ingressi, fade to black.
- Dalla pagina admin si configura il tally e si assegna a ogni utente (opzionale) un ingresso ATEM.
- L'app Android mostra una **fascia superiore** con lo stato tally, solo agli utenti con un ingresso assegnato.

## Non obiettivi (per ora)

- Client desktop di regia: riceve il messaggio di protocollo ma non lo mostra.
- Altre interfacce tally (TSL, vMix, ecc.): la tendina esiste ma contiene solo "Blackmagic ATEM".
- Più ingressi per utente, M/E multipli, AUX, stati di registrazione/streaming (la Production Studio 4K non li ha).
- Comandare l'ATEM: il server legge soltanto.

## Anti-obiettivi (sarebbe un fallimento anche se "funziona")

- Un operatore che crede di essere fuori onda mentre è in onda. Se il tally non è affidabile, si dice
  chiaramente ("TALLY NON DISPONIBILE"), non si mostra uno stato vecchio.
- La schermata che salta: la fascia ha altezza fissa e quando c'è non sparisce al cambio di stato.
- Rallentare o disturbare l'audio dell'intercom.

## Pagina admin — Impostazioni → sezione "Tally"

Nella scheda Impostazioni esistente (`server/internal/web/static/`), prima della password admin. Stesso stile.

- **Tally abilitato** (casella).
- **Interfaccia Tally**: tendina, unica voce "Blackmagic ATEM" (predefinita).
- **Indirizzo IP del mixer**: un campo. Porta UDP 9910 fissa, mostrata come nota.
- **Stato connessione**: pallino verde/rosso + nome del modello letto dall'ATEM ("Connesso · ATEM Production Studio 4K")
  oppure "Non raggiungibile".
- **Salva**.
- **Assegnazione ID Tally**: tabella con una riga per utente.
  - Colonna "Ingresso ATEM": tendina con "— nessuno —" e gli ingressi dell'ATEM come `numero · nome` (es. `3 · CAM 3`).
    Se l'ATEM non è raggiungibile, la tendina offre comunque i numeri 1–8.
  - Colonna "Ora": badge in tempo reale `IN ONDA` (rosso) / `PREVIEW` (verde) per verificare le assegnazioni.
  - Ogni utente ha **al massimo un** ingresso. Più utenti sullo stesso ingresso: ammesso (scelta mia, non vietarlo).
- Il cambio di assegnazione o di impostazioni arriva subito ai telefoni, senza nuovo login.

## App Android — fascia superiore

**Quando c'è**: tally abilitato sul server **e** utente con ingresso assegnato. Altrimenti nessuna fascia,
schermata identica a oggi.

**Dove**: in cima alla schermata principale, sopra l'intestazione (nome utente, orologio). Altezza fissa
(~44 dp nella bozza), larghezza piena, testo bianco, maiuscolo, grassetto. Nessun bordo intorno allo schermo.

**Stati**, in ordine di priorità:

| Stato | Fascia | Testo |
|---|---|---|
| Telefono disconnesso dal server | nera, testo **rosso** | `DISCONNESSO` |
| ATEM non raggiungibile dal server | grigia | `TALLY NON DISPONIBILE` |
| Fade to black completato (schermo del tutto nero, nessuna dissolvenza in corso) | grigia | `NERO · nessuno in onda` |
| Il mio ingresso è in program | rossa (`OnAirRed`) | `● IN ONDA` |
| Il mio ingresso è in preview | verde (`VoxGreen`) | `PREVIEW` |
| Altro in onda, un ingresso | nera | `IN ONDA: <nome ATEM> <utente>` — es. `IN ONDA: CAM 3 Mario`; senza utente assegnato solo il nome ATEM (`IN ONDA: GRAFICA`) |
| Altro in onda, più ingressi (dissolvenza, keyer, PiP) | nera | `IN ONDA: CAM 2 + CAM 3` — solo nomi ATEM |

- Durante la dissolvenza verso il nero o dal nero si resta "in onda" (scelta mia: meglio un falso IN ONDA che un falso NERO).
- Nella fascia nera si elencano solo gli **ingressi esterni** (camere/SDI) in onda: media player, colori e sorgenti dei keyer (es. un logo fisso) non compaiono. Se nessun ingresso esterno è in onda, si mostra il nome della sorgente sul program (es. `IN ONDA: MP1`). Scelta mia.
- Program vince su preview (durante una transizione un ingresso può essere in entrambi).
- In program con altri ingressi insieme (es. a metà dissolvenza): chi è uno di quelli vede comunque `● IN ONDA`.
- Il nome utente nella fascia nera è quello assegnato all'ingresso in onda (se più utenti: il primo — scelta mia).
- Testo troppo lungo: troncato con "…" su una riga, la fascia non cresce.

Bozze visive approvate in `.superpowers/brainstorm/82031-1792353926/content/` (`tally-schermo-v2.html`, `tally-multi.html`, `server-tally.html`).

## Rilevamento della disconnessione (deciso)

- Il telefono deve accorgersi di aver perso il server in **2–3 secondi** (oggi ~10 s). Vale per tutta l'app, non solo per il tally:
  ping più frequenti e timeout più corto lato client, e timeout coerente lato server (oggi 10 s, controllo ogni 2 s).
  Valori esatti al builder, dentro la finestra 2–3 s; aggiornare `docs/protocollo.md`.
- In disconnessione/riconnessione la fascia (se l'utente la vedeva) resta visibile con `DISCONNESSO`; non sparisce.
  Dopo la riconnessione il server rimanda lo stato tally aggiornato.

## Dopo il tally (progetto separato)

Annuncio vocale sul telefono "DISCONNESSO" / "CONNESSO" per tutti gli utenti. Si appoggia sul rilevamento 2–3 s qui sopra;
si progetta dopo aver chiuso il tally.

## Come si fa (decisioni prese)

- **Server**: client ATEM scritto in Go con la sola libreria standard, nuovo pacchetto `server/internal/tally`.
  Protocollo UDP 9910 non documentato ufficialmente ma ricostruito e stabile (Bitfocus Companion,
  `nrkno/sofie-atem-connection`, OpenSwitcher). Dati usati:
  - handshake: header di 12 byte (flag, lunghezza, session id, ack, sequenze), pacchetto SYN, risposta, ACK;
    i pacchetti "reliable" dell'ATEM vanno confermati con ACK, altrimenti il mixer chiude la sessione.
  - `TlSr` (tally per sorgente: u16 conteggio, poi per sorgente u16 id + u8 bit0=program bit1=preview) —
    è il tally "vero" che include keyer e transizioni.
  - `InPr` (nomi degli ingressi), `_pin` (nome del modello), `FtbS` (fade to black).
  - Riconnessione automatica; dopo qualche secondo senza pacchetti → stato "non raggiungibile".
- **Il server calcola lo stato per ogni utente** e invia ai client un messaggio già pronto da mostrare,
  così Android (e domani il desktop) non conoscono l'ATEM. Proposta: nuovo messaggio WebSocket
  `tally` = `{state: "off"|"program"|"preview"|"other"|"black"|"unavailable", text: "..."}`,
  inviato al login e a ogni cambiamento che riguarda quell'utente. `off` = nascondi la fascia.
  Il formato preciso è del builder; va aggiornato su **entrambi** i lati del protocollo e in `docs/protocollo.md`.
- **Configurazione** (`server/internal/config`): `settings.tally = {enabled, interface: "atem", host}`,
  `user.tallyInput` (0 = nessuno). Le API admin esistenti (`PUT /api/settings`, `PUT /api/users/{id}`) si estendono;
  per stato ATEM, ingressi e program/preview live serve un endpoint di lettura (es. dentro `GET /api/live`).
- **Android**: componente fascia in `MainScreen.kt`, colori da `Theme.kt`; stato tally in `UiState`
  attraverso la logica di connessione in `shared/`.

## Collaudo

L'ATEM **non è disponibile in rete durante lo sviluppo** e il firmware non è noto. Quindi:
- un finto ATEM in Go (stesso protocollo UDP 9910: handshake, ACK, invio di `_pin`, `InPr`, `TlSr`, `FtbS`)
  usato dai test e avviabile a mano per provare telefoni e pagina admin;
- test di decodifica su pacchetti costruiti secondo le fonti citate (non registrati dal vero mixer);
- passo finale di **collaudo sul posto** con il mixer vero: connessione, riconnessione dopo stacco del cavo,
  nomi degli ingressi, dissolvenza, fade to black, convivenza con ATEM Software Control acceso.

## Lasciato al builder

- Formato esatto del messaggio `tally` e degli endpoint admin.
- Timeout di "non raggiungibile" e intervallo di riconnessione.
- Quali ingressi ATEM mostrare nella tendina (solo esterni o tutti).
- Test: un test Go che decodifica pacchetti ATEM d'esempio e calcola lo stato per utente; un test JVM in `shared/` sul messaggio.
- Aggiornare `docs/server.md`, `docs/protocollo.md`, `docs/android.md`.
