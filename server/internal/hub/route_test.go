package hub

import (
	"reflect"
	"testing"

	"intercom/internal/proto"
)

func TestRouteDeduplicatesByListenerOrder(t *testing.T) {
	peers := []Peer{
		{UserID: 1, Listen: []uint16{5}},    // the talker itself
		{UserID: 2, Listen: []uint16{6, 5}}, // listens to both talk channels
		{UserID: 3, Listen: []uint16{7}},    // unrelated channel
	}
	got := Route(1, proto.Target{Kind: proto.TargetChannels}, []uint16{5, 6}, peers)
	want := []Delivery{{UserID: 2, ChannelID: 6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRouteAllAndPrivate(t *testing.T) {
	peers := []Peer{{UserID: 1}, {UserID: 2}, {UserID: 3}}
	all := Route(1, proto.Target{Kind: proto.TargetAll}, nil, peers)
	if want := []Delivery{{2, proto.ChanAll}, {3, proto.ChanAll}}; !reflect.DeepEqual(all, want) {
		t.Errorf("all = %v, want %v", all, want)
	}
	priv := Route(1, proto.Target{Kind: proto.TargetUser, UserID: 3}, nil, peers)
	if want := []Delivery{{3, proto.ChanPrivate}}; !reflect.DeepEqual(priv, want) {
		t.Errorf("private = %v, want %v", priv, want)
	}
}
