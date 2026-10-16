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
