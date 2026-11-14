package ws

import (
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gwenaf/linos/internal/game"
)

// Handler connects websockets to room. isHost grants the control and host roles: see IsLocal.
func Handler(room *game.Room, isHost func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		role := game.RolePlayer
		switch q := req.URL.Query().Get("role"); q {
		case "", game.RolePlayer:
		case game.RoleControl, game.RoleHost:
			if !isHost(req) {
				slog.Warn("websocket role refused", "role", q, "remote", req.RemoteAddr, "host", req.Host)
				http.Error(w, "role reserved to the host machine; use a gamemaster invite", http.StatusForbidden)
				return
			}
			role = q
		default:
			http.Error(w, "unknown role", http.StatusBadRequest)
			return
		}

		conn, err := websocket.Accept(w, req, nil)
		if err != nil {
			slog.Warn("websocket upgrade failed", "remote", req.RemoteAddr, "error", err)
			return
		}
		defer conn.CloseNow()
		slog.Info("websocket connected", "role", role, "remote", req.RemoteAddr, "userAgent", req.UserAgent())

		ctx := req.Context()
		c := room.Connect(role)

		go func() {
			// Write errors surface as a read error below, which disconnects the client.
			for m := range c.Messages() {
				wsjson.Write(ctx, conn, m)
			}
			conn.Close(websocket.StatusNormalClosure, "")
		}()

		limit := bucket{tokens: burst, last: time.Now()}
		flooding := false
		for {
			var m game.Message
			if err := wsjson.Read(ctx, conn, &m); err != nil {
				slog.Info("websocket disconnected", "role", role, "remote", req.RemoteAddr, "reason", err)
				break
			}
			// A flood is dropped, not the client: what a person sends always goes through, a script
			// cannot slow the game down or fill the journal.
			if !limit.take(time.Now()) {
				if !flooding {
					slog.Warn("websocket flood, messages dropped", "role", role, "remote", req.RemoteAddr)
				}
				flooding = true
				continue
			}
			flooding = false
			room.Receive(c, m)
		}
		room.Disconnect(c)
	})
}

// A connection may send burst messages at once, then rate per second: far more than a person tapping.
const (
	burst = 40
	rate  = 20
)

// bucket is a token bucket: each message takes a token, tokens come back at rate per second.
type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) take(now time.Time) bool {
	b.tokens = min(burst, b.tokens+now.Sub(b.last).Seconds()*rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// IsLocal reports a request from the host machine. It also checks the Host header so a DNS-rebinding page opened on the host PC cannot claim control.
func IsLocal(req *http.Request) bool {
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
