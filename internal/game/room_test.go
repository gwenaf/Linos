package game

import (
	"encoding/json"
	"testing"
	"time"
)

type testClient struct {
	*Client
	inbox chan Message
}

// connect drains the client's messages into a large inbox so the room never drops it as slow.
func connect(r *Room, role string) *testClient {
	c := r.Connect(role)
	tc := &testClient{c, make(chan Message, 1024)}
	go func() {
		for m := range c.Messages() {
			tc.inbox <- m
		}
		close(tc.inbox)
	}()
	return tc
}

func join(r *Room, role string, data any) *testClient {
	c := connect(r, role)
	send(r, c, "join", data)
	return c
}

func send(r *Room, c *testClient, typ string, data any) {
	r.Receive(c.Client, NewMessage(typ, data))
}

func expect(t *testing.T, c *testClient, typ string, v any) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case m, ok := <-c.inbox:
			if !ok {
				t.Fatalf("client closed while waiting for %s", typ)
			}
			if m.Type == typ {
				if v != nil {
					json.Unmarshal(m.Data, v)
				}
				return
			}
		case <-timeout:
			t.Fatalf("timeout waiting for %s", typ)
		}
	}
}

func expectError(t *testing.T, c *testClient, code string) {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	expect(t, c, "error", &e)
	if e.Code != code {
		t.Fatalf("error = %q, want %q", e.Code, code)
	}
}

func expectClosed(t *testing.T, c *testClient) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-c.inbox:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("client was not dropped")
		}
	}
}

// expectState reads lobby updates until the room reports the wanted state.
func expectState(t *testing.T, c *testClient, state string) {
	t.Helper()
	for {
		var lobby struct {
			State string `json:"state"`
		}
		expect(t, c, "lobby-update", &lobby)
		if lobby.State == state {
			return
		}
	}
}

// sync waits until every message sent before it has been processed.
func sync(t *testing.T, r *Room, c *testClient) {
	t.Helper()
	send(r, c, "sync", nil)
	expectError(t, c, "unknown-type")
}

func newPlayer(t *testing.T, r *Room, name string) *testClient {
	t.Helper()
	c := join(r, RolePlayer, nil)
	expect(t, c, "welcome", nil)
	send(r, c, "identify", map[string]string{"name": name})
	expect(t, c, "lobby-update", nil)
	return c
}

func newControl(t *testing.T, r *Room) *testClient {
	t.Helper()
	c := join(r, RoleControl, nil)
	expect(t, c, "welcome", nil)
	return c
}

// startGame connects control and host, marks the given players ready and starts the game.
func startGame(t *testing.T, r *Room, players ...*testClient) (ctrl, host *testClient) {
	t.Helper()
	ctrl = newControl(t, r)
	host = join(r, RoleHost, nil)
	expect(t, host, "welcome", nil)
	for _, p := range players {
		send(r, p, "ready", map[string]bool{"ready": true})
	}
	expectState(t, ctrl, stateReady)
	send(r, ctrl, "start-game", nil)
	expect(t, ctrl, "game-start", nil)
	return ctrl, host
}

func TestDispatchErrors(t *testing.T) {
	r := NewRoom()
	c := connect(r, RolePlayer)

	send(r, c, "buzz", nil)
	expectError(t, c, "not-joined")
	send(r, c, "nope", nil)
	expectError(t, c, "unknown-type")
	send(r, c, "join", "not an object")
	expectError(t, c, "bad-data")

	send(r, c, "join", nil)
	expect(t, c, "welcome", nil)
	send(r, c, "validate", map[string]bool{"correct": true})
	expectError(t, c, "forbidden")
	send(r, c, "buzz", nil)
	expectError(t, c, "wrong-state")
}

func TestSlowClientDropped(t *testing.T) {
	r := NewRoom()
	slow := r.Connect(RolePlayer)
	r.Receive(slow, NewMessage("join", map[string]string{}))
	alice := newPlayer(t, r, "alice")

	// Fill the slow client's buffer, then any broadcast drops it.
	for len(slow.send) < cap(slow.send) {
		slow.send <- Message{Type: "filler"}
	}
	send(r, alice, "identify", map[string]string{"name": "alice2"})
	sync(t, r, alice)

	// Messages still delivered for a dropped client are ignored.
	r.Receive(slow, NewMessage("identify", map[string]string{"name": "ghost"}))
	sync(t, r, alice)
	for range slow.Messages() {
	}
}

func TestDisconnectTwice(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	r.Disconnect(alice.Client)
	r.Disconnect(alice.Client)
	expectClosed(t, alice)

	bob := newPlayer(t, r, "bob")
	sync(t, r, bob)
}

func TestReconnectReplacesDevice(t *testing.T) {
	r := NewRoom()
	var w struct {
		Token string `json:"token"`
	}
	first := join(r, RolePlayer, nil)
	expect(t, first, "welcome", &w)

	second := join(r, RolePlayer, map[string]string{"token": w.Token})
	expect(t, second, "welcome", nil)
	expectClosed(t, first)

	// A client switching to another identity leaves its previous player disconnected.
	send(r, second, "join", nil)
	var other struct {
		Token string `json:"token"`
	}
	expect(t, second, "welcome", &other)
	if other.Token == w.Token {
		t.Fatal("join without token must create a new player")
	}
}

func TestGamemasterPhone(t *testing.T) {
	r := NewRoom()
	ctrl := newControl(t, r)
	send(r, ctrl, "master-invite", nil)
	var inv struct {
		Code      string  `json:"code"`
		ExpiresIn float64 `json:"expiresIn"`
	}
	expect(t, ctrl, "master-invite", &inv)
	if inv.Code == "" || inv.ExpiresIn != 120 {
		t.Fatalf("invite = %+v, want a code valid 120 s", inv)
	}

	// The phone first opened /play, then scans the invite.
	phone := join(r, RolePlayer, nil)
	expect(t, phone, "welcome", nil)
	send(r, phone, "join", map[string]string{"invite": inv.Code})
	var w struct {
		Role  string `json:"role"`
		Token string `json:"token"`
	}
	expect(t, phone, "welcome", &w)
	if w.Role != RoleControl || w.Token == "" {
		t.Fatalf("welcome = %+v, want control with a token", w)
	}
	send(r, phone, "master-invite", nil)
	expect(t, phone, "master-invite", nil)

	reuse := join(r, RolePlayer, map[string]string{"invite": inv.Code})
	expectError(t, reuse, "invalid-invite")

	back := join(r, RolePlayer, map[string]string{"token": w.Token})
	expect(t, back, "welcome", &w)
	if w.Role != RoleControl {
		t.Fatalf("reconnect role = %q, want control", w.Role)
	}

	expired := NewRoom()
	expired.inviteTTL = -time.Second
	ctrl2 := newControl(t, expired)
	send(expired, ctrl2, "master-invite", nil)
	expect(t, ctrl2, "master-invite", &inv)
	late := join(expired, RolePlayer, map[string]string{"invite": inv.Code})
	expectError(t, late, "invalid-invite")
}
