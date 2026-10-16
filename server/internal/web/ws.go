package web

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"intercom/internal/proto"
)

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// Not r.Context(): the connection is hijacked (see coder/websocket docs).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	helloCtx, helloCancel := context.WithTimeout(ctx, 10*time.Second)
	var hello proto.ClientMsg
	err = wsjson.Read(helloCtx, conn, &hello)
	helloCancel()
	if err != nil || hello.Type != proto.MsgHello {
		conn.Close(websocket.StatusPolicyViolation, "hello richiesto")
		return
	}
	sess, lerr := s.Hub.Login(hello.Username, hello.Password)
	if lerr != nil {
		wsjson.Write(ctx, conn, lerr)
		conn.Close(websocket.StatusNormalClosure, lerr.Code)
		return
	}
	defer s.Hub.Logout(sess)

	go func() {
		defer s.Hub.Logout(sess) // closes sess.Out, which ends the writer loop
		for {
			var m proto.ClientMsg
			if err := wsjson.Read(ctx, conn, &m); err != nil {
				return
			}
			switch m.Type {
			case proto.MsgState:
				s.Hub.SetState(sess, m.Talk, m.Listen)
			case proto.MsgPTT:
				s.Hub.SetPTT(sess, m.Active, m.Target)
			case proto.MsgPing:
				s.Hub.Ping(sess)
			}
		}
	}()

	for msg := range sess.Out {
		if err := wsjson.Write(ctx, conn, msg); err != nil {
			return
		}
	}
	conn.Close(websocket.StatusNormalClosure, "")
}
