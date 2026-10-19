package tally

import (
	"context"
	"errors"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Client keeps one read-only connection to an ATEM and publishes every change of its state.
type Client struct {
	OnChange func(Snapshot) // called outside any Client lock, from the connection goroutine
	Port     int            // 0 = 9910
	Silence  time.Duration  // no packet for this long = switcher lost; 0 = 5 s

	cfgMu sync.Mutex // serializes Configure
	stop  context.CancelFunc
	done  chan struct{}

	mu   sync.Mutex
	snap Snapshot
}

// Configure stops the current connection (waiting for it to end) and starts a new one
// when enabled with a host. The snapshot goes back to "not connected" either way.
func (c *Client) Configure(enabled bool, host string) {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()
	if c.stop != nil {
		c.stop()
		<-c.done
		c.stop = nil
	}
	c.publish(Snapshot{})
	if !enabled || host == "" {
		return
	}
	port, silence := c.Port, c.Silence
	if port == 0 {
		port = atemPort
	}
	if silence == 0 {
		silence = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.stop, c.done = cancel, done
	go func() {
		defer close(done)
		addr, _ := ParseHost(host, port) // validated by the admin API
		quiet := false                   // log an outage once, not every retry
		for {
			connected, err := c.session(ctx, addr, silence)
			if connected {
				quiet = false
			}
			if err != nil && ctx.Err() == nil && !quiet {
				log.Printf("tally: ATEM %s non raggiungibile: %v", addr, err)
				quiet = true
			}
			c.publish(Snapshot{})
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
}

// ParseHost accepts "IP" or "IP:port" (port 1–65535) and returns the address to dial,
// using defPort when none is given. Names are refused: the switcher is set by IP.
func ParseHost(s string, defPort int) (string, bool) {
	ip, port := s, defPort
	if h, p, err := net.SplitHostPort(s); err == nil {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 0xffff {
			return "", false
		}
		ip, port = h, n
	}
	if net.ParseIP(ip) == nil {
		return "", false
	}
	return net.JoinHostPort(ip, strconv.Itoa(port)), true
}

func (c *Client) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

func (c *Client) publish(s Snapshot) {
	s.Inputs, s.Program, s.Preview = slices.Clone(s.Inputs), slices.Clone(s.Program), slices.Clone(s.Preview)
	c.mu.Lock()
	same := reflect.DeepEqual(c.snap, s)
	c.snap = s
	c.mu.Unlock()
	if !same && c.OnChange != nil {
		c.OnChange(s)
	}
}

// session runs one connection until silence, an error, or ctx ends; connected reports
// whether the switcher completed its state dump.
func (c *Client) session(ctx context.Context, addr string, silence time.Duration) (connected bool, err error) {
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return false, err
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	ended := make(chan struct{})
	defer close(ended)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close() // unblocks Read
		case <-ended:
		}
	}()

	session := uint16(rand.IntN(0x7fff) + 1)
	if _, err := conn.Write(helloPacket(session)); err != nil {
		return false, err
	}
	var cur Snapshot
	var last uint16 // id of the last reliable packet applied
	ready := false
	buf := make([]byte, 2048)
	for {
		conn.SetReadDeadline(time.Now().Add(silence))
		n, err := conn.Read(buf)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return ready, errors.New("nessuna risposta")
		}
		if err != nil {
			return ready, err
		}
		h, ok := parseHeader(buf[:n])
		if !ok {
			continue
		}
		session = h.session
		if h.flags&flagHello != 0 {
			last = h.remoteID
			conn.Write(ackPacket(session, last))
			continue
		}
		if h.flags&flagAckRequest == 0 {
			continue
		}
		// Apply only the next packet in sequence; ack the last one applied so the switcher
		// resends anything after a gap. Duplicates and out-of-order packets are dropped.
		if h.remoteID != (last+1)&0x7fff {
			conn.Write(ackPacket(session, last))
			continue
		}
		last = h.remoteID
		conn.Write(ackPacket(session, last))
		if cur.apply(buf[headerLen:h.length]) {
			ready = true
		}
		if ready {
			cur.Connected = true
			c.publish(cur)
		}
	}
}
