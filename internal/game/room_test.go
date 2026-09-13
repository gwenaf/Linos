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

// join connects a client and drains its messages into a large inbox so the room never drops it as slow.
func join(r *Room, role string, data any) *testClient {
	c := r.Connect(role)
	tc := &testClient{c, make(chan Message, 1024)}
	go func() {
		for m := range c.Messages() {
			tc.inbox <- m
		}
		close(tc.inbox)
	}()
	r.Receive(c, NewMessage("join", data))
	return tc
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

func newPlayer(t *testing.T, r *Room, name string) *testClient {
	c := join(r, RolePlayer, nil)
	expect(t, c, "welcome", nil)
	send(r, c, "identify", map[string]string{"name": name})
	expect(t, c, "lobby-update", nil)
	return c
}

func startGame(t *testing.T, r *Room) (ctrl, host *testClient) {
	ctrl = join(r, RoleControl, nil)
	expect(t, ctrl, "welcome", nil)
	host = join(r, RoleHost, nil)
	expect(t, host, "welcome", nil)
	send(r, ctrl, "start-game", nil)
	expect(t, ctrl, "game-start", nil)
	return ctrl, host
}

type answerResult struct {
	Name    string `json:"name"`
	Correct bool   `json:"correct"`
	Points  int    `json:"points"`
}

type scoreUpdate struct {
	Name  string `json:"name"`
	Team  string `json:"team"`
	Score int    `json:"score"`
}

func TestBuzzPicksOneWinner(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	startGame(t, r)

	send(r, alice, "buzz", nil)
	send(r, bob, "buzz", nil)

	var winner string
	for _, c := range []*testClient{alice, bob} {
		var d struct {
			Name string `json:"name"`
		}
		expect(t, c, "buzz-accepted", &d)
		if winner == "" {
			winner = d.Name
		} else if d.Name != winner {
			t.Fatalf("clients disagree on winner: %q vs %q", winner, d.Name)
		}
	}
	loser := alice
	switch winner {
	case "alice":
		loser = bob
	case "bob":
	default:
		t.Fatalf("unexpected winner %q", winner)
	}
	expect(t, loser, "buzz-blocked", nil)
}

func TestReconnectWithToken(t *testing.T) {
	r := NewRoom()
	type welcome struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}

	c := join(r, RolePlayer, nil)
	var first welcome
	expect(t, c, "welcome", &first)
	send(r, c, "identify", map[string]string{"name": "alice"})
	expect(t, c, "lobby-update", nil)
	r.Disconnect(c.Client)

	c2 := join(r, RolePlayer, map[string]string{"token": first.Token})
	var again welcome
	expect(t, c2, "welcome", &again)
	if again.Token != first.Token || again.Name != "alice" {
		t.Fatalf("reconnect = %+v, want token kept and name alice", again)
	}

	c3 := join(r, RolePlayer, map[string]string{"token": "forged"})
	var forged welcome
	expect(t, c3, "welcome", &forged)
	if forged.Token == "forged" || forged.Token == first.Token || forged.Name != "" {
		t.Fatalf("unknown token must create a new player, got %+v", forged)
	}
}

func TestValidate(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r)

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)

	send(r, alice, "validate", map[string]bool{"correct": true})
	expectError(t, alice, "forbidden")

	send(r, ctrl, "validate", map[string]bool{"correct": false})
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	if res != (answerResult{"alice", false, 0}) {
		t.Fatalf("result = %+v, want alice wrong", res)
	}
	expect(t, ctrl, "buzz-available", nil)

	// alice already tried: only bob can take the hand.
	send(r, alice, "buzz", nil)
	send(r, bob, "buzz", nil)
	var acc struct {
		Name string `json:"name"`
	}
	expect(t, ctrl, "buzz-accepted", &acc)
	if acc.Name != "bob" {
		t.Fatalf("second buzz winner = %q, want bob", acc.Name)
	}

	send(r, ctrl, "validate", map[string]bool{"correct": true})
	expect(t, ctrl, "answer-result", &res)
	if res.Name != "bob" || !res.Correct || res.Points < 95 || res.Points > 100 {
		t.Fatalf("result = %+v, want bob correct with ~100 points", res)
	}
	var score scoreUpdate
	expect(t, ctrl, "score-update", &score)
	if score != (scoreUpdate{"bob", "", res.Points}) {
		t.Fatalf("score = %+v, want bob %d", score, res.Points)
	}

	send(r, ctrl, "score-adjust", map[string]any{"name": "bob", "delta": -10})
	expect(t, ctrl, "score-update", &score)
	if score != (scoreUpdate{"bob", "", res.Points - 10}) {
		t.Fatalf("adjusted score = %+v, want bob %d", score, res.Points-10)
	}
}

func TestTeams(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	carol := newPlayer(t, r, "carol")
	send(r, alice, "join-team", map[string]string{"team": "rouge"})
	send(r, bob, "join-team", map[string]string{"team": "ROUGE"})
	send(r, carol, "join-team", map[string]string{"team": "bleu"})
	var lobby struct {
		Teams []string `json:"teams"`
	}
	for range 3 {
		expect(t, carol, "lobby-update", &lobby)
	}
	if len(lobby.Teams) != 2 {
		t.Fatalf("teams = %v, want rouge and bleu merged case-insensitively", lobby.Teams)
	}
	ctrl, _ := startGame(t, r)

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", map[string]bool{"correct": false})
	expect(t, ctrl, "buzz-available", nil)

	// alice's team already tried: bob is excluded with her, carol takes the hand.
	send(r, bob, "buzz", nil)
	send(r, carol, "buzz", nil)
	var acc struct {
		Name string `json:"name"`
		Team string `json:"team"`
	}
	expect(t, ctrl, "buzz-accepted", &acc)
	if acc.Name != "carol" || acc.Team != "bleu" {
		t.Fatalf("winner = %+v, want carol of bleu", acc)
	}

	send(r, ctrl, "validate", map[string]bool{"correct": true})
	var score scoreUpdate
	expect(t, ctrl, "score-update", &score)
	if score.Team != "bleu" || score.Name != "" || score.Score < 95 {
		t.Fatalf("score = %+v, want team bleu credited", score)
	}
}

func TestGameStates(t *testing.T) {
	r := NewRoom()
	ctrl := join(r, RoleControl, nil)
	expect(t, ctrl, "welcome", nil)
	alice := newPlayer(t, r, "alice")

	send(r, alice, "buzz", nil)
	expectError(t, alice, "wrong-state")
	send(r, ctrl, "start-game", nil)
	expectError(t, ctrl, "no-host")

	host := join(r, RoleHost, nil)
	expect(t, host, "welcome", nil)
	send(r, ctrl, "start-game", nil)
	expect(t, ctrl, "game-start", nil)
	send(r, alice, "join-team", map[string]string{"team": "late"})
	expectError(t, alice, "wrong-state")

	var paused struct {
		Reason string `json:"reason"`
	}
	send(r, ctrl, "pause", nil)
	expect(t, ctrl, "game-paused", &paused)
	if paused.Reason != "control" {
		t.Fatalf("pause reason = %q, want control", paused.Reason)
	}
	send(r, alice, "buzz", nil)
	expectError(t, alice, "wrong-state")
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)

	r.Disconnect(host.Client)
	expect(t, ctrl, "game-paused", &paused)
	if paused.Reason != "technical" {
		t.Fatalf("pause reason = %q, want technical", paused.Reason)
	}
	host = join(r, RoleHost, nil)
	expect(t, ctrl, "game-resumed", nil)

	send(r, ctrl, "abort", nil)
	var end struct {
		Reason  string   `json:"reason"`
		Results []result `json:"results"`
	}
	expect(t, ctrl, "game-end", &end)
	if end.Reason != "aborted" || len(end.Results) != 1 || end.Results[0].Name != "alice" {
		t.Fatalf("game-end = %+v, want aborted with alice", end)
	}
	send(r, ctrl, "start-game", nil)
	expectError(t, ctrl, "wrong-state")
}

func TestSpeedPoints(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    int
	}{
		{0, 100},
		{15 * time.Second, 60},
		{30 * time.Second, 20},
		{45 * time.Second, 20},
		{-time.Second, 100},
	}
	for _, tc := range cases {
		if got := speedPoints(100, 20, 30*time.Second, tc.elapsed); got != tc.want {
			t.Errorf("speedPoints(%v) = %d, want %d", tc.elapsed, got, tc.want)
		}
	}
}
