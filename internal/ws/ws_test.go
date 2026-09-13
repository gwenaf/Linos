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

func dial(t *testing.T, ctx context.Context, url string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func joinAndRead(t *testing.T, ctx context.Context, c *websocket.Conn, data any) (string, map[string]any) {
	t.Helper()
	if err := wsjson.Write(ctx, c, game.NewMessage("join", data)); err != nil {
		t.Fatal(err)
	}
	var m game.Message
	if err := wsjson.Read(ctx, c, &m); err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	json.Unmarshal(m.Data, &d)
	return m.Type, d
}

func TestWebSocketFlow(t *testing.T) {
	srv := httptest.NewServer(Handler(game.NewRoom(t.TempDir())))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctrl := dial(t, ctx, url+"?role=control")
	if typ, d := joinAndRead(t, ctx, ctrl, nil); typ != "welcome" || d["role"] != game.RoleControl {
		t.Fatalf("got %s %v, want welcome for control", typ, d)
	}

	first := dial(t, ctx, url+"?role=player")
	typ, d := joinAndRead(t, ctx, first, nil)
	if typ != "welcome" || d["role"] != game.RolePlayer {
		t.Fatalf("got %s %v, want welcome for player", typ, d)
	}

	// Reusing the token on another connection makes the server close the first one.
	second := dial(t, ctx, url)
	joinAndRead(t, ctx, second, map[string]any{"token": d["token"]})
	for {
		var m game.Message
		err := wsjson.Read(ctx, first, &m)
		if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			break
		}
		if err != nil {
			t.Fatalf("first connection: %v, want normal closure", err)
		}
	}
}

func TestHandlerRejections(t *testing.T) {
	cases := []struct {
		name, url, remote string
		want              int
	}{
		{"remote control", "/ws?role=control", "192.168.1.20:5000", http.StatusForbidden},
		{"unknown role", "/ws?role=admin", "127.0.0.1:5000", http.StatusBadRequest},
		{"not a websocket", "/ws", "127.0.0.1:5000", http.StatusUpgradeRequired},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", tc.url, nil)
		req.RemoteAddr = tc.remote
		rec := httptest.NewRecorder()
		Handler(game.NewRoom(t.TempDir())).ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}

func TestIsLocal(t *testing.T) {
	cases := []struct {
		remote, host string
		want         bool
	}{
		{"127.0.0.1:5000", "localhost:7777", true},
		{"[::1]:5000", "[::1]:7777", true},
		{"127.0.0.1:5000", "localhost", true},
		{"192.168.1.20:5000", "192.168.1.10:7777", false},
		{"127.0.0.1:5000", "evil.example:7777", false},
		{"not an address", "localhost", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/ws", nil)
		req.RemoteAddr, req.Host = tc.remote, tc.host
		if got := IsLocal(req); got != tc.want {
			t.Errorf("IsLocal(%s, %s) = %v, want %v", tc.remote, tc.host, got, tc.want)
		}
	}
}
