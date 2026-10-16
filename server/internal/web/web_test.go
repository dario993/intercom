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
