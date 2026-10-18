package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"intercom/internal/config"
	"intercom/internal/hub"
)

// tallyServer is adminServer with a hook recording OnTally calls.
func tallyServer(t *testing.T) (*httptest.Server, *http.Client, *[]config.TallySettings) {
	t.Helper()
	store, err := config.Open(filepath.Join(t.TempDir(), "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []config.TallySettings
	s := New(store, hub.New(store))
	s.OnTally = func(ts config.TallySettings) { calls = append(calls, ts) }
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	c := newClient()
	if code, body := call(t, c, srv, "POST", "/api/setup", map[string]string{"password": "admin1"}); code != 200 {
		t.Fatalf("setup = %d %s", code, body)
	}
	return srv, c, &calls
}

func TestPutTallyValidates(t *testing.T) {
	srv, c, calls := tallyServer(t)
	bad := []map[string]any{
		{"enabled": true, "interface": "atem", "host": "non-un-ip"},
		{"enabled": true, "interface": "atem", "host": " "},
		{"enabled": false, "interface": "vmix", "host": ""},
	}
	for _, b := range bad {
		if code, _ := call(t, c, srv, "PUT", "/api/tally", b); code != 400 {
			t.Errorf("%v = %d, want 400", b, code)
		}
	}
	if code, body := call(t, c, srv, "PUT", "/api/tally", map[string]any{"enabled": true, "interface": "atem", "host": " 192.168.1.240 "}); code != 200 {
		t.Fatalf("valid = %d %s", code, body)
	}
	_, body := call(t, c, srv, "GET", "/api/config", nil)
	var cfg struct{ Tally config.TallySettings }
	json.Unmarshal([]byte(body), &cfg)
	want := config.TallySettings{Enabled: true, Interface: "atem", Host: "192.168.1.240"}
	if cfg.Tally != want {
		t.Errorf("config tally = %+v", cfg.Tally)
	}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Errorf("OnTally calls = %+v", *calls)
	}
}

func TestAssignTallyInput(t *testing.T) {
	srv, c, _ := tallyServer(t)
	call(t, c, srv, "POST", "/api/users", map[string]any{"name": "mario", "password": "pw12", "enabled": true})
	_, body := call(t, c, srv, "GET", "/api/config", nil)
	var cfg struct {
		Users []struct {
			ID         uint16 `json:"id"`
			TallyInput uint16 `json:"tallyInput"`
		}
	}
	json.Unmarshal([]byte(body), &cfg)
	id := cfg.Users[0].ID
	path := "/api/tally/users/" + itoa(id)
	if code, body := call(t, c, srv, "PUT", path, map[string]int{"input": 3}); code != 200 {
		t.Fatalf("assign = %d %s", code, body)
	}
	if code, _ := call(t, c, srv, "PUT", "/api/tally/users/999", map[string]int{"input": 3}); code != 404 {
		t.Errorf("unknown user = %d, want 404", code)
	}
	call(t, c, srv, "PUT", "/api/users/"+itoa(id), map[string]any{"name": "mario", "enabled": true})
	_, body = call(t, c, srv, "GET", "/api/config", nil)
	json.Unmarshal([]byte(body), &cfg)
	if cfg.Users[0].TallyInput != 3 {
		t.Errorf("tallyInput after user edit = %d, want 3", cfg.Users[0].TallyInput)
	}
}

func TestGetTallyRequiresAuth(t *testing.T) {
	srv, c, _ := tallyServer(t)
	if code, _ := call(t, &http.Client{}, srv, "GET", "/api/tally", nil); code != 401 {
		t.Errorf("anonymous = %d, want 401", code)
	}
	if code, body := call(t, c, srv, "GET", "/api/tally", nil); code != 200 || body == "" {
		t.Errorf("admin = %d %s", code, body)
	}
}

func itoa(id uint16) string { return strconv.Itoa(int(id)) }
