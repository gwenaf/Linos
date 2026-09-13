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

func TestBuzzPicksOneWinner(t *testing.T) {
	url, ctx := setup(t)

	names := []string{"alice", "bob"}
	conns := make([]*websocket.Conn, len(names))
	for i, name := range names {
		c := dial(t, ctx, url)
		write(t, ctx, c, "join", nil)
		readUntil(t, ctx, c, "welcome", nil)
		write(t, ctx, c, "identify", map[string]string{"name": name})
		readUntil(t, ctx, c, "lobby-update", nil)
		conns[i] = c
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
