package proto

import (
	"bytes"
	"testing"
)

func TestClientPacketRoundTrip(t *testing.T) {
	in := ClientPacket{Kind: KindVoice, Session: 0xDEADBEEF, Seq: 513, Payload: []byte{9, 8, 7}}
	b := in.Marshal()
	if want := []byte{1, 0xDE, 0xAD, 0xBE, 0xEF, 0x02, 0x01, 9, 8, 7}; !bytes.Equal(b, want) {
		t.Fatalf("Marshal = %v, want %v", b, want)
	}
	out, err := ParseClient(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != in.Kind || out.Session != in.Session || out.Seq != in.Seq || !bytes.Equal(out.Payload, in.Payload) {
		t.Errorf("ParseClient = %+v, want %+v", out, in)
	}
}

func TestServerPacketRoundTrip(t *testing.T) {
	in := ServerPacket{Kind: KindVoice, TalkerID: 10, ChannelID: ChanPrivate, Seq: 7, Payload: []byte{1}}
	b := in.Marshal()
	if want := []byte{1, 0, 10, 0xFF, 0xFF, 0, 7, 1}; !bytes.Equal(b, want) {
		t.Fatalf("Marshal = %v, want %v", b, want)
	}
	out, err := ParseServer(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.TalkerID != 10 || out.ChannelID != ChanPrivate || out.Seq != 7 || !bytes.Equal(out.Payload, []byte{1}) {
		t.Errorf("ParseServer = %+v", out)
	}
}

func TestShortPacketsRejected(t *testing.T) {
	if _, err := ParseClient([]byte{1, 2, 3}); err != ErrShort {
		t.Errorf("ParseClient short: err = %v", err)
	}
	if _, err := ParseServer(nil); err != ErrShort {
		t.Errorf("ParseServer nil: err = %v", err)
	}
}
