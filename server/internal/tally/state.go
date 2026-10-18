// Package tally reads on-air state from a Blackmagic ATEM switcher and turns it into
// the banner each intercom user sees.
package tally

import (
	"slices"
	"strconv"
	"strings"

	"intercom/internal/config"
	"intercom/internal/proto"
)

type Input struct {
	ID       uint16 `json:"id"`
	Name     string `json:"name"`
	External bool   `json:"external"` // a physical input (camera), not black/bars/media player/key
}

// Snapshot is what the switcher last told us. The zero value means "not connected".
type Snapshot struct {
	Connected     bool     `json:"connected"`
	Model         string   `json:"model"`
	Inputs        []Input  `json:"inputs"`
	Program       []uint16 `json:"program"` // sources with program tally, ascending
	Preview       []uint16 `json:"preview"` // sources with preview tally, ascending
	ProgramSource uint16   `json:"programSource"`
	Black         bool     `json:"black"` // fade to black complete, not transitioning
}

func (s Snapshot) input(id uint16) (Input, bool) {
	i := slices.IndexFunc(s.Inputs, func(in Input) bool { return in.ID == id })
	if i < 0 {
		return Input{}, false
	}
	return s.Inputs[i], true
}

func (s Snapshot) name(id uint16) string {
	if in, ok := s.input(id); ok && in.Name != "" {
		return in.Name
	}
	return strconv.Itoa(int(id))
}

// ForUser resolves the banner for userID.
func ForUser(s Snapshot, enabled bool, users []config.User, userID uint16) proto.Tally {
	t := func(state, text string) proto.Tally {
		return proto.Tally{Type: proto.MsgTally, State: state, Text: text}
	}
	var mine uint16
	if i := slices.IndexFunc(users, func(u config.User) bool { return u.ID == userID }); i >= 0 {
		mine = users[i].TallyInput
	}
	switch {
	case !enabled || mine == 0:
		return t(proto.TallyOff, "")
	case !s.Connected:
		return t(proto.TallyUnavailable, "TALLY NON DISPONIBILE")
	case s.Black:
		return t(proto.TallyBlack, "NERO · nessuno in onda")
	case slices.Contains(s.Program, mine):
		return t(proto.TallyProgram, "● IN ONDA")
	case slices.Contains(s.Preview, mine):
		return t(proto.TallyPreview, "PREVIEW")
	}

	var onAir []uint16
	for _, id := range s.Program {
		if in, ok := s.input(id); ok && in.External {
			onAir = append(onAir, id)
		}
	}
	if len(onAir) == 0 && s.ProgramSource != 0 {
		onAir = []uint16{s.ProgramSource}
	}
	switch len(onAir) {
	case 0:
		return t(proto.TallyOther, "IN ONDA: —")
	case 1:
		text := "IN ONDA: " + s.name(onAir[0])
		if i := slices.IndexFunc(users, func(u config.User) bool { return u.Enabled && u.TallyInput == onAir[0] }); i >= 0 {
			text += " " + users[i].Name
		}
		return t(proto.TallyOther, text)
	}
	names := make([]string, len(onAir))
	for i, id := range onAir {
		names[i] = s.name(id)
	}
	return t(proto.TallyOther, "IN ONDA: "+strings.Join(names, " + "))
}
