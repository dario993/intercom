// Command testclient logs into the server, optionally talks on a channel with dummy
// packets (not real Opus: the server only routes), and counts the packets it receives.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"intercom/internal/proto"
)

func main() {
	server := flag.String("server", "127.0.0.1:8080", "host:porta HTTP del server")
	user := flag.String("user", "", "nome utente")
	pass := flag.String("pass", "", "password")
	channel := flag.Uint("channel", 0, "canale su cui parlare e ascoltare")
	talk := flag.Duration("talk", 0, "per quanto trasmettere (0 = solo ascolto)")
	listen := flag.Duration("listen", 5*time.Second, "per quanto ascoltare dopo aver parlato")
	flag.Parse()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws://"+*server+"/ws", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.CloseNow()

	wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgHello, Username: *user, Password: *pass, Device: "testclient"})
	var raw json.RawMessage
	if err := wsjson.Read(ctx, conn, &raw); err != nil {
		log.Fatal(err)
	}
	var welcome proto.Welcome
	json.Unmarshal(raw, &welcome)
	if welcome.Type != proto.MsgWelcome {
		log.Fatalf("login fallito: %s", raw)
	}
	log.Printf("collegato come utente %d, sessione %d", welcome.UserID, welcome.Session)

	host, _, _ := net.SplitHostPort(*server)
	uaddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(welcome.UDPPort)))
	if err != nil {
		log.Fatal(err)
	}
	udp, err := net.DialUDP("udp", nil, uaddr)
	if err != nil {
		log.Fatal(err)
	}
	ch := uint16(*channel)
	wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgState, Talk: []uint16{ch}, Listen: []uint16{ch}})
	udp.Write(proto.ClientPacket{Kind: proto.KindKeepalive, Session: welcome.Session}.Marshal())

	go func() {
		for {
			var m json.RawMessage
			if wsjson.Read(ctx, conn, &m) != nil {
				return
			}
			log.Printf("controllo: %s", m)
		}
	}()

	var mu sync.Mutex
	counts := map[uint16]int{}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, err := udp.Read(buf)
			if err != nil {
				return
			}
			if p, err := proto.ParseServer(buf[:n]); err == nil {
				mu.Lock()
				counts[p.TalkerID]++
				mu.Unlock()
			}
		}
	}()

	if *talk > 0 {
		wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgPTT, Active: true, Target: proto.Target{Kind: proto.TargetChannels}})
		payload := make([]byte, 80)
		frames := int(*talk / (20 * time.Millisecond))
		for seq := 0; seq < frames; seq++ {
			udp.Write(proto.ClientPacket{Kind: proto.KindVoice, Session: welcome.Session, Seq: uint16(seq), Payload: payload}.Marshal())
			time.Sleep(20 * time.Millisecond)
		}
		wsjson.Write(ctx, conn, proto.ClientMsg{Type: proto.MsgPTT, Active: false, Target: proto.Target{Kind: proto.TargetChannels}})
		log.Printf("inviati %d pacchetti", frames)
	}

	time.Sleep(*listen)
	mu.Lock()
	fmt.Println("pacchetti ricevuti per utente:", counts)
	mu.Unlock()
}
