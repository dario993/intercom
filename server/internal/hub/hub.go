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
