package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gwenaf/linos/internal/game"
)

func TestJoinOverWebSocket(t *testing.T) {
	srv := httptest.NewServer(Handler(game.NewRoom()))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"?role=control", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()

	if err := wsjson.Write(ctx, c, game.NewMessage("join", nil)); err != nil {
		t.Fatal(err)
	}
	var m game.Message
	if err := wsjson.Read(ctx, c, &m); err != nil {
		t.Fatal(err)
	}
	var w struct {
		Role string `json:"role"`
	}
	json.Unmarshal(m.Data, &w)
	if m.Type != "welcome" || w.Role != game.RoleControl {
		t.Fatalf("got %s %s, want welcome for control", m.Type, m.Data)
	}
}

func TestRemoteControlRefused(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws?role=control", nil)
	req.RemoteAddr = "192.168.1.20:5000"
	rec := httptest.NewRecorder()
	Handler(game.NewRoom()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestIsLocal(t *testing.T) {
	cases := []struct {
		remote, host string
		want         bool
	}{
		{"127.0.0.1:5000", "localhost:7777", true},
		{"[::1]:5000", "[::1]:7777", true},
		{"192.168.1.20:5000", "192.168.1.10:7777", false},
		{"127.0.0.1:5000", "evil.example:7777", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/ws", nil)
		req.RemoteAddr, req.Host = tc.remote, tc.host
		if got := isLocal(req); got != tc.want {
			t.Errorf("isLocal(%s, %s) = %v, want %v", tc.remote, tc.host, got, tc.want)
		}
	}
}
