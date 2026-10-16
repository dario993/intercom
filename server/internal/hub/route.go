package hub

import (
	"slices"

	"intercom/internal/proto"
)

// Peer is a connected client able to receive audio.
type Peer struct {
	UserID uint16
	Listen []uint16 // in the listener's channel order
}

// Delivery means: send the packet to UserID, labelled with ChannelID.
type Delivery struct {
	UserID    uint16
	ChannelID uint16
}

// Route returns who receives a talker's packet. Each listener appears at most once;
// for channel talk the label is the first of the listener's channels the talker is on.
func Route(talker uint16, t proto.Target, talk []uint16, peers []Peer) []Delivery {
	var out []Delivery
	for _, p := range peers {
		if p.UserID == talker {
			continue
		}
		switch t.Kind {
		case proto.TargetAll:
			out = append(out, Delivery{p.UserID, proto.ChanAll})
		case proto.TargetUser:
			if p.UserID == t.UserID {
				out = append(out, Delivery{p.UserID, proto.ChanPrivate})
			}
		case proto.TargetChannels:
			for _, ch := range p.Listen {
				if slices.Contains(talk, ch) {
					out = append(out, Delivery{p.UserID, ch})
					break
				}
			}
		}
	}
	return out
}
