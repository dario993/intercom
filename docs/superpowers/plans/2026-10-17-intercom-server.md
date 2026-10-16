# Intercom Server (Tappa 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Server intercom in Go (binario unico, multipiattaforma) con WebSocket di controllo, inoltro audio UDP, pagina web admin, annuncio mDNS e client di prova.

**Architecture:** Un `hub` in memoria tiene le sessioni dei client e decide l'instradamento dei pacchetti voce (inoltro, nessun mix). La configurazione vive in un file JSON gestito da `config.Store` con scrittura atomica. Il pacchetto `web` espone `/ws` (client) e `/api/*` + file statici (admin). `main.go` collega tutto e apre le porte.

**Tech Stack:** Go ≥ 1.22 (ServeMux con metodi e `{id}`), `github.com/coder/websocket`, `golang.org/x/crypto/bcrypt`, `github.com/grandcat/zeroconf`, HTML/CSS/JS senza framework.

**Spec:** `docs/superpowers/specs/2026-10-17-intercom-tappa1-design.md`

Questo è il piano 1 di 2 per la tappa 1. Il piano 2 (app Android) si scrive dopo, sul protocollo implementato qui.

## Global Constraints

- Go puro, nessun cgo: `CGO_ENABLED=0` deve compilare per darwin, windows, linux.
- Porte predefinite: HTTP `8080`, UDP `8081`.
- ID canale `0` e `0xFFFF` riservati sul filo (`0` = chiamata a tutti, `0xFFFF` = chiamata privata): mai assegnati a canali.
- Pacchetti UDP big-endian. Client→server: `u8 kind | u32 session | u16 seq | payload`. Server→client: `u8 kind | u16 talkerId | u16 channelId | u16 seq | payload`. `kind` 1 = voce, 2 = keepalive.
- Nomi dei campi JSON esattamente come nella spec (camelCase: `userId`, `udpPort`, `canTalk`, `allCall`, …).
- Password (utenti e admin) solo come hash bcrypt; gli hash non escono mai dalle API.
- Client offline dopo 10 s senza ping o pacchetti UDP.
- Testi visibili all'utente in italiano.
- Nessuna grafica, testo o asset di Unity Intercom.

## Review Focus

1. Nome utente duplicato con maiuscole diverse ("Anna" / "anna") → rifiutato con 409 (test in Task 4).
2. Canale eliminato mentre qualcuno ci sta parlando → nessun pacchetto viene più inoltrato su quel canale (test in Task 3).
3. Pacchetti UDP corti, malformati o con sessione sconosciuta/scaduta → ignorati senza crash (test in Task 3).
4. `POST /api/setup` richiamato dopo il primo avvio → 403, la password admin non si sovrascrive senza login (test in Task 4).
5. Accessi concorrenti (WebSocket, UDP, admin) → tutti i test girano con `-race` (Task 6).

---

### Task 1: Setup progetto e pacchetto `proto`

**Files:**
- Create: `.gitignore`
- Create: `server/go.mod`
- Create: `server/internal/proto/proto.go`
- Test: `server/internal/proto/proto_test.go`

**Interfaces:**
- Produces:
  - costanti `KindVoice=1`, `KindKeepalive=2`, `ChanAll uint16=0`, `ChanPrivate uint16=0xFFFF`
  - `type ClientPacket struct{Kind uint8; Session uint32; Seq uint16; Payload []byte}` con `Marshal() []byte`, `ParseClient([]byte) (ClientPacket, error)`
  - `type ServerPacket struct{Kind uint8; TalkerID, ChannelID, Seq uint16; Payload []byte}` con `Marshal() []byte`, `ParseServer([]byte) (ServerPacket, error)`
  - `var ErrShort error`
  - costanti tipo messaggio `MsgHello, MsgState, MsgPTT, MsgPing, MsgWelcome, MsgConfig, MsgUsers, MsgTalking, MsgKicked, MsgError, MsgPong`
  - costanti `TargetChannels, TargetAll, TargetUser`, `ErrBadCredentials, ErrDisabled`, `KickReplaced, KickAdmin, KickDisabled`
  - struct JSON `Target, ClientMsg, ChannelInfo, Perms, UserStatus, Welcome, ConfigMsg, UsersMsg, Talking, Kicked, ErrorMsg, Pong`

- [ ] **Step 1: Installa Go e inizializza il repository**

```bash
brew install go
go version   # deve essere >= go1.22
cd /Users/dario/Documents/APP/Intercom
git init
mkdir -p server/internal/proto
cd server && go mod init intercom
```

Crea `/Users/dario/Documents/APP/Intercom/.gitignore`:

```gitignore
.DS_Store
server/intercom-server
server/dist/
server/intercom-config.json
```

- [ ] **Step 2: Scrivi il test che fallisce**

`server/internal/proto/proto_test.go`:

```go
package proto

import (
	"bytes"
	"testing"
)

func TestClientPacketRoundTrip(t *testing.T) {
	in := ClientPacket{Kind: KindVoice, Session: 0xDEADBEEF, Seq: 513, Payload: []byte{9, 8, 7}}
	b := in.Marshal()
	if want := []byte{1, 0xDE, 0xAD, 0xBE, 0xEF, 0x02, 0x01, 9, 8, 7}; !bytes.Equal(b, want) {
		t.Fatalf("Marshal = %v, want %v", b, want)
	}
	out, err := ParseClient(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != in.Kind || out.Session != in.Session || out.Seq != in.Seq || !bytes.Equal(out.Payload, in.Payload) {
		t.Errorf("ParseClient = %+v, want %+v", out, in)
	}
}

func TestServerPacketRoundTrip(t *testing.T) {
	in := ServerPacket{Kind: KindVoice, TalkerID: 10, ChannelID: ChanPrivate, Seq: 7, Payload: []byte{1}}
	b := in.Marshal()
	if want := []byte{1, 0, 10, 0xFF, 0xFF, 0, 7, 1}; !bytes.Equal(b, want) {
		t.Fatalf("Marshal = %v, want %v", b, want)
	}
	out, err := ParseServer(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.TalkerID != 10 || out.ChannelID != ChanPrivate || out.Seq != 7 || !bytes.Equal(out.Payload, []byte{1}) {
		t.Errorf("ParseServer = %+v", out)
	}
}

func TestShortPacketsRejected(t *testing.T) {
	if _, err := ParseClient([]byte{1, 2, 3}); err != ErrShort {
		t.Errorf("ParseClient short: err = %v", err)
	}
	if _, err := ParseServer(nil); err != ErrShort {
		t.Errorf("ParseServer nil: err = %v", err)
	}
}
```

- [ ] **Step 3: Verifica che fallisca**

Run: `cd server && go test ./internal/proto/`
Expected: FAIL, errori di compilazione `undefined: ClientPacket`

- [ ] **Step 4: Implementa**

`server/internal/proto/proto.go`:

```go
// Package proto defines the wire format shared by server and clients.
package proto

import (
	"encoding/binary"
	"errors"
)

const (
	KindVoice     = 1
	KindKeepalive = 2

	ChanAll     uint16 = 0
	ChanPrivate uint16 = 0xFFFF

	headerLen = 7
)

var ErrShort = errors.New("packet too short")

// ClientPacket is sent by a client: u8 kind | u32 session | u16 seq | payload.
type ClientPacket struct {
	Kind    uint8
	Session uint32
	Seq     uint16
	Payload []byte
}

func (p ClientPacket) Marshal() []byte {
	b := make([]byte, headerLen+len(p.Payload))
	b[0] = p.Kind
	binary.BigEndian.PutUint32(b[1:], p.Session)
	binary.BigEndian.PutUint16(b[5:], p.Seq)
	copy(b[headerLen:], p.Payload)
	return b
}

// ParseClient decodes b; the returned Payload aliases b.
func ParseClient(b []byte) (ClientPacket, error) {
	if len(b) < headerLen {
		return ClientPacket{}, ErrShort
	}
	return ClientPacket{
		Kind:    b[0],
		Session: binary.BigEndian.Uint32(b[1:]),
		Seq:     binary.BigEndian.Uint16(b[5:]),
		Payload: b[headerLen:],
	}, nil
}

// ServerPacket is sent by the server: u8 kind | u16 talkerId | u16 channelId | u16 seq | payload.
type ServerPacket struct {
	Kind      uint8
	TalkerID  uint16
	ChannelID uint16
	Seq       uint16
	Payload   []byte
}

func (p ServerPacket) Marshal() []byte {
	b := make([]byte, headerLen+len(p.Payload))
	b[0] = p.Kind
	binary.BigEndian.PutUint16(b[1:], p.TalkerID)
	binary.BigEndian.PutUint16(b[3:], p.ChannelID)
	binary.BigEndian.PutUint16(b[5:], p.Seq)
	copy(b[headerLen:], p.Payload)
	return b
}

// ParseServer decodes b; the returned Payload aliases b.
func ParseServer(b []byte) (ServerPacket, error) {
	if len(b) < headerLen {
		return ServerPacket{}, ErrShort
	}
	return ServerPacket{
		Kind:      b[0],
		TalkerID:  binary.BigEndian.Uint16(b[1:]),
		ChannelID: binary.BigEndian.Uint16(b[3:]),
		Seq:       binary.BigEndian.Uint16(b[5:]),
		Payload:   b[headerLen:],
	}, nil
}

// Control message types (WebSocket JSON "type" field).
const (
	MsgHello   = "hello"
	MsgState   = "state"
	MsgPTT     = "ptt"
	MsgPing    = "ping"
	MsgWelcome = "welcome"
	MsgConfig  = "config"
	MsgUsers   = "users"
	MsgTalking = "talking"
	MsgKicked  = "kicked"
	MsgError   = "error"
	MsgPong    = "pong"
)

const (
	TargetChannels = "channels"
	TargetAll      = "all"
	TargetUser     = "user"

	ErrBadCredentials = "bad_credentials"
	ErrDisabled       = "disabled"

	KickReplaced = "replaced"
	KickAdmin    = "admin"
	KickDisabled = "disabled"
)

type Target struct {
	Kind   string `json:"kind"`
	UserID uint16 `json:"userId,omitempty"`
}

// ClientMsg is any message a client sends; only the fields of its Type are set.
type ClientMsg struct {
	Type     string   `json:"type"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	Device   string   `json:"device,omitempty"`
	Talk     []uint16 `json:"talk,omitempty"`
	Listen   []uint16 `json:"listen,omitempty"`
	Active   bool     `json:"active,omitempty"`
	Target   Target   `json:"target"`
}

type ChannelInfo struct {
	ID        uint16 `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CanTalk   bool   `json:"canTalk"`
	CanListen bool   `json:"canListen"`
}

type Perms struct {
	AllCall     bool `json:"allCall"`
	PrivateCall bool `json:"privateCall"`
}

type UserStatus struct {
	ID      uint16 `json:"id"`
	Name    string `json:"name"`
	Online  bool   `json:"online"`
	Talking bool   `json:"talking"`
}

type Welcome struct {
	Type       string        `json:"type"`
	UserID     uint16        `json:"userId"`
	Session    uint32        `json:"session"`
	UDPPort    int           `json:"udpPort"`
	ServerName string        `json:"serverName"`
	Channels   []ChannelInfo `json:"channels"`
	Perms      Perms         `json:"perms"`
}

type ConfigMsg struct {
	Type     string        `json:"type"`
	Channels []ChannelInfo `json:"channels"`
	Perms    Perms         `json:"perms"`
}

type UsersMsg struct {
	Type  string       `json:"type"`
	Users []UserStatus `json:"users"`
}

type Talking struct {
	Type       string   `json:"type"`
	UserID     uint16   `json:"userId"`
	Name       string   `json:"name"`
	Active     bool     `json:"active"`
	Target     Target   `json:"target"`
	ChannelIDs []uint16 `json:"channelIds"`
}

type Kicked struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type ErrorMsg struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Pong struct {
	Type string `json:"type"`
}
```

- [ ] **Step 5: Verifica che passi**

Run: `cd server && go test ./internal/proto/`
Expected: `ok  intercom/internal/proto`

- [ ] **Step 6: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add .gitignore docs server
git commit -m "feat(server): protocol package and project setup

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Configurazione persistente (`config`)

**Files:**
- Create: `server/internal/config/config.go`
- Test: `server/internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Channel struct{ID uint16; Name, Color string}` (json `id,name,color`)
  - `type Assignment struct{ChannelID uint16; Talk, Listen bool}` (json `channelId,talk,listen`)
  - `type User struct{ID uint16; Name, PasswordHash string; Enabled bool; Channels []Assignment; AllCall, PrivateCall bool}`
  - `type Settings struct{ServerName string; HTTPPort, UDPPort int; AdminHash string}`
  - `type Config struct{Settings Settings; Channels []Channel; Users []User; NextID uint16}`
  - `Default() *Config`, `(*Config).NewID() uint16`, `(*Config).UserByName(string) *User` (case-insensitive), `(*Config).UserByID(uint16) *User`, `(*Config).ChannelByID(uint16) *Channel`
  - `HashPassword(string) (string, error)`, `CheckPassword(hash, password string) bool`
  - `Load(path) (*Config, error)`, `Save(path, *Config) error`
  - `type Store`; `Open(path) (*Store, error)`, `(*Store).View(func(*Config))`, `(*Store).Update(func(*Config) error) error`

- [ ] **Step 1: Scrivi il test che fallisce**

`server/internal/config/config_test.go`:

```go
package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Settings.HTTPPort != 8080 || c.Settings.UDPPort != 8081 || c.Settings.ServerName != "Intercom" || c.NextID != 1 {
		t.Errorf("defaults = %+v", c)
	}
}

func TestStoreUpdatePersistsAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(c *Config) error {
		c.Channels = append(c.Channels, Channel{ID: c.NewID(), Name: "Regia", Color: "#ff0000"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Channels) != 1 || again.Channels[0].ID != 1 || again.NextID != 2 {
		t.Errorf("reloaded = %+v", again)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestStoreUpdateErrorKeepsOldConfig(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err = s.Update(func(c *Config) error {
		c.Settings.ServerName = "Changed"
		return boom
	})
	if err != boom {
		t.Fatalf("err = %v", err)
	}
	s.View(func(c *Config) {
		if c.Settings.ServerName != "Intercom" {
			t.Errorf("failed update leaked: %q", c.Settings.ServerName)
		}
	})
}

func TestPasswordsAndLookups(t *testing.T) {
	h, err := HashPassword("segreta")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "segreta") || CheckPassword(h, "altra") || CheckPassword("", "") {
		t.Error("CheckPassword wrong")
	}
	c := Default()
	c.Users = []User{{ID: 5, Name: "Anna"}}
	c.Channels = []Channel{{ID: 6, Name: "Luci"}}
	if c.UserByName("anna") == nil || c.UserByID(5) == nil || c.UserByID(6) != nil || c.ChannelByID(6) == nil {
		t.Error("lookups wrong")
	}
}
```

- [ ] **Step 2: Verifica che fallisca**

Run: `cd server && go test ./internal/config/`
Expected: FAIL, `undefined: Load`

- [ ] **Step 3: Implementa**

```bash
cd server && go get golang.org/x/crypto/bcrypt
```

`server/internal/config/config.go`:

```go
// Package config holds the server configuration and persists it as JSON.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

type Channel struct {
	ID    uint16 `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Assignment struct {
	ChannelID uint16 `json:"channelId"`
	Talk      bool   `json:"talk"`
	Listen    bool   `json:"listen"`
}

type User struct {
	ID           uint16       `json:"id"`
	Name         string       `json:"name"`
	PasswordHash string       `json:"passwordHash"`
	Enabled      bool         `json:"enabled"`
	Channels     []Assignment `json:"channels"`
	AllCall      bool         `json:"allCall"`
	PrivateCall  bool         `json:"privateCall"`
}

type Settings struct {
	ServerName string `json:"serverName"`
	HTTPPort   int    `json:"httpPort"`
	UDPPort    int    `json:"udpPort"`
	AdminHash  string `json:"adminHash"`
}

type Config struct {
	Settings Settings  `json:"settings"`
	Channels []Channel `json:"channels"`
	Users    []User    `json:"users"`
	NextID   uint16    `json:"nextId"`
}

func Default() *Config {
	return &Config{
		Settings: Settings{ServerName: "Intercom", HTTPPort: 8080, UDPPort: 8081},
		NextID:   1,
	}
}

// NewID returns a fresh id for a user or channel. 0 and 0xFFFF are reserved on the wire.
// ponytail: no reuse of freed ids, 65k creations before wrap-around collisions.
func (c *Config) NewID() uint16 {
	if c.NextID == 0 || c.NextID == 0xFFFF {
		c.NextID = 1
	}
	id := c.NextID
	c.NextID++
	return id
}

func (c *Config) UserByName(name string) *User {
	for i := range c.Users {
		if strings.EqualFold(c.Users[i].Name, name) {
			return &c.Users[i]
		}
	}
	return nil
}

func (c *Config) UserByID(id uint16) *User {
	for i := range c.Users {
		if c.Users[i].ID == id {
			return &c.Users[i]
		}
	}
	return nil
}

func (c *Config) ChannelByID(id uint16) *Channel {
	for i := range c.Channels {
		if c.Channels[i].ID == id {
			return &c.Channels[i]
		}
	}
	return nil
}

func HashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, p string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

// Load reads path; a missing file yields the defaults.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Save writes c to path atomically: temp file in the same directory, fsync, rename.
func Save(path string, c *Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".intercom-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Store guards the live configuration. View callbacks must not call back into code that takes other locks.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  *Config
}

func Open(path string) (*Store, error) {
	c, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, cfg: c}, nil
}

func (s *Store) View(fn func(c *Config)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.cfg)
}

// Update runs fn on a copy; the copy is saved and kept only if fn and the save both succeed.
func (s *Store) Update(fn func(c *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := clone(s.cfg)
	if err != nil {
		return err
	}
	if err := fn(c); err != nil {
		return err
	}
	if err := Save(s.path, c); err != nil {
		return err
	}
	s.cfg = c
	return nil
}

func clone(c *Config) (*Config, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	n := &Config{}
	return n, json.Unmarshal(b, n)
}
```

- [ ] **Step 4: Verifica che passi**

Run: `cd server && go test ./internal/config/`
Expected: `ok  intercom/internal/config`

- [ ] **Step 5: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add server
git commit -m "feat(server): persistent JSON configuration store

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Hub — sessioni e instradamento audio

**Files:**
- Create: `server/internal/hub/route.go`
- Create: `server/internal/hub/hub.go`
- Test: `server/internal/hub/route_test.go`
- Test: `server/internal/hub/hub_test.go`

**Interfaces:**
- Consumes: `config.Store`, `config.Config` lookups, `config.CheckPassword` (Task 2); tutto `proto` (Task 1).
- Produces:
  - `type Peer struct{UserID uint16; Listen []uint16}`, `type Delivery struct{UserID, ChannelID uint16}`
  - `Route(talker uint16, t proto.Target, talk []uint16, peers []Peer) []Delivery`
  - `type Session struct{ID uint32; UserID uint16; Out chan any; ...}` — `Out` riceve i messaggi `proto.*` da inviare via WebSocket ed è **chiuso** quando la sessione termina
  - `New(*config.Store) *Hub`; campo `SendUDP func(*net.UDPAddr, []byte)`
  - `(*Hub).Login(username, password string) (*Session, *proto.ErrorMsg)`
  - `(*Hub).Logout(*Session)`, `SetState(*Session, talk, listen []uint16)`, `SetPTT(*Session, active bool, t proto.Target)`, `Ping(*Session)`
  - `(*Hub).HandleUDP(addr *net.UDPAddr, b []byte)` — non conserva `b` dopo il ritorno
  - `(*Hub).ConfigChanged()`, `Kick(userID uint16, reason string)`, `Reap(timeout time.Duration)`
  - `type LiveUser struct{proto.UserStatus; Target *proto.Target; Talk []uint16}` (json `target,omitempty`, `talk`), `(*Hub).Live() []LiveUser`

- [ ] **Step 1: Scrivi i test che falliscono**

`server/internal/hub/route_test.go`:

```go
package hub

import (
	"reflect"
	"testing"

	"intercom/internal/proto"
)

func TestRouteDeduplicatesByListenerOrder(t *testing.T) {
	peers := []Peer{
		{UserID: 1, Listen: []uint16{5}},    // the talker itself
		{UserID: 2, Listen: []uint16{6, 5}}, // listens to both talk channels
		{UserID: 3, Listen: []uint16{7}},    // unrelated channel
	}
	got := Route(1, proto.Target{Kind: proto.TargetChannels}, []uint16{5, 6}, peers)
	want := []Delivery{{UserID: 2, ChannelID: 6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRouteAllAndPrivate(t *testing.T) {
	peers := []Peer{{UserID: 1}, {UserID: 2}, {UserID: 3}}
	all := Route(1, proto.Target{Kind: proto.TargetAll}, nil, peers)
	if want := []Delivery{{2, proto.ChanAll}, {3, proto.ChanAll}}; !reflect.DeepEqual(all, want) {
		t.Errorf("all = %v, want %v", all, want)
	}
	priv := Route(1, proto.Target{Kind: proto.TargetUser, UserID: 3}, nil, peers)
	if want := []Delivery{{3, proto.ChanPrivate}}; !reflect.DeepEqual(priv, want) {
		t.Errorf("private = %v, want %v", priv, want)
	}
}
```

`server/internal/hub/hub_test.go`:

```go
package hub

import (
	"bytes"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"intercom/internal/config"
	"intercom/internal/proto"
)

const pw = "pw"

// testStore: channels 1 Regia, 2 Luci.
// anna(10): talk+listen on 1 and 2, allCall, privateCall. bruno(11): talk+listen on 1.
// carla(12): listen-only on 2. dario(13): disabled.
func testStore(t *testing.T) *config.Store {
	t.Helper()
	hash, err := config.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	c := config.Default()
	c.Channels = []config.Channel{{ID: 1, Name: "Regia"}, {ID: 2, Name: "Luci"}}
	c.Users = []config.User{
		{ID: 10, Name: "anna", PasswordHash: hash, Enabled: true, AllCall: true, PrivateCall: true,
			Channels: []config.Assignment{{ChannelID: 1, Talk: true, Listen: true}, {ChannelID: 2, Talk: true, Listen: true}}},
		{ID: 11, Name: "bruno", PasswordHash: hash, Enabled: true,
			Channels: []config.Assignment{{ChannelID: 1, Talk: true, Listen: true}}},
		{ID: 12, Name: "carla", PasswordHash: hash, Enabled: true,
			Channels: []config.Assignment{{ChannelID: 2, Talk: false, Listen: true}}},
		{ID: 13, Name: "dario", PasswordHash: hash, Enabled: false},
	}
	c.NextID = 100
	path := filepath.Join(t.TempDir(), "c.json")
	if err := config.Save(path, c); err != nil {
		t.Fatal(err)
	}
	s, err := config.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// fakeNet records the UDP packets the hub sends, keyed by destination port.
type fakeNet struct {
	mu  sync.Mutex
	got map[int][]proto.ServerPacket
}

func newHub(t *testing.T) (*Hub, *fakeNet) {
	t.Helper()
	h := New(testStore(t))
	f := &fakeNet{got: map[int][]proto.ServerPacket{}}
	h.SendUDP = func(a *net.UDPAddr, b []byte) {
		p, err := proto.ParseServer(b)
		if err != nil {
			t.Errorf("hub sent bad packet: %v", err)
		}
		f.mu.Lock()
		f.got[a.Port] = append(f.got[a.Port], p)
		f.mu.Unlock()
	}
	return h, f
}

func addr(port int) *net.UDPAddr { return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port} }

// connect logs name in and registers its UDP address on port.
func connect(t *testing.T, h *Hub, name string, port int) *Session {
	t.Helper()
	s, e := h.Login(name, pw)
	if e != nil {
		t.Fatalf("login %s: %s", name, e.Code)
	}
	h.HandleUDP(addr(port), proto.ClientPacket{Kind: proto.KindKeepalive, Session: s.ID}.Marshal())
	return s
}

func voice(h *Hub, s *Session, port int, seq uint16) {
	h.HandleUDP(addr(port), proto.ClientPacket{Kind: proto.KindVoice, Session: s.ID, Seq: seq, Payload: []byte{1, 2, 3}}.Marshal())
}

// drain returns the queued messages; a closed Out ends the list with "closed".
func drain(s *Session) []any {
	var out []any
	for {
		select {
		case m, ok := <-s.Out:
			if !ok {
				return append(out, "closed")
			}
			out = append(out, m)
		default:
			return out
		}
	}
}

func TestLoginErrors(t *testing.T) {
	h, _ := newHub(t)
	cases := []struct{ user, pass, code string }{
		{"anna", "wrong", proto.ErrBadCredentials},
		{"nessuno", pw, proto.ErrBadCredentials},
		{"dario", pw, proto.ErrDisabled},
	}
	for _, c := range cases {
		if _, e := h.Login(c.user, c.pass); e == nil || e.Code != c.code {
			t.Errorf("%s/%s: got %+v, want %s", c.user, c.pass, e, c.code)
		}
	}
}

func TestLoginWelcome(t *testing.T) {
	h, _ := newHub(t)
	s, e := h.Login("ANNA", pw) // names are case-insensitive
	if e != nil {
		t.Fatal(e.Code)
	}
	msgs := drain(s)
	w, ok := msgs[0].(proto.Welcome)
	if !ok {
		t.Fatalf("first message is %T, want proto.Welcome", msgs[0])
	}
	if w.Type != proto.MsgWelcome || w.UserID != 10 || w.Session != s.ID || w.UDPPort != 8081 || !w.Perms.AllCall || len(w.Channels) != 2 {
		t.Errorf("welcome = %+v", w)
	}
	if w.Channels[0].Name != "Regia" || !w.Channels[0].CanTalk || !w.Channels[0].CanListen {
		t.Errorf("channels = %+v", w.Channels)
	}
}

func TestSecondLoginReplacesFirst(t *testing.T) {
	h, _ := newHub(t)
	first := connect(t, h, "anna", 1010)
	connect(t, h, "anna", 1020)
	msgs := drain(first)
	if msgs[len(msgs)-1] != "closed" {
		t.Fatalf("first session not closed: %v", msgs)
	}
	if k, ok := msgs[len(msgs)-2].(proto.Kicked); !ok || k.Reason != proto.KickReplaced {
		t.Errorf("want kicked/replaced before close, got %v", msgs)
	}
}

func TestVoiceGoesOnlyToListenersOfTalkChannels(t *testing.T) {
	h, f := newHub(t)
	anna, bruno, carla := connect(t, h, "anna", 1010), connect(t, h, "bruno", 1011), connect(t, h, "carla", 1012)
	h.SetState(anna, []uint16{1}, []uint16{1, 2})
	h.SetState(bruno, nil, []uint16{1})
	h.SetState(carla, nil, []uint16{2})
	h.SetPTT(anna, true, proto.Target{Kind: proto.TargetChannels})
	voice(h, anna, 1010, 7)

	got := f.got[1011]
	if len(got) != 1 || got[0].TalkerID != 10 || got[0].ChannelID != 1 || got[0].Seq != 7 || !bytes.Equal(got[0].Payload, []byte{1, 2, 3}) {
		t.Errorf("bruno got %+v", got)
	}
	if len(f.got[1012]) != 0 || len(f.got[1010]) != 0 {
		t.Errorf("carla %v and anna %v must get nothing", f.got[1012], f.got[1010])
	}
}

func TestNoVoiceWithoutPTT(t *testing.T) {
	h, f := newHub(t)
	anna, bruno := connect(t, h, "anna", 1010), connect(t, h, "bruno", 1011)
	h.SetState(anna, []uint16{1}, nil)
	h.SetState(bruno, nil, []uint16{1})
	voice(h, anna, 1010, 1)
	if len(f.got[1011]) != 0 {
		t.Errorf("bruno got %v without PTT", f.got[1011])
	}
}

func TestListenOnlyChannelCannotTalk(t *testing.T) {
	h, f := newHub(t)
	anna, carla := connect(t, h, "anna", 1010), connect(t, h, "carla", 1012)
	h.SetState(anna, nil, []uint16{2})
	h.SetState(carla, []uint16{2}, []uint16{2}) // talk on 2 is not permitted for carla
	h.SetPTT(carla, true, proto.Target{Kind: proto.TargetChannels})
	voice(h, carla, 1012, 1)
	if len(f.got[1010]) != 0 {
		t.Errorf("anna got %v from a listen-only user", f.got[1010])
	}
}

func TestAllCallNeedsPermission(t *testing.T) {
	h, f := newHub(t)
	anna, bruno := connect(t, h, "anna", 1010), connect(t, h, "bruno", 1011)
	connect(t, h, "carla", 1012)

	h.SetPTT(bruno, true, proto.Target{Kind: proto.TargetAll})
	voice(h, bruno, 1011, 1)
	if len(f.got[1010])+len(f.got[1012]) != 0 {
		t.Fatalf("all-call without permission was routed")
	}

	h.SetPTT(anna, true, proto.Target{Kind: proto.TargetAll})
	voice(h, anna, 1010, 2)
	for _, port := range []int{1011, 1012} {
		if got := f.got[port]; len(got) != 1 || got[0].ChannelID != proto.ChanAll {
			t.Errorf("port %d got %+v, want one all-call packet", port, got)
		}
	}
}

func TestPrivateCall(t *testing.T) {
	h, f := newHub(t)
	anna, bruno := connect(t, h, "anna", 1010), connect(t, h, "bruno", 1011)
	connect(t, h, "carla", 1012)

	h.SetPTT(anna, true, proto.Target{Kind: proto.TargetUser, UserID: 12})
	voice(h, anna, 1010, 1)
	if got := f.got[1012]; len(got) != 1 || got[0].ChannelID != proto.ChanPrivate {
		t.Errorf("carla got %+v", got)
	}
	if len(f.got[1011]) != 0 {
		t.Errorf("bruno overheard a private call")
	}

	h.SetPTT(bruno, true, proto.Target{Kind: proto.TargetUser, UserID: 10}) // bruno lacks privateCall
	voice(h, bruno, 1011, 1)
	if len(f.got[1010]) != 0 {
		t.Errorf("private call without permission was routed")
	}
}

func TestBadPacketsAreIgnored(t *testing.T) {
	h, f := newHub(t)
	connect(t, h, "bruno", 1011)
	h.HandleUDP(addr(5000), []byte{1, 2, 3})
	h.HandleUDP(addr(5000), nil)
	h.HandleUDP(addr(5000), proto.ClientPacket{Kind: proto.KindVoice, Session: 0xABCDEF, Payload: []byte{1}}.Marshal())
	if len(f.got) != 0 {
		t.Errorf("bad packets produced output: %v", f.got)
	}
}

func TestConfigChangeKicksDisabledUser(t *testing.T) {
	h, _ := newHub(t)
	bruno := connect(t, h, "bruno", 1011)
	err := h.store.Update(func(c *config.Config) error {
		c.UserByID(11).Enabled = false
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.ConfigChanged()
	msgs := drain(bruno)
	if msgs[len(msgs)-1] != "closed" {
		t.Fatalf("bruno still connected: %v", msgs)
	}
	if k, ok := msgs[len(msgs)-2].(proto.Kicked); !ok || k.Reason != proto.KickDisabled {
		t.Errorf("want kicked/disabled, got %v", msgs)
	}
}

func TestDeletedChannelStopsRouting(t *testing.T) {
	h, f := newHub(t)
	anna, bruno := connect(t, h, "anna", 1010), connect(t, h, "bruno", 1011)
	h.SetState(anna, []uint16{1}, nil)
	h.SetState(bruno, nil, []uint16{1})
	h.SetPTT(anna, true, proto.Target{Kind: proto.TargetChannels})

	err := h.store.Update(func(c *config.Config) error {
		c.Channels = c.Channels[1:] // drop channel 1
		for i := range c.Users {
			c.Users[i].Channels = slices.DeleteFunc(c.Users[i].Channels, func(a config.Assignment) bool { return a.ChannelID == 1 })
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.ConfigChanged()
	voice(h, anna, 1010, 1)
	if len(f.got[1011]) != 0 {
		t.Errorf("voice routed on a deleted channel: %v", f.got[1011])
	}
	var cfgMsg *proto.ConfigMsg
	for _, m := range drain(anna) {
		if c, ok := m.(proto.ConfigMsg); ok {
			cfgMsg = &c
		}
	}
	if cfgMsg == nil || len(cfgMsg.Channels) != 1 || cfgMsg.Channels[0].ID != 2 {
		t.Errorf("anna config = %+v", cfgMsg)
	}
}

func TestReapDropsSilentClients(t *testing.T) {
	h, _ := newHub(t)
	now := time.Now()
	h.now = func() time.Time { return now }
	bruno := connect(t, h, "bruno", 1011)
	now = now.Add(11 * time.Second)
	h.Reap(10 * time.Second)
	if msgs := drain(bruno); msgs[len(msgs)-1] != "closed" {
		t.Errorf("silent client not reaped: %v", msgs)
	}
}

func TestLiveShowsWhoTalksWhere(t *testing.T) {
	h, _ := newHub(t)
	anna := connect(t, h, "anna", 1010)
	h.SetState(anna, []uint16{2, 1}, nil)
	h.SetPTT(anna, true, proto.Target{Kind: proto.TargetChannels})
	live := h.Live()
	if len(live) != 3 { // dario is disabled and not listed
		t.Fatalf("live = %+v", live)
	}
	a := live[0]
	if a.Name != "anna" || !a.Online || !a.Talking || a.Target == nil || !slices.Equal(a.Talk, []uint16{1, 2}) {
		t.Errorf("anna = %+v (talk must follow assignment order)", a)
	}
	if live[1].Online {
		t.Errorf("bruno should be offline: %+v", live[1])
	}
}
```

- [ ] **Step 2: Verifica che falliscano**

Run: `cd server && go test ./internal/hub/`
Expected: FAIL, `undefined: Route`, `undefined: New`

- [ ] **Step 3: Implementa `route.go`**

`server/internal/hub/route.go`:

```go
package hub

import (
	"slices"

	"intercom/internal/proto"
)

// Peer is a connected client able to receive audio.
type Peer struct {
	UserID uint16
	Listen []uint16 // in the listener's channel order
}

// Delivery means: send the packet to UserID, labelled with ChannelID.
type Delivery struct {
	UserID    uint16
	ChannelID uint16
}

// Route returns who receives a talker's packet. Each listener appears at most once;
// for channel talk the label is the first of the listener's channels the talker is on.
func Route(talker uint16, t proto.Target, talk []uint16, peers []Peer) []Delivery {
	var out []Delivery
	for _, p := range peers {
		if p.UserID == talker {
			continue
		}
		switch t.Kind {
		case proto.TargetAll:
			out = append(out, Delivery{p.UserID, proto.ChanAll})
		case proto.TargetUser:
			if p.UserID == t.UserID {
				out = append(out, Delivery{p.UserID, proto.ChanPrivate})
			}
		case proto.TargetChannels:
			for _, ch := range p.Listen {
				if slices.Contains(talk, ch) {
					out = append(out, Delivery{p.UserID, ch})
					break
				}
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Implementa `hub.go`**

`server/internal/hub/hub.go`:

```go
// Package hub tracks connected clients and forwards their audio.
package hub

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"slices"
	"sync"
	"time"

	"intercom/internal/config"
	"intercom/internal/proto"
)

// Session is one logged-in client. Out carries proto messages for its WebSocket
// and is closed when the session ends.
type Session struct {
	ID     uint32
	UserID uint16
	Out    chan any

	name     string
	udp      *net.UDPAddr
	channels []proto.ChannelInfo // assigned channels, in order
	perms    proto.Perms
	talk     []uint16
	listen   []uint16
	ptt      bool
	target   proto.Target
	lastSeen time.Time
	closed   bool
}

type Hub struct {
	SendUDP func(addr *net.UDPAddr, b []byte)

	mu       sync.Mutex
	store    *config.Store
	now      func() time.Time
	sessions map[uint32]*Session
	byUser   map[uint16]*Session
}

func New(store *config.Store) *Hub {
	return &Hub{
		SendUDP:  func(*net.UDPAddr, []byte) {},
		store:    store,
		now:      time.Now,
		sessions: map[uint32]*Session{},
		byUser:   map[uint16]*Session{},
	}
}

// userView returns user u's channels (in assignment order) and permissions.
func userView(c *config.Config, u *config.User) ([]proto.ChannelInfo, proto.Perms) {
	infos := []proto.ChannelInfo{}
	for _, a := range u.Channels {
		if ch := c.ChannelByID(a.ChannelID); ch != nil {
			infos = append(infos, proto.ChannelInfo{ID: ch.ID, Name: ch.Name, Color: ch.Color, CanTalk: a.Talk, CanListen: a.Listen})
		}
	}
	return infos, proto.Perms{AllCall: u.AllCall, PrivateCall: u.PrivateCall}
}

// pick keeps the channels from infos that are in want and pass ok, in infos order.
func pick(infos []proto.ChannelInfo, want []uint16, ok func(proto.ChannelInfo) bool) []uint16 {
	out := []uint16{}
	for _, ci := range infos {
		if ok(ci) && slices.Contains(want, ci.ID) {
			out = append(out, ci.ID)
		}
	}
	return out
}

func canTalk(ci proto.ChannelInfo) bool   { return ci.CanTalk }
func canListen(ci proto.ChannelInfo) bool { return ci.CanListen }

func (h *Hub) applyLocked(s *Session, infos []proto.ChannelInfo, perms proto.Perms) {
	s.channels, s.perms = infos, perms
	s.talk = pick(infos, s.talk, canTalk)
	s.listen = pick(infos, s.listen, canListen)
}

func (h *Hub) Login(username, password string) (*Session, *proto.ErrorMsg) {
	var u config.User
	var found bool
	var w proto.Welcome
	h.store.View(func(c *config.Config) {
		p := c.UserByName(username)
		if p == nil {
			return
		}
		u, found = *p, true
		w.Channels, w.Perms = userView(c, p)
		w.UDPPort, w.ServerName = c.Settings.UDPPort, c.Settings.ServerName
	})
	// bcrypt is slow: check outside the hub lock.
	if !found || !config.CheckPassword(u.PasswordHash, password) {
		return nil, &proto.ErrorMsg{Type: proto.MsgError, Code: proto.ErrBadCredentials, Message: "Nome utente o password errati"}
	}
	if !u.Enabled {
		return nil, &proto.ErrorMsg{Type: proto.MsgError, Code: proto.ErrDisabled, Message: "Utente disabilitato"}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.byUser[u.ID]; old != nil {
		h.kickLocked(old, proto.KickReplaced)
	}
	s := &Session{ID: h.newIDLocked(), UserID: u.ID, Out: make(chan any, 64), name: u.Name, lastSeen: h.now()}
	h.applyLocked(s, w.Channels, w.Perms)
	h.sessions[s.ID], h.byUser[u.ID] = s, s
	w.Type, w.UserID, w.Session = proto.MsgWelcome, u.ID, s.ID
	h.send(s, w)
	h.broadcastUsersLocked()
	return s, nil
}

func (h *Hub) newIDLocked() uint32 {
	var b [4]byte
	for {
		rand.Read(b[:])
		if id := binary.BigEndian.Uint32(b[:]); id != 0 && h.sessions[id] == nil {
			return id
		}
	}
}

func (h *Hub) Logout(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.endLocked(s)
	h.broadcastUsersLocked()
}

func (h *Hub) SetState(s *Session, talk, listen []uint16) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.closed {
		return
	}
	s.lastSeen = h.now()
	s.talk = pick(s.channels, talk, canTalk)
	s.listen = pick(s.channels, listen, canListen)
	if h.stopIfNotAllowedLocked(s) {
		h.broadcastUsersLocked()
	}
}

func (h *Hub) SetPTT(s *Session, active bool, t proto.Target) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.closed {
		return
	}
	s.lastSeen = h.now()
	s.target = t
	s.ptt = active && h.allowedLocked(s)
	h.broadcastTalkingLocked(s)
	h.broadcastUsersLocked()
}

func (h *Hub) Ping(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s.lastSeen = h.now()
	h.send(s, proto.Pong{Type: proto.MsgPong})
}

// HandleUDP processes one datagram. It does not keep b after returning.
func (h *Hub) HandleUDP(addr *net.UDPAddr, b []byte) {
	p, err := proto.ParseClient(b)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[p.Session]
	if s == nil {
		return
	}
	s.udp, s.lastSeen = addr, h.now()
	if p.Kind != proto.KindVoice || !s.ptt || !h.allowedLocked(s) {
		return
	}
	peers := make([]Peer, 0, len(h.sessions))
	for _, o := range h.sessions {
		if o.udp != nil {
			peers = append(peers, Peer{UserID: o.UserID, Listen: o.listen})
		}
	}
	// ponytail: sends under the hub lock; fine for LAN sizes, move to a send queue if the lock shows up in profiles.
	for _, d := range Route(s.UserID, s.target, s.talk, peers) {
		pkt := proto.ServerPacket{Kind: proto.KindVoice, TalkerID: s.UserID, ChannelID: d.ChannelID, Seq: p.Seq, Payload: p.Payload}
		h.SendUDP(h.byUser[d.UserID].udp, pkt.Marshal())
	}
}

// ConfigChanged re-applies the stored configuration to every connected client.
func (h *Hub) ConfigChanged() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store.View(func(c *config.Config) {
		for _, s := range h.sessions {
			u := c.UserByID(s.UserID)
			if u == nil || !u.Enabled {
				h.kickLocked(s, proto.KickDisabled)
				continue
			}
			s.name = u.Name
			infos, perms := userView(c, u)
			h.applyLocked(s, infos, perms)
			h.send(s, proto.ConfigMsg{Type: proto.MsgConfig, Channels: infos, Perms: perms})
		}
	})
	for _, s := range h.sessions {
		h.stopIfNotAllowedLocked(s)
	}
	h.broadcastUsersLocked()
}

func (h *Hub) Kick(userID uint16, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s := h.byUser[userID]; s != nil {
		h.kickLocked(s, reason)
		h.broadcastUsersLocked()
	}
}

// Reap ends sessions silent for longer than timeout.
func (h *Hub) Reap(timeout time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now, n := h.now(), 0
	for _, s := range h.sessions {
		if now.Sub(s.lastSeen) > timeout {
			h.endLocked(s)
			n++
		}
	}
	if n > 0 {
		h.broadcastUsersLocked()
	}
}

type LiveUser struct {
	proto.UserStatus
	Target *proto.Target `json:"target,omitempty"`
	Talk   []uint16      `json:"talk"`
}

// Live is the admin overview: every enabled user, online state and where they talk.
func (h *Hub) Live() []LiveUser {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []LiveUser{}
	for _, u := range h.usersLocked() {
		l := LiveUser{UserStatus: u, Talk: []uint16{}}
		if s := h.byUser[u.ID]; s != nil && s.ptt {
			t := s.target
			l.Target = &t
			l.Talk = append(l.Talk, s.talk...)
		}
		out = append(out, l)
	}
	return out
}

func (h *Hub) allowedLocked(s *Session) bool {
	switch s.target.Kind {
	case proto.TargetChannels:
		return len(s.talk) > 0
	case proto.TargetAll:
		return s.perms.AllCall
	case proto.TargetUser:
		return s.perms.PrivateCall && s.target.UserID != s.UserID && h.byUser[s.target.UserID] != nil
	}
	return false
}

// stopIfNotAllowedLocked ends a transmission that permissions no longer allow.
func (h *Hub) stopIfNotAllowedLocked(s *Session) bool {
	if !s.ptt || h.allowedLocked(s) {
		return false
	}
	s.ptt = false
	h.broadcastTalkingLocked(s)
	return true
}

// send queues msg without blocking; a client that falls 64 messages behind is dropped.
func (h *Hub) send(s *Session, msg any) {
	if s.closed {
		return
	}
	select {
	case s.Out <- msg:
	default:
		h.endLocked(s)
	}
}

func (h *Hub) kickLocked(s *Session, reason string) {
	h.send(s, proto.Kicked{Type: proto.MsgKicked, Reason: reason})
	h.endLocked(s)
}

// endLocked closes s and tells the others it stopped talking.
func (h *Hub) endLocked(s *Session) {
	if s.closed {
		return
	}
	s.closed = true
	close(s.Out)
	delete(h.sessions, s.ID)
	if h.byUser[s.UserID] == s {
		delete(h.byUser, s.UserID)
	}
	if s.ptt {
		s.ptt = false
		h.broadcastTalkingLocked(s)
	}
}

func (h *Hub) broadcastTalkingLocked(s *Session) {
	msg := proto.Talking{Type: proto.MsgTalking, UserID: s.UserID, Name: s.name, Active: s.ptt, Target: s.target, ChannelIDs: []uint16{}}
	if s.target.Kind == proto.TargetChannels {
		msg.ChannelIDs = append(msg.ChannelIDs, s.talk...)
	}
	if s.target.Kind == proto.TargetUser { // private: only the callee is told
		if o := h.byUser[s.target.UserID]; o != nil {
			h.send(o, msg)
		}
		return
	}
	for _, o := range h.sessions {
		if o != s {
			h.send(o, msg)
		}
	}
}

func (h *Hub) usersLocked() []proto.UserStatus {
	out := []proto.UserStatus{}
	h.store.View(func(c *config.Config) {
		for _, u := range c.Users {
			if !u.Enabled {
				continue
			}
			s := h.byUser[u.ID]
			out = append(out, proto.UserStatus{ID: u.ID, Name: u.Name, Online: s != nil, Talking: s != nil && s.ptt})
		}
	})
	return out
}

func (h *Hub) broadcastUsersLocked() {
	msg := proto.UsersMsg{Type: proto.MsgUsers, Users: h.usersLocked()}
	for _, s := range h.sessions {
		h.send(s, msg)
	}
}
```

Ordine dei lock: sempre `hub.mu` → `store` (lettura). Le callback di `View`/`Update` non toccano mai l'hub.

- [ ] **Step 5: Verifica che passino**

Run: `cd server && go test -race ./internal/hub/`
Expected: `ok  intercom/internal/hub`

- [ ] **Step 6: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add server
git commit -m "feat(server): session hub and audio routing

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: WebSocket client e API admin (`web`)

**Files:**
- Create: `server/internal/web/server.go`
- Create: `server/internal/web/ws.go`
- Create: `server/internal/web/admin.go`
- Test: `server/internal/web/web_test.go`

**Interfaces:**
- Consumes: `hub.Hub` (`Login`, `Logout`, `SetState`, `SetPTT`, `Ping`, `ConfigChanged`, `Kick`, `Live`), `config.Store`, `proto.*`.
- Produces:
  - `type Server struct{Store *config.Store; Hub *hub.Hub; OnSettings func(config.Settings); ...}`
  - `New(*config.Store, *hub.Hub) *Server`, `(*Server).Handler() http.Handler`
  - API JSON (tutte le risposte di successo senza dati sono `{}`; errori `{"error": "..."}`):
    - `GET /api/status` → `{needsSetup, loggedIn}`
    - `POST /api/setup|login` `{password}`; `POST /api/logout`
    - `GET /api/config` → `{settings:{serverName,httpPort,udpPort}, channels:[{id,name,color}], users:[{id,name,enabled,channels:[{channelId,talk,listen}],allCall,privateCall}]}`
    - `PUT /api/settings` `{serverName,httpPort,udpPort}`; `POST /api/password` `{password}`
    - `POST /api/channels` `{name,color}`; `PUT /api/channels/order` `[id,…]`; `PUT|DELETE /api/channels/{id}`
    - `POST /api/users` `{name,password,enabled,channels,allCall,privateCall}`; `PUT|DELETE /api/users/{id}` (password vuota = invariata)
    - `GET /api/live` → `[LiveUser]`; `POST /api/kick/{id}`

- [ ] **Step 1: Scrivi i test che falliscono**

`server/internal/web/web_test.go`:

```go
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"intercom/internal/config"
	"intercom/internal/hub"
	"intercom/internal/proto"
)

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

// adminServer starts a server and completes first-time setup with password "admin1".
func adminServer(t *testing.T) (*httptest.Server, *http.Client, *config.Store) {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(store, hub.New(store)).Handler())
	t.Cleanup(srv.Close)
	c := newClient()
	if code, body := call(t, c, srv, "POST", "/api/setup", map[string]string{"password": "admin1"}); code != 200 {
		t.Fatalf("setup = %d %s", code, body)
	}
	return srv, c, store
}

func call(t *testing.T, c *http.Client, srv *httptest.Server, method, path string, body any) (int, string) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, r)
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestSetupOnlyOnceAndLogin(t *testing.T) {
	srv, c, _ := adminServer(t)
	if code, _ := call(t, c, srv, "POST", "/api/setup", map[string]string{"password": "altro1"}); code != 403 {
		t.Errorf("second setup = %d, want 403", code)
	}
	anon := &http.Client{}
	if code, _ := call(t, anon, srv, "GET", "/api/config", nil); code != 401 {
		t.Errorf("config without login = %d, want 401", code)
	}
	if code, _ := call(t, anon, srv, "POST", "/api/login", map[string]string{"password": "sbagliata"}); code != 401 {
		t.Errorf("wrong password = %d, want 401", code)
	}
	fresh := newClient()
	if code, _ := call(t, fresh, srv, "POST", "/api/login", map[string]string{"password": "admin1"}); code != 200 {
		t.Errorf("login = %d", code)
	}
	if code, body := call(t, fresh, srv, "GET", "/api/status", nil); code != 200 || !strings.Contains(body, `"loggedIn":true`) {
		t.Errorf("status = %d %s", code, body)
	}
}

func TestChannelAndUserCRUD(t *testing.T) {
	srv, c, store := adminServer(t)
	if code, body := call(t, c, srv, "POST", "/api/channels", map[string]string{"name": "Regia", "color": "#ff0000"}); code != 200 {
		t.Fatalf("create channel = %d %s", code, body)
	}
	var chID uint16
	store.View(func(cfg *config.Config) { chID = cfg.Channels[0].ID })

	user := map[string]any{"name": "Anna", "password": "pw", "enabled": true,
		"channels": []map[string]any{{"channelId": chID, "talk": true, "listen": true}}}
	if code, body := call(t, c, srv, "POST", "/api/users", user); code != 200 {
		t.Fatalf("create user = %d %s", code, body)
	}
	user["name"] = "anna"
	if code, _ := call(t, c, srv, "POST", "/api/users", user); code != 409 {
		t.Errorf("duplicate name (other case) = %d, want 409", code)
	}
	badUser := map[string]any{"name": "Bruno", "password": "pw", "channels": []map[string]any{{"channelId": 999}}}
	if code, _ := call(t, c, srv, "POST", "/api/users", badUser); code != 400 {
		t.Errorf("unknown channel = %d, want 400", code)
	}
	if code, _ := call(t, c, srv, "POST", "/api/users", map[string]any{"name": "Carla"}); code != 400 {
		t.Errorf("missing password = %d, want 400", code)
	}

	_, body := call(t, c, srv, "GET", "/api/config", nil)
	if strings.Contains(body, "passwordHash") || strings.Contains(body, "adminHash") || strings.Contains(body, "$2a$") {
		t.Errorf("hash leaked: %s", body)
	}

	if code, _ := call(t, c, srv, "DELETE", fmt.Sprintf("/api/channels/%d", chID), nil); code != 200 {
		t.Fatalf("delete channel = %d", code)
	}
	store.View(func(cfg *config.Config) {
		if len(cfg.Users[0].Channels) != 0 {
			t.Errorf("assignment survived channel delete: %+v", cfg.Users[0].Channels)
		}
	})
}

func TestUpdateUserKeepsPasswordWhenEmpty(t *testing.T) {
	srv, c, store := adminServer(t)
	call(t, c, srv, "POST", "/api/users", map[string]any{"name": "anna", "password": "pw", "enabled": true})
	var id uint16
	var before string
	store.View(func(cfg *config.Config) { id, before = cfg.Users[0].ID, cfg.Users[0].PasswordHash })
	if code, body := call(t, c, srv, "PUT", fmt.Sprintf("/api/users/%d", id), map[string]any{"name": "Anna B", "enabled": false}); code != 200 {
		t.Fatalf("update = %d %s", code, body)
	}
	store.View(func(cfg *config.Config) {
		u := cfg.Users[0]
		if u.Name != "Anna B" || u.Enabled || u.PasswordHash != before {
			t.Errorf("user = %+v", u)
		}
	})
}

func TestChannelOrderAndSettings(t *testing.T) {
	srv, c, store := adminServer(t)
	call(t, c, srv, "POST", "/api/channels", map[string]string{"name": "A"})
	call(t, c, srv, "POST", "/api/channels", map[string]string{"name": "B"})
	var a, b uint16
	store.View(func(cfg *config.Config) { a, b = cfg.Channels[0].ID, cfg.Channels[1].ID })
	if code, _ := call(t, c, srv, "PUT", "/api/channels/order", []uint16{b, a}); code != 200 {
		t.Fatalf("order = %d", code)
	}
	if code, _ := call(t, c, srv, "PUT", "/api/channels/order", []uint16{b, b}); code != 400 {
		t.Errorf("duplicate ids in order = %d, want 400", code)
	}
	store.View(func(cfg *config.Config) {
		if cfg.Channels[0].Name != "B" {
			t.Errorf("order not applied: %+v", cfg.Channels)
		}
	})

	if code, _ := call(t, c, srv, "PUT", "/api/settings", map[string]any{"serverName": "Teatro", "httpPort": 0, "udpPort": 8081}); code != 400 {
		t.Errorf("port 0 = %d, want 400", code)
	}
	if code, _ := call(t, c, srv, "PUT", "/api/settings", map[string]any{"serverName": "Teatro", "httpPort": 9090, "udpPort": 9091}); code != 200 {
		t.Errorf("valid settings rejected")
	}
	store.View(func(cfg *config.Config) {
		if cfg.Settings.ServerName != "Teatro" || cfg.Settings.UDPPort != 9091 || cfg.Settings.AdminHash == "" {
			t.Errorf("settings = %+v", cfg.Settings)
		}
	})
}

func TestWebSocketLogin(t *testing.T) {
	srv, c, _ := adminServer(t)
	call(t, c, srv, "POST", "/api/users", map[string]any{"name": "anna", "password": "pw", "enabled": true})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgHello, Username: "anna", Password: "pw"})
	var w proto.Welcome
	if err := wsjson.Read(ctx, conn, &w); err != nil || w.Type != proto.MsgWelcome || w.Session == 0 {
		t.Fatalf("welcome = %+v, err %v", w, err)
	}
	wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgPing})
	for { // skip the users broadcast, expect a pong
		var m map[string]any
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] == proto.MsgPong {
			break
		}
	}

	bad, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bad.CloseNow()
	wsjson.Write(ctx, bad, proto.ClientMsg{Type: proto.MsgHello, Username: "anna", Password: "no"})
	var e proto.ErrorMsg
	if err := wsjson.Read(ctx, bad, &e); err != nil || e.Code != proto.ErrBadCredentials {
		t.Errorf("error = %+v, err %v", e, err)
	}
}
```

- [ ] **Step 2: Verifica che falliscano**

```bash
cd server && go get github.com/coder/websocket
go test ./internal/web/
```

Expected: FAIL, `undefined: New`

- [ ] **Step 3: Implementa `server.go`**

`server/internal/web/server.go`:

```go
// Package web serves the client WebSocket, the admin API and the admin page.
package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"

	"intercom/internal/config"
	"intercom/internal/hub"
)

type Server struct {
	Store      *config.Store
	Hub        *hub.Hub
	OnSettings func(config.Settings) // called after the settings are saved

	mu     sync.Mutex
	tokens map[string]bool // admin session cookies; lost on restart
}

func New(store *config.Store, h *hub.Hub) *Server {
	return &Server{Store: store, Hub: h, tokens: map[string]bool{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", s.handleWS)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/config", s.auth(s.getConfig))
	mux.HandleFunc("PUT /api/settings", s.auth(s.putSettings))
	mux.HandleFunc("POST /api/password", s.auth(s.changePassword))
	mux.HandleFunc("POST /api/channels", s.auth(s.createChannel))
	mux.HandleFunc("PUT /api/channels/order", s.auth(s.orderChannels))
	mux.HandleFunc("PUT /api/channels/{id}", s.auth(s.updateChannel))
	mux.HandleFunc("DELETE /api/channels/{id}", s.auth(s.deleteChannel))
	mux.HandleFunc("POST /api/users", s.auth(s.createUser))
	mux.HandleFunc("PUT /api/users/{id}", s.auth(s.updateUser))
	mux.HandleFunc("DELETE /api/users/{id}", s.auth(s.deleteUser))
	mux.HandleFunc("GET /api/live", s.auth(s.live))
	mux.HandleFunc("POST /api/kick/{id}", s.auth(s.kick))
	return mux
}

type apiError struct {
	status int
	msg    string
}

func (e apiError) Error() string { return e.msg }

func bad(msg string) error { return apiError{http.StatusBadRequest, msg} }

var errNotFound = apiError{http.StatusNotFound, "non trovato"}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeOK(w http.ResponseWriter) { writeJSON(w, http.StatusOK, struct{}{}) }

func writeErr(w http.ResponseWriter, err error) {
	var ae apiError
	if errors.As(err, &ae) {
		writeJSON(w, ae.status, map[string]string{"error": ae.msg})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return bad("JSON non valido")
	}
	return nil
}

func pathID(r *http.Request) (uint16, error) {
	n, err := strconv.ParseUint(r.PathValue("id"), 10, 16)
	if err != nil {
		return 0, bad("id non valido")
	}
	return uint16(n), nil
}

// update applies fn to the configuration, saves it and pushes it to connected clients.
func (s *Server) update(w http.ResponseWriter, fn func(c *config.Config) error) {
	if err := s.Store.Update(fn); err != nil {
		writeErr(w, err)
		return
	}
	s.Hub.ConfigChanged()
	writeOK(w)
}
```

- [ ] **Step 4: Implementa `ws.go`**

`server/internal/web/ws.go`:

```go
package web

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"intercom/internal/proto"
)

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// Not r.Context(): the connection is hijacked (see coder/websocket docs).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	helloCtx, helloCancel := context.WithTimeout(ctx, 10*time.Second)
	var hello proto.ClientMsg
	err = wsjson.Read(helloCtx, conn, &hello)
	helloCancel()
	if err != nil || hello.Type != proto.MsgHello {
		conn.Close(websocket.StatusPolicyViolation, "hello richiesto")
		return
	}
	sess, lerr := s.Hub.Login(hello.Username, hello.Password)
	if lerr != nil {
		wsjson.Write(ctx, conn, lerr)
		conn.Close(websocket.StatusNormalClosure, lerr.Code)
		return
	}
	defer s.Hub.Logout(sess)

	go func() {
		defer s.Hub.Logout(sess) // closes sess.Out, which ends the writer loop
		for {
			var m proto.ClientMsg
			if err := wsjson.Read(ctx, conn, &m); err != nil {
				return
			}
			switch m.Type {
			case proto.MsgState:
				s.Hub.SetState(sess, m.Talk, m.Listen)
			case proto.MsgPTT:
				s.Hub.SetPTT(sess, m.Active, m.Target)
			case proto.MsgPing:
				s.Hub.Ping(sess)
			}
		}
	}()

	for msg := range sess.Out {
		if err := wsjson.Write(ctx, conn, msg); err != nil {
			return
		}
	}
	conn.Close(websocket.StatusNormalClosure, "")
}
```

- [ ] **Step 5: Implementa `admin.go`**

`server/internal/web/admin.go`:

```go
package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"

	"intercom/internal/config"
)

const cookieName = "intercom_admin"

func (s *Server) loggedIn(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[c.Value]
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.loggedIn(r) {
			writeErr(w, apiError{http.StatusUnauthorized, "accesso richiesto"})
			return
		}
		next(w, r)
	}
}

func (s *Server) startSession(w http.ResponseWriter) {
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	s.mu.Lock()
	s.tokens[tok] = true
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

type passwordReq struct {
	Password string `json:"password"`
}

// readPassword reads {password} and enforces the minimum length.
func readPassword(r *http.Request) (string, error) {
	var req passwordReq
	if err := readJSON(r, &req); err != nil {
		return "", err
	}
	if len(req.Password) < 4 {
		return "", bad("la password deve avere almeno 4 caratteri")
	}
	return req.Password, nil
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	var needs bool
	s.Store.View(func(c *config.Config) { needs = c.Settings.AdminHash == "" })
	writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": needs, "loggedIn": !needs && s.loggedIn(r)})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	pass, err := readPassword(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	hash, err := config.HashPassword(pass)
	if err != nil {
		writeErr(w, err)
		return
	}
	err = s.Store.Update(func(c *config.Config) error {
		if c.Settings.AdminHash != "" {
			return apiError{http.StatusForbidden, "password amministratore già impostata"}
		}
		c.Settings.AdminHash = hash
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	s.startSession(w)
	writeOK(w)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req passwordReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	var hash string
	s.Store.View(func(c *config.Config) { hash = c.Settings.AdminHash })
	if !config.CheckPassword(hash, req.Password) {
		writeErr(w, apiError{http.StatusUnauthorized, "password errata"})
		return
	}
	s.startSession(w)
	writeOK(w)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.tokens, c.Value)
		s.mu.Unlock()
	}
	writeOK(w)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	pass, err := readPassword(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	hash, err := config.HashPassword(pass)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.Update(func(c *config.Config) error { c.Settings.AdminHash = hash; return nil }); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w)
}

type publicSettings struct {
	ServerName string `json:"serverName"`
	HTTPPort   int    `json:"httpPort"`
	UDPPort    int    `json:"udpPort"`
}

type publicUser struct {
	ID          uint16              `json:"id"`
	Name        string              `json:"name"`
	Enabled     bool                `json:"enabled"`
	Channels    []config.Assignment `json:"channels"`
	AllCall     bool                `json:"allCall"`
	PrivateCall bool                `json:"privateCall"`
}

type publicConfig struct {
	Settings publicSettings   `json:"settings"`
	Channels []config.Channel `json:"channels"`
	Users    []publicUser     `json:"users"`
}

// getConfig returns the configuration without any password hash.
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	out := publicConfig{Channels: []config.Channel{}, Users: []publicUser{}}
	s.Store.View(func(c *config.Config) {
		out.Settings = publicSettings{c.Settings.ServerName, c.Settings.HTTPPort, c.Settings.UDPPort}
		out.Channels = append(out.Channels, c.Channels...)
		for _, u := range c.Users {
			out.Users = append(out.Users, publicUser{u.ID, u.Name, u.Enabled, append([]config.Assignment{}, u.Channels...), u.AllCall, u.PrivateCall})
		}
	})
	writeJSON(w, http.StatusOK, out)
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in publicSettings
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	in.ServerName = strings.TrimSpace(in.ServerName)
	if in.ServerName == "" {
		writeErr(w, bad("il nome del server è obbligatorio"))
		return
	}
	if !validPort(in.HTTPPort) || !validPort(in.UDPPort) {
		writeErr(w, bad("porta non valida (1-65535)"))
		return
	}
	var saved config.Settings
	err := s.Store.Update(func(c *config.Config) error {
		c.Settings.ServerName, c.Settings.HTTPPort, c.Settings.UDPPort = in.ServerName, in.HTTPPort, in.UDPPort
		saved = c.Settings
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	if s.OnSettings != nil {
		s.OnSettings(saved)
	}
	writeOK(w)
}

type channelReq struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func readChannel(r *http.Request) (channelReq, error) {
	var in channelReq
	if err := readJSON(r, &in); err != nil {
		return in, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, bad("il nome del canale è obbligatorio")
	}
	if in.Color == "" {
		in.Color = "#3b82f6"
	}
	return in, nil
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	in, err := readChannel(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.update(w, func(c *config.Config) error {
		c.Channels = append(c.Channels, config.Channel{ID: c.NewID(), Name: in.Name, Color: in.Color})
		return nil
	})
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := readChannel(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.update(w, func(c *config.Config) error {
		ch := c.ChannelByID(id)
		if ch == nil {
			return errNotFound
		}
		ch.Name, ch.Color = in.Name, in.Color
		return nil
	})
}

// deleteChannel removes the channel and every user's assignment to it.
func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.update(w, func(c *config.Config) error {
		i := slices.IndexFunc(c.Channels, func(ch config.Channel) bool { return ch.ID == id })
		if i < 0 {
			return errNotFound
		}
		c.Channels = slices.Delete(c.Channels, i, i+1)
		for u := range c.Users {
			c.Users[u].Channels = slices.DeleteFunc(c.Users[u].Channels, func(a config.Assignment) bool { return a.ChannelID == id })
		}
		return nil
	})
}

// orderChannels takes every channel id exactly once, in the new order.
func (s *Server) orderChannels(w http.ResponseWriter, r *http.Request) {
	var ids []uint16
	if err := readJSON(r, &ids); err != nil {
		writeErr(w, err)
		return
	}
	s.update(w, func(c *config.Config) error {
		if len(ids) != len(c.Channels) {
			return bad("ordine non valido")
		}
		out := make([]config.Channel, 0, len(ids))
		for _, id := range ids {
			ch := c.ChannelByID(id)
			if ch == nil || slices.ContainsFunc(out, func(x config.Channel) bool { return x.ID == id }) {
				return bad("ordine non valido")
			}
			out = append(out, *ch)
		}
		c.Channels = out
		return nil
	})
}

type userReq struct {
	Name        string              `json:"name"`
	Password    string              `json:"password"`
	Enabled     bool                `json:"enabled"`
	Channels    []config.Assignment `json:"channels"`
	AllCall     bool                `json:"allCall"`
	PrivateCall bool                `json:"privateCall"`
}

// validate checks in against c; self is the id of the user being edited (0 when creating).
func (in *userReq) validate(c *config.Config, self uint16) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return bad("il nome utente è obbligatorio")
	}
	if u := c.UserByName(in.Name); u != nil && u.ID != self {
		return apiError{http.StatusConflict, "esiste già un utente con questo nome"}
	}
	seen := map[uint16]bool{}
	for _, a := range in.Channels {
		if c.ChannelByID(a.ChannelID) == nil || seen[a.ChannelID] {
			return bad("canale non valido")
		}
		seen[a.ChannelID] = true
	}
	return nil
}

// apply copies in onto u; an empty hash keeps the current password.
func (in *userReq) apply(u *config.User, hash string) {
	u.Name, u.Enabled, u.Channels = in.Name, in.Enabled, in.Channels
	u.AllCall, u.PrivateCall = in.AllCall, in.PrivateCall
	if hash != "" {
		u.PasswordHash = hash
	}
}

// readUser decodes the body and hashes the password if one was given.
func readUser(r *http.Request) (userReq, string, error) {
	var in userReq
	if err := readJSON(r, &in); err != nil {
		return in, "", err
	}
	if in.Password == "" {
		return in, "", nil
	}
	hash, err := config.HashPassword(in.Password)
	return in, hash, err
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	in, hash, err := readUser(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if hash == "" {
		writeErr(w, bad("la password è obbligatoria"))
		return
	}
	s.update(w, func(c *config.Config) error {
		if err := in.validate(c, 0); err != nil {
			return err
		}
		u := config.User{ID: c.NewID()}
		in.apply(&u, hash)
		c.Users = append(c.Users, u)
		return nil
	})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	in, hash, err := readUser(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.update(w, func(c *config.Config) error {
		u := c.UserByID(id)
		if u == nil {
			return errNotFound
		}
		if err := in.validate(c, id); err != nil {
			return err
		}
		in.apply(u, hash)
		return nil
	})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.update(w, func(c *config.Config) error {
		i := slices.IndexFunc(c.Users, func(u config.User) bool { return u.ID == id })
		if i < 0 {
			return errNotFound
		}
		c.Users = slices.Delete(c.Users, i, i+1)
		return nil
	})
}

func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Hub.Live())
}

func (s *Server) kick(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.Hub.Kick(id, "admin")
	writeOK(w)
}
```

- [ ] **Step 6: Verifica che passino**

Run: `cd server && go test -race ./internal/web/`
Expected: `ok  intercom/internal/web`

- [ ] **Step 7: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add server
git commit -m "feat(server): client websocket and admin API

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Pagina web admin

**Files:**
- Create: `server/internal/web/static/index.html`
- Create: `server/internal/web/static/style.css`
- Create: `server/internal/web/static/app.js`
- Modify: `server/internal/web/server.go` (embed + route `GET /`)
- Test: `server/internal/web/web_test.go` (aggiungi `TestServesAdminPage`)

**Interfaces:**
- Consumes: le API di Task 4 con i formati JSON indicati lì.

- [ ] **Step 1: Scrivi il test che fallisce**

Aggiungi in fondo a `server/internal/web/web_test.go`:

```go
func TestServesAdminPage(t *testing.T) {
	srv, _, _ := adminServer(t)
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		code, body := call(t, &http.Client{}, srv, "GET", path, nil)
		if code != 200 || body == "" {
			t.Errorf("GET %s = %d", path, code)
		}
	}
	if _, body := call(t, &http.Client{}, srv, "GET", "/", nil); !strings.Contains(body, "Intercom Server") {
		t.Errorf("index.html missing title")
	}
}
```

- [ ] **Step 2: Verifica che fallisca**

Run: `cd server && go test ./internal/web/ -run TestServesAdminPage`
Expected: FAIL, `GET / = 404`

- [ ] **Step 3: Crea `index.html`**

`server/internal/web/static/index.html`:

```html
<!doctype html>
<html lang="it">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Intercom Server</title>
  <link rel="stylesheet" href="style.css">
</head>
<body>
<main id="auth" hidden>
  <h1>Intercom Server</h1>
  <form id="auth-form">
    <p id="auth-hint"></p>
    <input type="password" id="auth-pass" placeholder="Password amministratore" required minlength="4">
    <button>Entra</button>
    <p class="err" id="auth-err"></p>
  </form>
</main>

<div id="app" hidden>
  <header>
    <h1>Intercom Server</h1>
    <nav>
      <button data-tab="overview">Panoramica</button>
      <button data-tab="channels">Canali</button>
      <button data-tab="users">Utenti</button>
      <button data-tab="settings">Impostazioni</button>
    </nav>
    <button id="logout" class="link">Esci</button>
  </header>

  <section id="tab-overview">
    <h2>Utenti</h2>
    <table>
      <thead><tr><th>Utente</th><th>Stato</th><th>Sta parlando</th><th></th></tr></thead>
      <tbody id="live"></tbody>
    </table>
  </section>

  <section id="tab-channels" hidden>
    <h2>Canali</h2>
    <form id="channel-form" class="row">
      <input id="ch-name" placeholder="Nome canale" required>
      <input id="ch-color" type="color" value="#3b82f6">
      <button>Aggiungi</button>
    </form>
    <table><tbody id="channels"></tbody></table>
  </section>

  <section id="tab-users" hidden>
    <h2>Utenti</h2>
    <button id="user-new">Nuovo utente</button>
    <table><tbody id="users"></tbody></table>
    <dialog id="user-dialog">
      <form id="user-form">
        <h3 id="user-title"></h3>
        <label>Nome <input id="u-name" required></label>
        <label>Password <input id="u-pass" type="password" autocomplete="new-password"></label>
        <label class="check"><input id="u-enabled" type="checkbox"> Abilitato</label>
        <label class="check"><input id="u-allcall" type="checkbox"> Chiamata a tutti</label>
        <label class="check"><input id="u-private" type="checkbox"> Chiamate private</label>
        <h4>Canali</h4>
        <table>
          <thead><tr><th>Canale</th><th>Assegnato</th><th>Parla</th><th>Ascolta</th><th>Ordine</th></tr></thead>
          <tbody id="u-channels"></tbody>
        </table>
        <p class="err" id="user-err"></p>
        <div class="row">
          <button type="button" id="user-cancel">Annulla</button>
          <button>Salva</button>
        </div>
      </form>
    </dialog>
  </section>

  <section id="tab-settings" hidden>
    <h2>Impostazioni</h2>
    <form id="settings-form">
      <label>Nome server <input id="s-name" required></label>
      <label>Porta HTTP <input id="s-http" type="number" min="1" max="65535" required></label>
      <label>Porta UDP audio <input id="s-udp" type="number" min="1" max="65535" required></label>
      <p class="hint">Le porte cambiano al prossimo riavvio del server.</p>
      <button>Salva</button> <span id="s-msg"></span>
    </form>
    <form id="password-form">
      <h3>Password amministratore</h3>
      <input id="p-new" type="password" minlength="4" required autocomplete="new-password">
      <button>Cambia password</button> <span id="p-msg"></span>
    </form>
  </section>
</div>
<script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 4: Crea `style.css`**

`server/internal/web/static/style.css`:

```css
:root { --bg: #121417; --panel: #1c2026; --line: #2c323a; --text: #e8eaed; --muted: #9aa3ad; --accent: #f5a524; --ok: #22c55e; --err: #ef4444; }
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--text); font: 15px/1.4 system-ui, sans-serif; }
h1 { font-size: 20px; margin: 0; }
h2 { font-size: 18px; }
main#auth { max-width: 360px; margin: 15vh auto; padding: 0 16px; }
main#auth input, main#auth button { width: 100%; margin-top: 8px; }
header { display: flex; gap: 24px; align-items: center; padding: 12px 24px; background: var(--panel); border-bottom: 1px solid var(--line); flex-wrap: wrap; }
nav { display: flex; gap: 4px; flex: 1; flex-wrap: wrap; }
nav button { background: none; border: 0; color: var(--muted); }
nav button.active { color: var(--accent); border-bottom: 2px solid var(--accent); border-radius: 0; }
section { padding: 8px 24px 32px; max-width: 960px; }
table { width: 100%; border-collapse: collapse; margin-top: 12px; }
th, td { text-align: left; padding: 8px; border-bottom: 1px solid var(--line); }
th { color: var(--muted); font-weight: 500; }
input, button { font: inherit; color: var(--text); background: var(--panel); border: 1px solid var(--line); border-radius: 6px; padding: 7px 10px; }
input[type=color] { padding: 2px; width: 44px; height: 34px; }
input[type=checkbox] { width: 18px; height: 18px; }
button { cursor: pointer; }
button:hover:not(:disabled) { border-color: var(--accent); }
button:disabled { opacity: .35; cursor: default; }
button.link { background: none; border: 0; color: var(--muted); }
label { display: block; margin: 10px 0; }
label input:not([type=checkbox]) { display: block; width: 100%; margin-top: 4px; }
label.check { display: flex; gap: 8px; align-items: center; }
.row { display: flex; gap: 8px; align-items: center; justify-content: flex-end; }
#channel-form { justify-content: flex-start; }
#password-form { margin-top: 32px; }
.dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; background: var(--muted); margin-right: 8px; }
.dot.on { background: var(--ok); }
.err { color: var(--err); min-height: 1.4em; }
.hint { color: var(--muted); font-size: 13px; }
dialog { background: var(--panel); color: var(--text); border: 1px solid var(--line); border-radius: 10px; width: min(640px, 94vw); }
dialog::backdrop { background: rgba(0, 0, 0, .6); }
```

- [ ] **Step 5: Crea `app.js`**

`server/internal/web/static/app.js`:

```js
const $ = (id) => document.getElementById(id);
let cfg = { settings: {}, channels: [], users: [] };
let editing = null; // user in the dialog: { id, channels: [{channelId, talk, listen}] }

async function api(method, path, body) {
  const res = await fetch('/api/' + path, {
    method,
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (res.status === 401 && path !== 'login') {
    showAuth(false);
    throw new Error('Sessione scaduta');
  }
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

// guard runs fn and shows its error; returns undefined on failure.
async function guard(fn) {
  try { return await fn(); } catch (err) { alert(err.message); }
}

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const chanName = (id) => (cfg.channels.find((c) => c.id === id) || {}).name || '?';
const userName = (id) => (cfg.users.find((u) => u.id === id) || {}).name || '?';

// --- login ---
function showAuth(needsSetup) {
  $('app').hidden = true;
  $('auth').hidden = false;
  $('auth-form').dataset.setup = needsSetup ? '1' : '';
  $('auth-hint').textContent = needsSetup ? 'Primo avvio: crea la password amministratore.' : 'Accedi con la password amministratore.';
  $('auth-pass').autocomplete = needsSetup ? 'new-password' : 'current-password';
}

$('auth-form').onsubmit = async (e) => {
  e.preventDefault();
  $('auth-err').textContent = '';
  try {
    await api('POST', e.target.dataset.setup ? 'setup' : 'login', { password: $('auth-pass').value });
    $('auth-pass').value = '';
    await showApp();
  } catch (err) {
    $('auth-err').textContent = err.message;
  }
};

async function showApp() {
  $('auth').hidden = true;
  $('app').hidden = false;
  await loadConfig();
  showTab('overview');
}

async function loadConfig() {
  cfg = await api('GET', 'config');
  renderChannels();
  renderUsers();
  renderSettings();
}

function showTab(name) {
  for (const s of document.querySelectorAll('section[id^=tab-]')) s.hidden = s.id !== 'tab-' + name;
  for (const b of document.querySelectorAll('nav button')) b.classList.toggle('active', b.dataset.tab === name);
  refreshLive();
}
for (const b of document.querySelectorAll('nav button')) b.onclick = () => showTab(b.dataset.tab);
$('logout').onclick = async () => { await api('POST', 'logout'); showAuth(false); };

// --- overview, refreshed every second while visible ---
function talkLabel(u) {
  if (!u.target || u.target.kind === 'channels') return u.talk.map(chanName).join(', ');
  if (u.target.kind === 'all') return 'Chiamata a tutti';
  return 'Privata → ' + userName(u.target.userId);
}

async function refreshLive() {
  if ($('app').hidden || $('tab-overview').hidden) return;
  const users = await api('GET', 'live').catch(() => null);
  if (!users) return;
  $('live').innerHTML = users.map((u) => `<tr>
    <td>${esc(u.name)}</td>
    <td><span class="dot ${u.online ? 'on' : ''}"></span>${u.online ? 'Online' : 'Offline'}</td>
    <td>${u.talking ? esc(talkLabel(u)) : ''}</td>
    <td>${u.online ? `<button data-kick="${u.id}">Disconnetti</button>` : ''}</td>
  </tr>`).join('');
}
$('live').onclick = async (e) => {
  const id = e.target.dataset.kick;
  if (id) { await guard(() => api('POST', 'kick/' + id)); refreshLive(); }
};
setInterval(refreshLive, 1000);

// --- channels ---
function renderChannels() {
  const last = cfg.channels.length - 1;
  $('channels').innerHTML = cfg.channels.map((c, i) => `<tr data-id="${c.id}">
    <td><input type="color" value="${esc(c.color)}" data-field="color"></td>
    <td><input value="${esc(c.name)}" data-field="name"></td>
    <td>
      <button data-move="-1" ${i === 0 ? 'disabled' : ''}>↑</button>
      <button data-move="1" ${i === last ? 'disabled' : ''}>↓</button>
      <button data-del>Elimina</button>
    </td>
  </tr>`).join('');
}

$('channel-form').onsubmit = async (e) => {
  e.preventDefault();
  if (await guard(() => api('POST', 'channels', { name: $('ch-name').value, color: $('ch-color').value }))) $('ch-name').value = '';
  await loadConfig();
};

$('channels').onchange = async (e) => {
  const tr = e.target.closest('tr');
  const name = tr.querySelector('[data-field=name]').value;
  const color = tr.querySelector('[data-field=color]').value;
  await guard(() => api('PUT', 'channels/' + tr.dataset.id, { name, color }));
  await loadConfig();
};

$('channels').onclick = async (e) => {
  const tr = e.target.closest('tr');
  if (!tr) return;
  const id = +tr.dataset.id;
  if (e.target.dataset.del !== undefined) {
    if (!confirm(`Eliminare il canale "${chanName(id)}"? Verrà tolto anche a tutti gli utenti.`)) return;
    await guard(() => api('DELETE', 'channels/' + id));
  } else if (e.target.dataset.move) {
    const ids = cfg.channels.map((c) => c.id);
    const i = ids.indexOf(id), j = i + Number(e.target.dataset.move);
    [ids[i], ids[j]] = [ids[j], ids[i]];
    await guard(() => api('PUT', 'channels/order', ids));
  } else return;
  await loadConfig();
};

// --- users ---
function renderUsers() {
  $('users').innerHTML = cfg.users.map((u) => `<tr data-id="${u.id}">
    <td>${esc(u.name)}</td>
    <td>${u.enabled ? 'Abilitato' : 'Disabilitato'}</td>
    <td>${(u.channels || []).map((a) => esc(chanName(a.channelId))).join(', ')}</td>
    <td><button data-edit>Modifica</button> <button data-del>Elimina</button></td>
  </tr>`).join('');
}

$('users').onclick = async (e) => {
  const tr = e.target.closest('tr');
  if (!tr) return;
  const u = cfg.users.find((x) => x.id === +tr.dataset.id);
  if (e.target.dataset.edit !== undefined) openUser(u);
  if (e.target.dataset.del !== undefined && confirm(`Eliminare l'utente "${u.name}"?`)) {
    await guard(() => api('DELETE', 'users/' + u.id));
    await loadConfig();
  }
};
$('user-new').onclick = () => openUser(null);

function openUser(u) {
  editing = { id: u ? u.id : null, channels: u ? (u.channels || []).map((a) => ({ ...a })) : [] };
  $('user-title').textContent = u ? 'Modifica ' + u.name : 'Nuovo utente';
  $('u-name').value = u ? u.name : '';
  $('u-pass').value = '';
  $('u-pass').required = !u;
  $('u-pass').placeholder = u ? 'Lascia vuoto per non cambiarla' : '';
  $('u-enabled').checked = u ? u.enabled : true;
  $('u-allcall').checked = u ? u.allCall : false;
  $('u-private').checked = u ? u.privateCall : true;
  $('user-err').textContent = '';
  renderUserChannels();
  $('user-dialog').showModal();
}

// Assigned channels first (in the user's order), then the unassigned ones.
function renderUserChannels() {
  const assigned = editing.channels.map((a) => a.channelId);
  const rows = [
    ...editing.channels,
    ...cfg.channels.filter((c) => !assigned.includes(c.id)).map((c) => ({ channelId: c.id, unassigned: true })),
  ];
  const last = editing.channels.length - 1;
  $('u-channels').innerHTML = rows.map((a, i) => `<tr data-id="${a.channelId}">
    <td>${esc(chanName(a.channelId))}</td>
    <td><input type="checkbox" data-f="assigned" ${a.unassigned ? '' : 'checked'}></td>
    <td><input type="checkbox" data-f="talk" ${a.talk ? 'checked' : ''} ${a.unassigned ? 'disabled' : ''}></td>
    <td><input type="checkbox" data-f="listen" ${a.listen ? 'checked' : ''} ${a.unassigned ? 'disabled' : ''}></td>
    <td>${a.unassigned ? '' : `<button type="button" data-move="-1" ${i === 0 ? 'disabled' : ''}>↑</button>
      <button type="button" data-move="1" ${i === last ? 'disabled' : ''}>↓</button>`}</td>
  </tr>`).join('');
}

$('u-channels').onchange = (e) => {
  const id = +e.target.closest('tr').dataset.id, f = e.target.dataset.f;
  if (f === 'assigned') {
    if (e.target.checked) editing.channels.push({ channelId: id, talk: true, listen: true });
    else editing.channels = editing.channels.filter((a) => a.channelId !== id);
  } else {
    editing.channels.find((a) => a.channelId === id)[f] = e.target.checked;
  }
  renderUserChannels();
};

$('u-channels').onclick = (e) => {
  if (!e.target.dataset.move) return;
  const id = +e.target.closest('tr').dataset.id, ch = editing.channels;
  const i = ch.findIndex((a) => a.channelId === id), j = i + Number(e.target.dataset.move);
  [ch[i], ch[j]] = [ch[j], ch[i]];
  renderUserChannels();
};

$('user-cancel').onclick = () => $('user-dialog').close();

$('user-form').onsubmit = async (e) => {
  e.preventDefault();
  const body = {
    name: $('u-name').value,
    password: $('u-pass').value,
    enabled: $('u-enabled').checked,
    allCall: $('u-allcall').checked,
    privateCall: $('u-private').checked,
    channels: editing.channels,
  };
  try {
    await api(editing.id ? 'PUT' : 'POST', editing.id ? 'users/' + editing.id : 'users', body);
    $('user-dialog').close();
    await loadConfig();
  } catch (err) {
    $('user-err').textContent = err.message;
  }
};

// --- settings ---
function renderSettings() {
  const s = cfg.settings;
  $('s-name').value = s.serverName;
  $('s-http').value = s.httpPort;
  $('s-udp').value = s.udpPort;
}

$('settings-form').onsubmit = async (e) => {
  e.preventDefault();
  const ok = await guard(() => api('PUT', 'settings', {
    serverName: $('s-name').value, httpPort: +$('s-http').value, udpPort: +$('s-udp').value,
  }));
  $('s-msg').textContent = ok ? 'Salvato' : '';
  await loadConfig();
};

$('password-form').onsubmit = async (e) => {
  e.preventDefault();
  const ok = await guard(() => api('POST', 'password', { password: $('p-new').value }));
  $('p-msg').textContent = ok ? 'Password cambiata' : '';
  $('p-new').value = '';
};

(async () => {
  const st = await api('GET', 'status');
  if (st.loggedIn) await showApp();
  else showAuth(st.needsSetup);
})();
```

- [ ] **Step 6: Servi i file statici**

In `server/internal/web/server.go` aggiungi agli import `"embed"` e `"io/fs"`, aggiungi sotto gli import:

```go
//go:embed static
var staticFiles embed.FS
```

e in `Handler()`, subito dopo `mux := http.NewServeMux()`:

```go
	static, _ := fs.Sub(staticFiles, "static")
	mux.Handle("GET /", http.FileServerFS(static))
```

- [ ] **Step 7: Verifica che passi**

Run: `cd server && go test -race ./internal/web/`
Expected: `ok  intercom/internal/web`

- [ ] **Step 8: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add server
git commit -m "feat(server): admin web page

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: mDNS, `main`, client di prova, build e verifica end-to-end

**Files:**
- Create: `server/internal/discovery/mdns.go`
- Create: `server/main.go`
- Create: `server/cmd/testclient/main.go`
- Create: `server/Dockerfile`
- Create: `server/README.md`

**Interfaces:**
- Consumes: `config.Open`, `hub.New` + `SendUDP`/`HandleUDP`/`Reap`, `web.New` + `OnSettings`/`Handler`, `proto` (Task 1–5).
- Produces:
  - `type discovery.Announcer`; `(*Announcer).Announce(name string, httpPort, udpPort int) error`, `(*Announcer).Close()`
  - binario `intercom-server` con flag `-config <path>` (predefinito `intercom-config.json`)
  - binario `testclient` con flag `-server host:porta -user -pass -channel N -talk durata -listen durata`

- [ ] **Step 1: Implementa l'annuncio mDNS**

```bash
cd server && go get github.com/grandcat/zeroconf
```

`server/internal/discovery/mdns.go`:

```go
// Package discovery announces the server on the LAN via mDNS/DNS-SD.
package discovery

import (
	"strconv"
	"sync"

	"github.com/grandcat/zeroconf"
)

const Service = "_intercom._tcp"

type Announcer struct {
	mu  sync.Mutex
	srv *zeroconf.Server
}

// Announce (re)publishes the service; calling it again replaces the previous announcement.
func (a *Announcer) Announce(name string, httpPort, udpPort int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.srv != nil {
		a.srv.Shutdown()
		a.srv = nil
	}
	txt := []string{"udp=" + strconv.Itoa(udpPort), "name=" + name}
	srv, err := zeroconf.Register(name, Service, "local.", httpPort, txt, nil)
	if err != nil {
		return err
	}
	a.srv = srv
	return nil
}

func (a *Announcer) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.srv != nil {
		a.srv.Shutdown()
		a.srv = nil
	}
}
```

- [ ] **Step 2: Implementa `main.go`**

`server/main.go`:

```go
// Command intercom-server runs the intercom server and its admin page.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"intercom/internal/config"
	"intercom/internal/discovery"
	"intercom/internal/hub"
	"intercom/internal/web"
)

func main() {
	cfgPath := flag.String("config", "intercom-config.json", "percorso del file di configurazione")
	flag.Parse()

	store, err := config.Open(*cfgPath)
	if err != nil {
		log.Fatalf("configurazione %s non leggibile: %v", *cfgPath, err)
	}
	var st config.Settings
	store.View(func(c *config.Config) { st = c.Settings })

	h := hub.New(store)
	udp, err := net.ListenUDP("udp", &net.UDPAddr{Port: st.UDPPort})
	if err != nil {
		log.Fatalf("porta UDP %d non disponibile: %v", st.UDPPort, err)
	}
	h.SendUDP = func(a *net.UDPAddr, b []byte) { udp.WriteToUDP(b, a) }
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := udp.ReadFromUDP(buf)
			if err != nil {
				log.Fatalf("UDP: %v", err)
			}
			h.HandleUDP(addr, buf[:n])
		}
	}()
	go func() {
		for range time.Tick(2 * time.Second) {
			h.Reap(10 * time.Second)
		}
	}()

	var ann discovery.Announcer
	if err := ann.Announce(st.ServerName, st.HTTPPort, st.UDPPort); err != nil {
		log.Printf("annuncio mDNS non disponibile (i telefoni dovranno usare l'IP): %v", err)
	}
	defer ann.Close()

	srv := web.New(store, h)
	srv.OnSettings = func(s config.Settings) {
		// Ports only change on restart, so keep announcing the ones in use.
		if err := ann.Announce(s.ServerName, st.HTTPPort, st.UDPPort); err != nil {
			log.Printf("annuncio mDNS: %v", err)
		}
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", st.HTTPPort))
	if err != nil {
		log.Fatalf("porta HTTP %d non disponibile: %v", st.HTTPPort, err)
	}
	printAddresses(st.HTTPPort)
	log.Fatal(http.Serve(ln, srv.Handler()))
}

func printAddresses(port int) {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ip, ok := a.(*net.IPNet); ok && !ip.IP.IsLoopback() && ip.IP.To4() != nil {
			log.Printf("Pagina web: http://%s:%d", ip.IP, port)
		}
	}
}
```

- [ ] **Step 3: Implementa il client di prova**

`server/cmd/testclient/main.go`:

```go
// Command testclient logs into the server, optionally talks on a channel with dummy
// packets (not real Opus: the server only routes), and counts the packets it receives.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"intercom/internal/proto"
)

func main() {
	server := flag.String("server", "127.0.0.1:8080", "host:porta HTTP del server")
	user := flag.String("user", "", "nome utente")
	pass := flag.String("pass", "", "password")
	channel := flag.Uint("channel", 0, "canale su cui parlare e ascoltare")
	talk := flag.Duration("talk", 0, "per quanto trasmettere (0 = solo ascolto)")
	listen := flag.Duration("listen", 5*time.Second, "per quanto ascoltare dopo aver parlato")
	flag.Parse()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws://"+*server+"/ws", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.CloseNow()

	wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgHello, Username: *user, Password: *pass, Device: "testclient"})
	var raw json.RawMessage
	if err := wsjson.Read(ctx, conn, &raw); err != nil {
		log.Fatal(err)
	}
	var welcome proto.Welcome
	json.Unmarshal(raw, &welcome)
	if welcome.Type != proto.MsgWelcome {
		log.Fatalf("login fallito: %s", raw)
	}
	log.Printf("collegato come utente %d, sessione %d", welcome.UserID, welcome.Session)

	host, _, _ := net.SplitHostPort(*server)
	uaddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(welcome.UDPPort)))
	if err != nil {
		log.Fatal(err)
	}
	udp, err := net.DialUDP("udp", nil, uaddr)
	if err != nil {
		log.Fatal(err)
	}
	ch := uint16(*channel)
	wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgState, Talk: []uint16{ch}, Listen: []uint16{ch}})
	udp.Write(proto.ClientPacket{Kind: proto.KindKeepalive, Session: welcome.Session}.Marshal())

	go func() {
		for {
			var m json.RawMessage
			if wsjson.Read(ctx, conn, &m) != nil {
				return
			}
			log.Printf("controllo: %s", m)
		}
	}()

	var mu sync.Mutex
	counts := map[uint16]int{}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, err := udp.Read(buf)
			if err != nil {
				return
			}
			if p, err := proto.ParseServer(buf[:n]); err == nil {
				mu.Lock()
				counts[p.TalkerID]++
				mu.Unlock()
			}
		}
	}()

	if *talk > 0 {
		wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgPTT, Active: true, Target: proto.Target{Kind: proto.TargetChannels}})
		payload := make([]byte, 80)
		frames := int(*talk / (20 * time.Millisecond))
		for seq := 0; seq < frames; seq++ {
			udp.Write(proto.ClientPacket{Kind: proto.KindVoice, Session: welcome.Session, Seq: uint16(seq), Payload: payload}.Marshal())
			time.Sleep(20 * time.Millisecond)
		}
		wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgPTT, Active: false, Target: proto.Target{Kind: proto.TargetChannels}})
		log.Printf("inviati %d pacchetti", frames)
	}

	time.Sleep(*listen)
	mu.Lock()
	fmt.Println("pacchetti ricevuti per utente:", counts)
	mu.Unlock()
}
```

- [ ] **Step 4: Dockerfile e README**

`server/Dockerfile`:

```dockerfile
FROM golang:1 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /intercom-server .

FROM gcr.io/distroless/static
COPY --from=build /intercom-server /intercom-server
WORKDIR /data
ENTRYPOINT ["/intercom-server", "-config", "/data/intercom-config.json"]
```

`server/README.md`:

````markdown
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
````

- [ ] **Step 5: Esegui tutti i test con il race detector**

Run: `cd server && go vet ./... && go test -race ./...`
Expected: `ok` per `proto`, `config`, `hub`, `web`; nessun `DATA RACE`.

- [ ] **Step 6: Cross-compila**

Run:
```bash
cd server
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o dist/intercom-server-mac .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o dist/intercom-server.exe .
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o dist/intercom-server-linux .
ls -la dist
```
Expected: tre binari, nessun errore.

- [ ] **Step 7: Verifica end-to-end**

```bash
cd server && rm -f /tmp/ic-e2e.json && go run . -config /tmp/ic-e2e.json
```

In un secondo terminale (setup e dati di prova via API):

```bash
J=/tmp/ic-cookies
curl -s -c $J -X POST localhost:8080/api/setup -d '{"password":"admin1"}'
curl -s -b $J -X POST localhost:8080/api/channels -d '{"name":"Regia"}'
curl -s -b $J -X POST localhost:8080/api/users -d '{"name":"anna","password":"pw","enabled":true,"channels":[{"channelId":1,"talk":true,"listen":true}]}'
curl -s -b $J -X POST localhost:8080/api/users -d '{"name":"bruno","password":"pw","enabled":true,"channels":[{"channelId":1,"talk":true,"listen":true}]}'
dns-sd -B _intercom._tcp local. & sleep 2; kill %1
go run ./cmd/testclient -user bruno -pass pw -channel 1 -listen 6s &
sleep 1
go run ./cmd/testclient -user anna -pass pw -channel 1 -talk 2s -listen 1s
wait
```

Expected:
- `dns-sd` mostra un'istanza `Intercom` di tipo `_intercom._tcp`.
- Il client di bruno stampa `pacchetti ricevuti per utente: map[2:100]` (±2), quello di anna `map[]`.
- Aprendo `http://localhost:8080` nel browser e facendo login con `admin1`: le schede Panoramica, Canali, Utenti e Impostazioni funzionano; creare e modificare un utente dal dialogo aggiorna la tabella.

- [ ] **Step 8: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add server
git commit -m "feat(server): mDNS, main binary, test client, Docker

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
