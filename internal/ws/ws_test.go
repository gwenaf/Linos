package ws

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func setup(t *testing.T) (string, context.Context) {
	srv := httptest.NewServer(NewRoom())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), ctx
}

func dial(t *testing.T, ctx context.Context, url string) *websocket.Conn {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func write(t *testing.T, ctx context.Context, c *websocket.Conn, typ string, data any) {
	if err := wsjson.Write(ctx, c, newMessage(typ, data)); err != nil {
		t.Fatal(err)
	}
}

func readUntil(t *testing.T, ctx context.Context, c *websocket.Conn, typ string, v any) {
	for {
		var m Message
		if err := wsjson.Read(ctx, c, &m); err != nil {
			t.Fatalf("waiting for %s: %v", typ, err)
		}
		if m.Type == typ {
			if v != nil {
				json.Unmarshal(m.Data, v)
			}
			return
		}
	}
}

type welcome struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

func joinPlayer(t *testing.T, ctx context.Context, url, name string) *websocket.Conn {
	c := dial(t, ctx, url)
	write(t, ctx, c, "join", nil)
	readUntil(t, ctx, c, "welcome", nil)
	write(t, ctx, c, "identify", map[string]string{"name": name})
	readUntil(t, ctx, c, "lobby-update", nil)
	return c
}

type answerResult struct {
	Name    string `json:"name"`
	Correct bool   `json:"correct"`
}

func TestValidate(t *testing.T) {
	url, ctx := setup(t)

	ctrl := dial(t, ctx, url+"?role=control")
	write(t, ctx, ctrl, "join", nil)
	var w struct {
		Role string `json:"role"`
	}
	readUntil(t, ctx, ctrl, "welcome", &w)
	if w.Role != "control" {
		t.Fatalf("role = %q, want control", w.Role)
	}
	alice := joinPlayer(t, ctx, url, "alice")
	bob := joinPlayer(t, ctx, url, "bob")

	write(t, ctx, alice, "buzz", nil)
	readUntil(t, ctx, ctrl, "buzz-accepted", nil)

	write(t, ctx, alice, "validate", map[string]bool{"correct": true})
	var e struct {
		Code string `json:"code"`
	}
	readUntil(t, ctx, alice, "error", &e)
	if e.Code != "forbidden" {
		t.Fatalf("player validate error = %q, want forbidden", e.Code)
	}

	write(t, ctx, ctrl, "validate", map[string]bool{"correct": false})
	var res answerResult
	readUntil(t, ctx, ctrl, "answer-result", &res)
	if res != (answerResult{"alice", false}) {
		t.Fatalf("result = %+v, want alice wrong", res)
	}
	readUntil(t, ctx, ctrl, "buzz-available", nil)

	// alice already tried: only bob can take the hand.
	write(t, ctx, alice, "buzz", nil)
	write(t, ctx, bob, "buzz", nil)
	var acc struct {
		Name string `json:"name"`
	}
	readUntil(t, ctx, ctrl, "buzz-accepted", &acc)
	if acc.Name != "bob" {
		t.Fatalf("second buzz winner = %q, want bob", acc.Name)
	}

	write(t, ctx, ctrl, "validate", map[string]bool{"correct": true})
	readUntil(t, ctx, ctrl, "answer-result", &res)
	if res != (answerResult{"bob", true}) {
		t.Fatalf("result = %+v, want bob correct", res)
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

func TestBuzzPicksOneWinner(t *testing.T) {
	url, ctx := setup(t)

	names := []string{"alice", "bob"}
	conns := make([]*websocket.Conn, len(names))
	for i, name := range names {
		conns[i] = joinPlayer(t, ctx, url, name)
	}

	for _, c := range conns {
		write(t, ctx, c, "buzz", nil)
	}

	var winner string
	for _, c := range conns {
		var d struct {
			Name string `json:"name"`
		}
		readUntil(t, ctx, c, "buzz-accepted", &d)
		if winner == "" {
			winner = d.Name
		} else if d.Name != winner {
			t.Fatalf("clients disagree on winner: %q vs %q", winner, d.Name)
		}
	}

	loser := conns[0]
	switch winner {
	case "alice":
		loser = conns[1]
	case "bob":
	default:
		t.Fatalf("unexpected winner %q", winner)
	}
	readUntil(t, ctx, loser, "buzz-blocked", nil)
}

func TestReconnectWithToken(t *testing.T) {
	url, ctx := setup(t)

	c := dial(t, ctx, url)
	write(t, ctx, c, "join", nil)
	var first welcome
	readUntil(t, ctx, c, "welcome", &first)
	write(t, ctx, c, "identify", map[string]string{"name": "alice"})
	readUntil(t, ctx, c, "lobby-update", nil)
	c.Close(websocket.StatusNormalClosure, "")

	c2 := dial(t, ctx, url)
	write(t, ctx, c2, "join", map[string]string{"token": first.Token})
	var again welcome
	readUntil(t, ctx, c2, "welcome", &again)
	if again.Token != first.Token || again.Name != "alice" {
		t.Fatalf("reconnect = %+v, want token kept and name alice", again)
	}

	c3 := dial(t, ctx, url)
	write(t, ctx, c3, "join", map[string]string{"token": "forged"})
	var forged welcome
	readUntil(t, ctx, c3, "welcome", &forged)
	if forged.Token == "forged" || forged.Token == first.Token || forged.Name != "" {
		t.Fatalf("unknown token must create a new player, got %+v", forged)
	}
}
