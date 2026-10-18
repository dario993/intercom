package tally

import (
	"net"
	"sync"
	"time"
)

// FakeATEM is a minimal switcher for tests and for trying the tally without hardware:
// it answers the handshake, sends its state, pings every 500 ms and pushes changes made with Set.
type FakeATEM struct {
	conn      *net.UDPConn
	closeOnce sync.Once
	closed    chan struct{}

	mu       sync.Mutex
	state    Snapshot
	client   *net.UDPAddr
	session  uint16
	seq      uint16
	received int
}

func NewFakeATEM(addr string) (*FakeATEM, error) {
	ua, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		return nil, err
	}
	f := &FakeATEM{conn: conn, closed: make(chan struct{}), state: Snapshot{
		Model: "ATEM Production Studio 4K", Program: []uint16{1}, Preview: []uint16{2}, ProgramSource: 1,
	}}
	for i := uint16(1); i <= 8; i++ {
		f.state.Inputs = append(f.state.Inputs, Input{ID: i, Name: "CAM " + string(rune('0'+i)), External: true})
	}
	f.state.Inputs = append(f.state.Inputs, Input{ID: 3010, Name: "MP1"})
	go f.serve()
	go f.ping()
	return f, nil
}

func (f *FakeATEM) Port() int { return f.conn.LocalAddr().(*net.UDPAddr).Port }

func (f *FakeATEM) Close() {
	f.closeOnce.Do(func() {
		close(f.closed)
		f.conn.Close()
	})
}

// Set changes the switcher state and sends the tally-related commands to the client.
func (f *FakeATEM) Set(fn func(*Snapshot)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(&f.state)
	f.sendLocked(f.liveCommands())
}

func (f *FakeATEM) receivedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.received
}

func (f *FakeATEM) serve() {
	buf := make([]byte, 2048)
	for {
		n, addr, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		h, ok := parseHeader(buf[:n])
		if !ok {
			continue
		}
		f.mu.Lock()
		f.received++
		if h.flags&flagHello != 0 {
			f.client, f.session, f.seq = addr, 0x8001, 0
			f.conn.WriteToUDP(packet(flagHello, h.session, 0, []byte{0x02, 0, 0, 0, 0, 0, 0, 0}), addr)
			var dump []byte
			dump = append(dump, command("_pin", append([]byte(f.state.Model), make([]byte, 44-len(f.state.Model))...))...)
			for _, in := range f.state.Inputs {
				d := make([]byte, 36)
				be.PutUint16(d, in.ID)
				copy(d[2:22], in.Name)
				if !in.External {
					d[32] = 1
				}
				dump = append(dump, command("InPr", d)...)
			}
			dump = append(dump, f.liveCommands()...)
			f.sendLocked(append(dump, command("InCm", []byte{1, 0, 0, 0})...))
		}
		f.mu.Unlock()
	}
}

func (f *FakeATEM) ping() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-f.closed:
			return
		case <-t.C:
			f.mu.Lock()
			f.sendLocked(nil)
			f.mu.Unlock()
		}
	}
}

func (f *FakeATEM) liveCommands() []byte {
	s := f.state
	var tl []byte
	tl = be.AppendUint16(tl, uint16(len(s.Inputs)))
	for _, in := range s.Inputs {
		var fl byte
		for _, p := range s.Program {
			if p == in.ID {
				fl |= 0x01
			}
		}
		for _, p := range s.Preview {
			if p == in.ID {
				fl |= 0x02
			}
		}
		tl = append(be.AppendUint16(tl, in.ID), fl)
	}
	black := byte(0)
	if s.Black {
		black = 1
	}
	out := command("PrgI", be.AppendUint16([]byte{0, 0}, s.ProgramSource))
	out = append(out, command("TlSr", tl)...)
	return append(out, command("FtbS", []byte{0, black, 0, 0})...)
}

func (f *FakeATEM) sendLocked(payload []byte) {
	if f.client == nil {
		return
	}
	f.seq++
	f.conn.WriteToUDP(packet(flagAckRequest, f.session, f.seq, payload), f.client)
}
