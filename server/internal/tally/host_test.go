package tally

import "testing"

func TestParseHost(t *testing.T) {
	for _, c := range []struct {
		in   string
		addr string
		ok   bool
	}{
		{"192.168.1.240", "192.168.1.240:9910", true},
		{"127.0.0.1:19910", "127.0.0.1:19910", true},
		{"::1", "[::1]:9910", true},
		{"[::1]:19910", "[::1]:19910", true},
		{"127.0.0.1:0", "", false},
		{"127.0.0.1:99999", "", false},
		{"127.0.0.1:abc", "", false},
		{"non-un-ip", "", false},
		{"mixer.local:9910", "", false},
	} {
		addr, ok := ParseHost(c.in, 9910)
		if addr != c.addr || ok != c.ok {
			t.Errorf("ParseHost(%q) = %q, %v; want %q, %v", c.in, addr, ok, c.addr, c.ok)
		}
	}
}
