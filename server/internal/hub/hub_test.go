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
