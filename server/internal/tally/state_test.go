package tally

import (
	"testing"

	"intercom/internal/config"
	"intercom/internal/proto"
)

func TestForUser(t *testing.T) {
	base := Snapshot{
		Connected: true,
		Inputs: []Input{
			{ID: 1, Name: "CAM 1", External: true}, {ID: 2, Name: "CAM 2", External: true},
			{ID: 3, Name: "CAM 3", External: true}, {ID: 3010, Name: "MP1"},
		},
		Program: []uint16{3}, Preview: []uint16{1}, ProgramSource: 3,
	}
	users := []config.User{
		{ID: 1, Name: "Luca", Enabled: true, TallyInput: 1},
		{ID: 2, Name: "Anna", Enabled: true, TallyInput: 2},
		{ID: 3, Name: "Mario", Enabled: true, TallyInput: 3},
		{ID: 4, Name: "Regista", Enabled: true},
	}
	with := func(fn func(s *Snapshot)) Snapshot {
		s := base
		fn(&s)
		return s
	}
	cases := []struct {
		name    string
		snap    Snapshot
		enabled bool
		users   []config.User
		user    uint16
		state   string
		text    string
	}{
		{"disabled", base, false, users, 3, proto.TallyOff, ""},
		{"unassigned", base, true, users, 4, proto.TallyOff, ""},
		{"unavailable", Snapshot{}, true, users, 3, proto.TallyUnavailable, "TALLY NON DISPONIBILE"},
		{"black", with(func(s *Snapshot) { s.Black = true }), true, users, 3, proto.TallyBlack, "NERO · nessuno in onda"},
		{"program", base, true, users, 3, proto.TallyProgram, "● IN ONDA"},
		{"preview", base, true, users, 1, proto.TallyPreview, "PREVIEW"},
		{"other", base, true, users, 2, proto.TallyOther, "IN ONDA: CAM 3 Mario"},
		{"program beats preview", with(func(s *Snapshot) { s.Preview = []uint16{3} }), true, users, 3, proto.TallyProgram, "● IN ONDA"},
		{"mix on air", with(func(s *Snapshot) { s.Program = []uint16{2, 3} }), true, users, 2, proto.TallyProgram, "● IN ONDA"},
		{"mix other", with(func(s *Snapshot) { s.Program, s.Preview = []uint16{2, 3}, nil }), true, users, 1, proto.TallyOther, "IN ONDA: CAM 2 + CAM 3"},
		{"logo ignored", with(func(s *Snapshot) { s.Program = []uint16{3, 3010} }), true, users, 2, proto.TallyOther, "IN ONDA: CAM 3 Mario"},
		{"internal only", with(func(s *Snapshot) { s.Program, s.ProgramSource = []uint16{3010}, 3010 }), true, users, 2, proto.TallyOther, "IN ONDA: MP1"},
		{"unnamed input", with(func(s *Snapshot) { s.Program, s.ProgramSource = []uint16{4}, 4 }), true, users, 2, proto.TallyOther, "IN ONDA: 4"},
		{"two users same input", base, true, append(users, config.User{ID: 5, Name: "Piero", Enabled: true, TallyInput: 3}), 2, proto.TallyOther, "IN ONDA: CAM 3 Mario"},
		{"disabled user not named", base, true, []config.User{
			{ID: 2, Name: "Anna", Enabled: true, TallyInput: 2},
			{ID: 3, Name: "Mario", Enabled: false, TallyInput: 3},
		}, 2, proto.TallyOther, "IN ONDA: CAM 3"},
	}
	for _, c := range cases {
		got := ForUser(c.snap, c.enabled, c.users, c.user)
		want := proto.Tally{Type: proto.MsgTally, State: c.state, Text: c.text}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, want)
		}
	}
}
