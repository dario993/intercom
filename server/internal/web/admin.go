package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"intercom/internal/config"
	"intercom/internal/tally"
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
	TallyInput  uint16              `json:"tallyInput"`
}

type publicConfig struct {
	Settings publicSettings       `json:"settings"`
	Tally    config.TallySettings `json:"tally"`
	Channels []config.Channel     `json:"channels"`
	Users    []publicUser         `json:"users"`
}

// getConfig returns the configuration without any password hash.
func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	out := publicConfig{Channels: []config.Channel{}, Users: []publicUser{}}
	s.Store.View(func(c *config.Config) {
		out.Settings = publicSettings{c.Settings.ServerName, c.Settings.HTTPPort, c.Settings.UDPPort}
		out.Tally = c.Settings.Tally
		out.Channels = append(out.Channels, c.Channels...)
		for _, u := range c.Users {
			out.Users = append(out.Users, publicUser{u.ID, u.Name, u.Enabled, append([]config.Assignment{}, u.Channels...), u.AllCall, u.PrivateCall, u.TallyInput})
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

func (s *Server) getTally(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Hub.Tally())
}

func hostOK(h string) bool { _, ok := tally.ParseHost(h, 9910); return ok }

func (s *Server) putTally(w http.ResponseWriter, r *http.Request) {
	var in config.TallySettings
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	in.Host = strings.TrimSpace(in.Host)
	switch {
	case in.Interface != "atem":
		writeErr(w, bad("interfaccia non supportata"))
		return
	case in.Host != "" && !hostOK(in.Host):
		writeErr(w, bad("indirizzo non valido: scrivi IP oppure IP:porta"))
		return
	case in.Enabled && in.Host == "":
		writeErr(w, bad("inserisci l'indirizzo IP del mixer"))
		return
	}
	if err := s.Store.Update(func(c *config.Config) error { c.Settings.Tally = in; return nil }); err != nil {
		writeErr(w, err)
		return
	}
	s.Hub.ConfigChanged()
	if s.OnTally != nil {
		s.OnTally(in)
	}
	writeOK(w)
}

func (s *Server) assignTally(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Input uint16 `json:"input"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s.update(w, func(c *config.Config) error {
		u := c.UserByID(id)
		if u == nil {
			return errNotFound
		}
		u.TallyInput = in.Input
		return nil
	})
}

// exportConfig downloads the whole configuration, password hashes included, to move it to another machine.
func (s *Server) exportConfig(w http.ResponseWriter, r *http.Request) {
	var b []byte
	var err error
	s.Store.View(func(c *config.Config) { b, err = json.MarshalIndent(c, "", "  ") })
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="intercom-config.json"`)
	w.Write(b)
}

// importConfig replaces the whole configuration with an exported one.
func (s *Server) importConfig(w http.ResponseWriter, r *http.Request) {
	in := config.Default()
	if err := readJSON(r, in); err != nil {
		writeErr(w, err)
		return
	}
	if err := validImport(in); err != nil {
		writeErr(w, err)
		return
	}
	var saved config.Settings
	err := s.Store.Update(func(c *config.Config) error {
		if in.Settings.AdminHash == "" {
			in.Settings.AdminHash = c.Settings.AdminHash
		}
		*c = *in
		saved = c.Settings
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	s.Hub.ConfigChanged()
	if s.OnSettings != nil {
		s.OnSettings(saved)
	}
	if s.OnTally != nil {
		s.OnTally(saved.Tally)
	}
	writeOK(w)
}

// validImport rejects files the admin API could never have produced, and fixes NextID.
func validImport(c *config.Config) error {
	if strings.TrimSpace(c.Settings.ServerName) == "" || !validPort(c.Settings.HTTPPort) || !validPort(c.Settings.UDPPort) {
		return bad("file non valido: impostazioni del server")
	}
	if c.Settings.Tally.Interface != "atem" || (c.Settings.Tally.Host != "" && !hostOK(c.Settings.Tally.Host)) {
		return bad("file non valido: impostazioni tally")
	}
	ids := map[uint16]bool{}
	var maxID uint16
	useID := func(id uint16) bool {
		if id == 0 || id == 0xFFFF || ids[id] {
			return false
		}
		ids[id] = true
		maxID = max(maxID, id)
		return true
	}
	for _, ch := range c.Channels {
		if !useID(ch.ID) || strings.TrimSpace(ch.Name) == "" {
			return bad("file non valido: canale " + ch.Name)
		}
	}
	names := map[string]bool{}
	for _, u := range c.Users {
		key := strings.ToLower(strings.TrimSpace(u.Name))
		if !useID(u.ID) || key == "" || names[key] || u.PasswordHash == "" {
			return bad("file non valido: utente " + u.Name)
		}
		names[key] = true
		seen := map[uint16]bool{}
		for _, a := range u.Channels {
			if c.ChannelByID(a.ChannelID) == nil || seen[a.ChannelID] {
				return bad("file non valido: canali dell'utente " + u.Name)
			}
			seen[a.ChannelID] = true
		}
	}
	if c.NextID <= maxID {
		c.NextID = maxID + 1
	}
	return nil
}
