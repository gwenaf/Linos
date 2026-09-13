package ws

import (
	"net"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gwenaf/linos/internal/game"
)

func Handler(room *game.Room) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		role := game.RolePlayer
		switch q := req.URL.Query().Get("role"); q {
		case "", game.RolePlayer:
		case game.RoleControl, game.RoleHost:
			if !isLocal(req) {
				http.Error(w, "role reserved to the host machine", http.StatusForbidden)
				return
			}
			role = q
		default:
			http.Error(w, "unknown role", http.StatusBadRequest)
			return
		}

		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		ctx := req.Context()
		c := room.Connect(role)

		go func() {
			for m := range c.Messages() {
				if err := wsjson.Write(ctx, conn, m); err != nil {
					conn.CloseNow()
					return
				}
			}
			conn.Close(websocket.StatusNormalClosure, "")
		}()

		for {
			var m game.Message
			if err := wsjson.Read(ctx, conn, &m); err != nil {
				break
			}
			room.Receive(c, m)
		}
		room.Disconnect(c)
	})
}

// isLocal also checks the Host header so a DNS-rebinding page opened on the host PC cannot claim control.
func isLocal(req *http.Request) bool {
	remote, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() {
		return false
	}
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}
