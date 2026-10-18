package tally

import (
	"bytes"
	"encoding/binary"
	"slices"
)

// ATEM control protocol over UDP 9910. Not published by Blackmagic; layout from
// OpenSwitcher (docs.openswitcher.org) and nrkno/sofie-atem-connection.
//
// Packet: u16 flags<<11|length, u16 session, u16 ackID, u16 unknown, u16 unknown, u16 remoteID,
// then commands: u16 length (incl. 8-byte head), u16 0, 4-byte name, data.

const atemPort = 9910

const (
	flagAckRequest        = 0x01 // reliable: must be acked
	flagHello             = 0x02 // handshake (NewSessionId)
	flagRetransmit        = 0x04
	flagRetransmitRequest = 0x08
	flagAck               = 0x10

	headerLen = 12
)

type header struct {
	flags                    uint8
	length                   int
	session, ackID, remoteID uint16
}

var be = binary.BigEndian

func parseHeader(b []byte) (header, bool) {
	if len(b) < headerLen {
		return header{}, false
	}
	w := be.Uint16(b)
	h := header{flags: uint8(w >> 11), length: int(w & 0x07ff), session: be.Uint16(b[2:]), ackID: be.Uint16(b[4:]), remoteID: be.Uint16(b[10:])}
	if h.length < headerLen || h.length > len(b) {
		return header{}, false
	}
	return h, true
}

func packet(flags uint8, session, remoteID uint16, payload []byte) []byte {
	b := make([]byte, headerLen+len(payload))
	be.PutUint16(b, uint16(flags)<<11|uint16(len(b)))
	be.PutUint16(b[2:], session)
	be.PutUint16(b[10:], remoteID)
	copy(b[headerLen:], payload)
	return b
}

func helloPacket(session uint16) []byte {
	b := packet(flagHello, session, 0, []byte{0x01, 0, 0, 0, 0, 0, 0, 0})
	b[9] = 0x3a
	return b
}

func ackPacket(session, id uint16) []byte {
	b := packet(flagAck, session, 0, nil)
	be.PutUint16(b[4:], id)
	return b
}

func command(name string, data []byte) []byte {
	b := make([]byte, 8, 8+len(data))
	be.PutUint16(b, uint16(8+len(data)))
	copy(b[4:], name)
	return append(b, data...)
}

func cstring(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// apply updates s from the commands in payload and reports whether the initial
// state dump is complete (InCm). Malformed commands end the walk; nothing panics.
func (s *Snapshot) apply(payload []byte) (initDone bool) {
	for len(payload) >= 8 {
		n := int(be.Uint16(payload))
		if n < 8 || n > len(payload) {
			return initDone
		}
		name, d := string(payload[4:8]), payload[8:n]
		payload = payload[n:]
		switch name {
		case "InCm":
			initDone = true
		case "_pin":
			s.Model = cstring(d[:min(len(d), 40)])
		case "InPr":
			if len(d) < 33 {
				continue
			}
			in := Input{ID: be.Uint16(d), Name: cstring(d[2:22]), External: d[32] == 0}
			if i := slices.IndexFunc(s.Inputs, func(x Input) bool { return x.ID == in.ID }); i >= 0 {
				s.Inputs[i] = in
			} else {
				s.Inputs = append(s.Inputs, in)
			}
		case "TlSr":
			if len(d) < 2 {
				continue
			}
			count := int(be.Uint16(d))
			if len(d) < 2+3*count {
				continue
			}
			s.Program, s.Preview = nil, nil
			for i := range count {
				e := d[2+3*i:]
				id := be.Uint16(e)
				if e[2]&0x01 != 0 {
					s.Program = append(s.Program, id)
				}
				if e[2]&0x02 != 0 {
					s.Preview = append(s.Preview, id)
				}
			}
			slices.Sort(s.Program)
			slices.Sort(s.Preview)
		case "PrgI":
			if len(d) >= 4 && d[0] == 0 {
				s.ProgramSource = be.Uint16(d[2:])
			}
		case "FtbS":
			if len(d) >= 3 && d[0] == 0 {
				s.Black = d[1] != 0 && d[2] == 0
			}
		}
	}
	return initDone
}
