package hub

import (
	"testing"

	"intercom/internal/config"
	"intercom/internal/proto"
	"intercom/internal/tally"
)

// tallies returns the tally messages among msgs.
func tallies(msgs []any) []proto.Tally {
	var out []proto.Tally
	for _, m := range msgs {
		if t, ok := m.(proto.Tally); ok {
			out = append(out, t)
		}
	}
	return out
}

func onAir(ids ...uint16) tally.Snapshot {
	return tally.Snapshot{Connected: true, Program: ids, ProgramSource: ids[0]}
}

func TestLoginSendsCurrentTally(t *testing.T) {
	h, _ := newHub(t)
	h.SetTally(onAir(1))
	bruno := connect(t, h, "bruno", 1011)
	msgs := drain(bruno)
	if _, ok := msgs[0].(proto.Welcome); !ok {
		t.Fatalf("first message = %#v", msgs[0])
	}
	want := proto.Tally{Type: proto.MsgTally, State: proto.TallyOther, Text: "IN ONDA: 1 anna"}
	if got := msgs[1]; got != want {
		t.Errorf("second message = %#v, want %#v", got, want)
	}
	if h.Tally().Program[0] != 1 {
		t.Errorf("Tally() = %+v", h.Tally())
	}
}

func TestTallyOnlyOnChange(t *testing.T) {
	h, _ := newHub(t)
	anna := connect(t, h, "anna", 1010)
	drain(anna)
	h.SetTally(onAir(2))
	h.SetTally(onAir(2))
	if got := tallies(drain(anna)); len(got) != 1 || got[0].Text != "IN ONDA: 2 bruno" {
		t.Errorf("tallies = %+v", got)
	}
	s := onAir(2)
	s.Preview = []uint16{1}
	h.SetTally(s)
	if got := tallies(drain(anna)); len(got) != 1 || got[0].State != proto.TallyPreview {
		t.Errorf("tallies = %+v", got)
	}
}

func TestTallyOffWhenUnassignedOrDisabled(t *testing.T) {
	h, _ := newHub(t)
	h.SetTally(onAir(1))
	anna := connect(t, h, "anna", 1010)
	bruno := connect(t, h, "bruno", 1011)
	carla := connect(t, h, "carla", 1012)
	if got := tallies(drain(carla)); len(got) != 1 || got[0].State != proto.TallyOff {
		t.Errorf("carla at login = %+v", got)
	}
	drain(anna)
	drain(bruno)

	update := func(fn func(c *config.Config)) {
		t.Helper()
		if err := h.store.Update(func(c *config.Config) error { fn(c); return nil }); err != nil {
			t.Fatal(err)
		}
		h.ConfigChanged()
	}
	update(func(c *config.Config) { c.UserByID(10).TallyInput = 0 })
	if got := tallies(drain(anna)); len(got) != 1 || got[0].State != proto.TallyOff {
		t.Errorf("anna unassigned = %+v", got)
	}
	if got := tallies(drain(bruno)); len(got) != 1 || got[0].Text != "IN ONDA: 1" {
		t.Errorf("bruno after anna unassigned = %+v", got)
	}
	update(func(c *config.Config) { c.Settings.Tally.Enabled = false })
	if got := tallies(drain(bruno)); len(got) != 1 || got[0].State != proto.TallyOff {
		t.Errorf("bruno with tally disabled = %+v", got)
	}
	if got := tallies(drain(carla)); len(got) != 0 {
		t.Errorf("carla got %+v", got)
	}
}
