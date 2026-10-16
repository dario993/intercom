// Package proto defines the wire format shared by server and clients.
package proto

import (
	"encoding/binary"
	"errors"
)

const (
	KindVoice     = 1
	KindKeepalive = 2

	ChanAll     uint16 = 0
	ChanPrivate uint16 = 0xFFFF

	headerLen = 7
)

var ErrShort = errors.New("packet too short")

// ClientPacket is sent by a client: u8 kind | u32 session | u16 seq | payload.
type ClientPacket struct {
	Kind    uint8
	Session uint32
	Seq     uint16
	Payload []byte
}

func (p ClientPacket) Marshal() []byte {
	b := make([]byte, headerLen+len(p.Payload))
	b[0] = p.Kind
	binary.BigEndian.PutUint32(b[1:], p.Session)
	binary.BigEndian.PutUint16(b[5:], p.Seq)
	copy(b[headerLen:], p.Payload)
	return b
}

// ParseClient decodes b; the returned Payload aliases b.
func ParseClient(b []byte) (ClientPacket, error) {
	if len(b) < headerLen {
		return ClientPacket{}, ErrShort
	}
	return ClientPacket{
		Kind:    b[0],
		Session: binary.BigEndian.Uint32(b[1:]),
		Seq:     binary.BigEndian.Uint16(b[5:]),
		Payload: b[headerLen:],
	}, nil
}

// ServerPacket is sent by the server: u8 kind | u16 talkerId | u16 channelId | u16 seq | payload.
type ServerPacket struct {
	Kind      uint8
	TalkerID  uint16
	ChannelID uint16
	Seq       uint16
	Payload   []byte
}

func (p ServerPacket) Marshal() []byte {
	b := make([]byte, headerLen+len(p.Payload))
	b[0] = p.Kind
	binary.BigEndian.PutUint16(b[1:], p.TalkerID)
	binary.BigEndian.PutUint16(b[3:], p.ChannelID)
	binary.BigEndian.PutUint16(b[5:], p.Seq)
	copy(b[headerLen:], p.Payload)
	return b
}

// ParseServer decodes b; the returned Payload aliases b.
func ParseServer(b []byte) (ServerPacket, error) {
	if len(b) < headerLen {
		return ServerPacket{}, ErrShort
	}
	return ServerPacket{
		Kind:      b[0],
		TalkerID:  binary.BigEndian.Uint16(b[1:]),
		ChannelID: binary.BigEndian.Uint16(b[3:]),
		Seq:       binary.BigEndian.Uint16(b[5:]),
		Payload:   b[headerLen:],
	}, nil
}

// Control message types (WebSocket JSON "type" field).
const (
	MsgHello   = "hello"
	MsgState   = "state"
	MsgPTT     = "ptt"
	MsgPing    = "ping"
	MsgWelcome = "welcome"
	MsgConfig  = "config"
	MsgUsers   = "users"
	MsgTalking = "talking"
	MsgKicked  = "kicked"
	MsgError   = "error"
	MsgPong    = "pong"
)

const (
	TargetChannels = "channels"
	TargetAll      = "all"
	TargetUser     = "user"

	ErrBadCredentials = "bad_credentials"
	ErrDisabled       = "disabled"

	KickReplaced = "replaced"
	KickAdmin    = "admin"
	KickDisabled = "disabled"
)

type Target struct {
	Kind   string `json:"kind"`
	UserID uint16 `json:"userId,omitempty"`
}

// ClientMsg is any message a client sends; only the fields of its Type are set.
type ClientMsg struct {
	Type     string   `json:"type"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	Device   string   `json:"device,omitempty"`
	Talk     []uint16 `json:"talk,omitempty"`
	Listen   []uint16 `json:"listen,omitempty"`
	Active   bool     `json:"active,omitempty"`
	Target   Target   `json:"target"`
}

type ChannelInfo struct {
	ID        uint16 `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CanTalk   bool   `json:"canTalk"`
	CanListen bool   `json:"canListen"`
}

type Perms struct {
	AllCall     bool `json:"allCall"`
	PrivateCall bool `json:"privateCall"`
}

type UserStatus struct {
	ID      uint16 `json:"id"`
	Name    string `json:"name"`
	Online  bool   `json:"online"`
	Talking bool   `json:"talking"`
}

type Welcome struct {
	Type       string        `json:"type"`
	UserID     uint16        `json:"userId"`
	Session    uint32        `json:"session"`
	UDPPort    int           `json:"udpPort"`
	ServerName string        `json:"serverName"`
	Channels   []ChannelInfo `json:"channels"`
	Perms      Perms         `json:"perms"`
}

type ConfigMsg struct {
	Type     string        `json:"type"`
	Channels []ChannelInfo `json:"channels"`
	Perms    Perms         `json:"perms"`
}

type UsersMsg struct {
	Type  string       `json:"type"`
	Users []UserStatus `json:"users"`
}

type Talking struct {
	Type       string   `json:"type"`
	UserID     uint16   `json:"userId"`
	Name       string   `json:"name"`
	Active     bool     `json:"active"`
	Target     Target   `json:"target"`
	ChannelIDs []uint16 `json:"channelIds"`
}

type Kicked struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type ErrorMsg struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Pong struct {
	Type string `json:"type"`
}
