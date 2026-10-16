// Package web serves the client WebSocket, the admin API and the admin page.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"sync"

	"intercom/internal/config"
	"intercom/internal/hub"
)

//go:embed static
var staticFiles embed.FS

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
	static, _ := fs.Sub(staticFiles, "static")
	mux.Handle("GET /", http.FileServerFS(static))
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
