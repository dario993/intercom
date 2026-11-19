package main

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

type fakeReader struct{ steps []error }

func (f *fakeReader) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	err := f.steps[0]
	f.steps = f.steps[1:]
	if err != nil {
		return 0, nil, err
	}
	return copy(b, "pkt"), &net.UDPAddr{Port: 1}, nil
}

func TestReadLoopSurvivesTransientErrors(t *testing.T) {
	// e.g. Windows reports ICMP "port unreachable" from a vanished phone as ECONNRESET on the next read.
	r := &fakeReader{steps: []error{syscall.ECONNRESET, nil, net.ErrClosed}}
	n := 0
	err := readLoop(r, func(*net.UDPAddr, []byte) { n++ })
	if !errors.Is(err, net.ErrClosed) || n != 1 {
		t.Errorf("err = %v, handled = %d; want ErrClosed after 1 packet", err, n)
	}
}

func TestParseChoice(t *testing.T) {
	addrs := []lanAddr{{ip: net.IPv4(192, 168, 1, 10).To4()}, {ip: net.IPv4(10, 8, 0, 2).To4()}}
	for _, c := range []struct {
		in   string
		want string // "" = all
		ok   bool
	}{
		{"0", "", true}, {"", "", true}, {"Tutti", "", true},
		{"1", "192.168.1.10", true}, {" 2 ", "10.8.0.2", true}, {"10.8.0.2", "10.8.0.2", true},
		{"3", "", false}, {"abc", "", false}, {"1.2.3.4", "", false},
	} {
		a, ok := parseChoice(c.in, addrs)
		got := ""
		if a != nil {
			got = a.ip.String()
		}
		if ok != c.ok || got != c.want {
			t.Errorf("parseChoice(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
