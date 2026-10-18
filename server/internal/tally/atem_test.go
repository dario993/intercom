package tally

import (
	"bytes"
	"slices"
	"testing"
)

var helloReply = []byte{0x10, 0x14, 0x53, 0xab, 0, 0, 0, 0, 0, 0x3a, 0, 0, 0x02, 0, 0, 0, 0, 0, 0, 0}

func TestParseHeader(t *testing.T) {
	h, ok := parseHeader(helloReply)
	if !ok || h.flags != flagHello || h.length != 20 || h.session != 0x53ab {
		t.Fatalf("header = %+v ok=%v", h, ok)
	}
	rel := []byte{0x08, 0x0c, 0x80, 0x01, 0, 0, 0, 0, 0, 0, 0x00, 0x07}
	h, ok = parseHeader(rel)
	if !ok || h.flags != flagAckRequest || h.session != 0x8001 || h.remoteID != 7 {
		t.Fatalf("reliable header = %+v ok=%v", h, ok)
	}
	if _, ok := parseHeader(rel[:11]); ok {
		t.Error("short packet accepted")
	}
	if _, ok := parseHeader([]byte{0x08, 0x20, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); ok {
		t.Error("length beyond buffer accepted")
	}
}

func TestHelloAndAckBytes(t *testing.T) {
	want := []byte{0x10, 0x14, 0x53, 0xab, 0, 0, 0, 0, 0, 0x3a, 0, 0, 0x01, 0, 0, 0, 0, 0, 0, 0}
	if got := helloPacket(0x53ab); !bytes.Equal(got, want) {
		t.Errorf("hello = % x", got)
	}
	want = []byte{0x80, 0x0c, 0x80, 0x01, 0x00, 0x07, 0, 0, 0, 0, 0, 0}
	if got := ackPacket(0x8001, 7); !bytes.Equal(got, want) {
		t.Errorf("ack = % x", got)
	}
}

// raw builds a command by hand, independently of command().
func raw(name string, data ...byte) []byte {
	n := 8 + len(data)
	return append([]byte{byte(n >> 8), byte(n), 0, 0, name[0], name[1], name[2], name[3]}, data...)
}

func padded(s string, n int) []byte {
	b := make([]byte, n)
	copy(b, s)
	return b
}

func inPr(id uint16, name string, internalType byte) []byte {
	d := make([]byte, 36)
	d[0], d[1] = byte(id>>8), byte(id)
	copy(d[2:22], name)
	d[32] = internalType
	return raw("InPr", d...)
}

func TestApplyCommands(t *testing.T) {
	var p []byte
	p = append(p, raw("_pin", append(padded("ATEM Production Studio 4K", 40), 0, 0, 0, 0)...)...)
	p = append(p, inPr(1, "CAM 1", 0)...)
	p = append(p, inPr(3010, "MP1", 4)...)
	p = append(p, raw("TlSr", 0, 2, 0, 1, 0x01, 0, 2, 0x02)...)
	p = append(p, raw("PrgI", 0, 0, 0, 1)...)
	p = append(p, raw("FtbS", 0, 1, 1, 10)...)
	p = append(p, raw("InCm", 1, 0, 0, 0)...)

	var s Snapshot
	if !s.apply(p) {
		t.Error("InCm not reported")
	}
	if s.Model != "ATEM Production Studio 4K" {
		t.Errorf("model = %q", s.Model)
	}
	wantIn := []Input{{ID: 1, Name: "CAM 1", External: true}, {ID: 3010, Name: "MP1"}}
	if !slices.Equal(s.Inputs, wantIn) {
		t.Errorf("inputs = %+v", s.Inputs)
	}
	if !slices.Equal(s.Program, []uint16{1}) || !slices.Equal(s.Preview, []uint16{2}) || s.ProgramSource != 1 {
		t.Errorf("tally = %+v", s)
	}
	if s.Black {
		t.Error("black while fading")
	}
	s.apply(raw("FtbS", 0, 1, 0, 0))
	if !s.Black {
		t.Error("not black after fade completed")
	}
	s.apply(inPr(1, "Palco", 0))
	if len(s.Inputs) != 2 || s.Inputs[0].Name != "Palco" {
		t.Errorf("rename = %+v", s.Inputs)
	}
	if s.apply(raw("PrgI", 1, 0, 0, 5)); s.ProgramSource != 1 {
		t.Error("M/E 2 applied")
	}
}

func TestApplyIgnoresTruncatedCommands(t *testing.T) {
	good := raw("PrgI", 0, 0, 0, 4)
	for _, tail := range [][]byte{
		{0x01, 0xf4, 0, 0, 'T', 'l', 'S', 'r', 0}, // length 500
		{0, 4, 0, 0, 'T', 'l', 'S', 'r'},          // length < 8
		{0, 1, 2},                                 // leftovers
		raw("TlSr", 0, 9, 0, 1),                   // count beyond data
		raw("InPr", 0, 1),                         // short InPr
		raw("FtbS", 0),
	} {
		var s Snapshot
		s.apply(append(append([]byte{}, good...), tail...))
		if s.ProgramSource != 4 {
			t.Errorf("good command lost before % x", tail)
		}
	}
}
