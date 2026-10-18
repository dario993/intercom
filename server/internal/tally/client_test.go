package tally

import (
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

// recorder collects published snapshots.
type recorder struct {
	mu   sync.Mutex
	last Snapshot
	n    int
}

func (r *recorder) on(s Snapshot) {
	r.mu.Lock()
	r.last, r.n = s, r.n+1
	r.mu.Unlock()
}

func (r *recorder) wait(t *testing.T, d time.Duration, what string, ok func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		s := r.last
		r.mu.Unlock()
		if ok(s) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s; last = %+v", what, r.last)
	return Snapshot{}
}

func newFake(t *testing.T, addr string) *FakeATEM {
	t.Helper()
	f, err := NewFakeATEM(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f
}

func newClient(t *testing.T, r *recorder, port int) *Client {
	c := &Client{OnChange: r.on, Port: port, Silence: 300 * time.Millisecond}
	t.Cleanup(func() { c.Configure(false, "") })
	return c
}

func TestClientReceivesState(t *testing.T) {
	f := newFake(t, "127.0.0.1:0")
	r := &recorder{}
	c := newClient(t, r, f.Port())
	c.Configure(true, "127.0.0.1")
	s := r.wait(t, 2*time.Second, "connected", func(s Snapshot) bool { return s.Connected })
	if s.Model != "ATEM Production Studio 4K" || len(s.Inputs) != 9 || !slices.Equal(s.Program, []uint16{1}) || !slices.Equal(s.Preview, []uint16{2}) {
		t.Fatalf("snapshot = %+v", s)
	}
	f.Set(func(s *Snapshot) { s.Program, s.ProgramSource = []uint16{3}, 3 })
	r.wait(t, time.Second, "program 3", func(s Snapshot) bool { return slices.Equal(s.Program, []uint16{3}) && s.ProgramSource == 3 })
	if got := c.Snapshot(); !got.Connected {
		t.Errorf("Snapshot() = %+v", got)
	}
}

func TestClientDetectsSilenceAndReconnects(t *testing.T) {
	f := newFake(t, "127.0.0.1:0")
	port := f.Port()
	r := &recorder{}
	c := newClient(t, r, port)
	c.Configure(true, "127.0.0.1")
	r.wait(t, 2*time.Second, "connected", func(s Snapshot) bool { return s.Connected })
	f.Close()
	r.wait(t, time.Second, "disconnected", func(s Snapshot) bool { return !s.Connected })
	newFake(t, "127.0.0.1:"+strconv.Itoa(port))
	r.wait(t, 3*time.Second, "reconnected", func(s Snapshot) bool { return s.Connected })
}

func TestConfigureSwitchesHost(t *testing.T) {
	a := newFake(t, "127.0.0.1:0")
	b := newFake(t, "127.0.0.1:0")
	b.Set(func(s *Snapshot) { s.Model = "Fake B" })
	r := &recorder{}
	c := newClient(t, r, a.Port())
	c.Configure(true, "127.0.0.1")
	r.wait(t, 2*time.Second, "A", func(s Snapshot) bool { return s.Connected && s.Model == "ATEM Production Studio 4K" })

	c.Port = b.Port()
	c.Configure(true, "127.0.0.1")
	r.wait(t, 2*time.Second, "B", func(s Snapshot) bool { return s.Connected && s.Model == "Fake B" })
	before := a.receivedCount()
	time.Sleep(700 * time.Millisecond) // A pings every 500 ms: an old loop would ack it
	if after := a.receivedCount(); after != before {
		t.Errorf("old connection still talking to A: %d → %d packets", before, after)
	}

	c.Configure(false, "")
	r.wait(t, time.Second, "off", func(s Snapshot) bool { return !s.Connected })
}

func TestClientAppliesPacketsInOrderOnly(t *testing.T) {
	f := newFake(t, "127.0.0.1:0")
	r := &recorder{}
	c := newClient(t, r, f.Port())
	c.Silence = 2 * time.Second
	c.Configure(true, "127.0.0.1")
	r.wait(t, 2*time.Second, "connected", func(s Snapshot) bool { return s.Connected })
	f.pausePings()
	time.Sleep(100 * time.Millisecond)

	onAir := func(id uint16) []byte { return command("TlSr", []byte{0, 1, byte(id >> 8), byte(id), 0x01}) }
	program := func() []uint16 { return c.Snapshot().Program }
	settle := func() { time.Sleep(150 * time.Millisecond) }
	n := f.nextSeq() // the next id the client expects

	f.sendRaw(n+1, onAir(5)) // n is lost: n+1 must wait for it
	settle()
	if !slices.Equal(program(), []uint16{1}) || f.lastAck() != n-1 {
		t.Fatalf("after gap: program %v, last ack %d (want [1], %d)", program(), f.lastAck(), n-1)
	}
	f.sendRaw(n, onAir(4))   // the lost one, resent
	f.sendRaw(n+1, onAir(5)) // and the next, resent
	settle()
	if !slices.Equal(program(), []uint16{5}) || f.lastAck() != n+1 {
		t.Fatalf("after resend: program %v, last ack %d (want [5], %d)", program(), f.lastAck(), n+1)
	}
	f.sendRaw(n, onAir(4)) // late duplicate must not roll the state back
	settle()
	if !slices.Equal(program(), []uint16{5}) || f.lastAck() != n+1 {
		t.Errorf("after duplicate: program %v, last ack %d", program(), f.lastAck())
	}
}
