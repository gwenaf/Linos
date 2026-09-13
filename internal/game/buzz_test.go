package game

import (
	"testing"
	"time"
)

type answerResult struct {
	Name    string `json:"name"`
	Correct bool   `json:"correct"`
	Points  int    `json:"points"`
}

type buzzAccepted struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

func TestBuzzPicksOneWinner(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	dave := newPlayer(t, r, "dave")
	r.Disconnect(dave.Client)
	startGame(t, r, alice, bob)

	send(r, alice, "buzz", nil)
	send(r, bob, "buzz", nil)

	var winner string
	for _, c := range []*testClient{alice, bob} {
		var acc buzzAccepted
		expect(t, c, "buzz-accepted", &acc)
		if winner == "" {
			winner = acc.Name
		} else if acc.Name != winner {
			t.Fatalf("clients disagree on winner: %q vs %q", winner, acc.Name)
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

func TestBuzzEdgeCases(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	anon := join(r, RolePlayer, nil)
	expect(t, anon, "welcome", nil)
	send(r, anon, "buzz", nil)
	expectError(t, anon, "not-identified")

	// Duplicate buzzes count once; a buzz timestamped after the window is ignored.
	now := time.Now()
	r.events <- event{kind: evMessage, client: alice.Client, msg: NewMessage("buzz", nil), at: now}
	r.events <- event{kind: evMessage, client: alice.Client, msg: NewMessage("buzz", nil), at: now}
	r.events <- event{kind: evMessage, client: bob.Client, msg: NewMessage("buzz", nil), at: now.Add(time.Second)}
	var acc buzzAccepted
	expect(t, ctrl, "buzz-accepted", &acc)
	if acc.Name != "alice" {
		t.Fatalf("winner = %q, want alice (bob buzzed too late)", acc.Name)
	}

	// Kicking the holder reopens the buzzer.
	send(r, ctrl, "kick", map[string]string{"name": "alice"})
	expect(t, ctrl, "buzz-available", nil)

	// Kicking the only candidate closes the window without winner.
	carol := newPlayer(t, r, "carol")
	send(r, bob, "buzz", nil)
	send(r, ctrl, "kick", map[string]string{"name": "bob"})
	time.Sleep(2 * buzzWindow)
	send(r, carol, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", &acc)
	if acc.Name != "carol" {
		t.Fatalf("winner = %q, want carol", acc.Name)
	}
}

func TestValidate(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	send(r, ctrl, "validate", map[string]bool{"correct": true})
	expectError(t, ctrl, "no-pending-answer")

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", "x")
	expectError(t, ctrl, "bad-data")
	send(r, ctrl, "validate", nil)
	expectError(t, ctrl, "bad-data")

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
	var acc buzzAccepted
	expect(t, ctrl, "buzz-accepted", &acc)
	if acc.Name != "bob" {
		t.Fatalf("second buzz winner = %q, want bob", acc.Name)
	}

	send(r, ctrl, "validate", map[string]bool{"correct": true})
	expect(t, ctrl, "answer-result", &res)
	if res.Name != "bob" || !res.Correct || res.Points < 95 || res.Points > 100 {
		t.Fatalf("result = %+v, want bob correct with ~100 points", res)
	}

	send(r, ctrl, "skip", nil)
	expect(t, alice, "buzz-available", nil)
}

func TestAnswerTimeout(t *testing.T) {
	r := NewRoom()
	r.answerTime = 20 * time.Millisecond
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)

	send(r, alice, "buzz", nil)
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	if res.Name != "alice" || res.Correct {
		t.Fatalf("result = %+v, want alice timed out", res)
	}
}

func TestStaleTimersIgnored(t *testing.T) {
	r := NewRoom()
	r.answerTime = 30 * time.Millisecond
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	// Pausing during the buzz window discards alice's pending buzz.
	send(r, alice, "buzz", nil)
	send(r, ctrl, "pause", nil)
	time.Sleep(2 * buzzWindow)
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)
	send(r, bob, "buzz", nil)
	var acc buzzAccepted
	expect(t, ctrl, "buzz-accepted", &acc)
	if acc.Name != "bob" {
		t.Fatalf("winner = %q, want bob", acc.Name)
	}

	// Validating before the answer timeout makes the timer stale.
	send(r, ctrl, "validate", map[string]bool{"correct": true})
	expect(t, ctrl, "answer-result", nil)
	time.Sleep(2 * r.answerTime)
	send(r, ctrl, "sync", nil)
	for {
		m := <-ctrl.inbox
		if m.Type == "answer-result" {
			t.Fatal("stale answer timeout resolved an answer")
		}
		if m.Type == "error" {
			return
		}
	}
}

func TestPauseWhileAnswering(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "pause", nil)
	expect(t, ctrl, "game-paused", nil)
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)

	send(r, ctrl, "pause", nil)
	expect(t, ctrl, "game-paused", nil)
	// A wrong answer while paused reopens the buzzer silently, until the game resumes.
	send(r, ctrl, "validate", map[string]bool{"correct": false})
	expect(t, ctrl, "answer-result", nil)
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)
	expect(t, bob, "buzz-available", nil)

	send(r, bob, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", map[string]bool{"correct": true})
	expect(t, ctrl, "answer-result", nil)
	send(r, ctrl, "pause", nil)
	expect(t, ctrl, "game-paused", nil)
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)
}
