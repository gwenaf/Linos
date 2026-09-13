package game

import (
	"testing"
	"time"
)

// withAnswerRules is the basic single-track pack with answer and scoring rules.
func withAnswerRules(answer, scoring obj) obj {
	m := basicManifest(1)
	m["rules"] = obj{"answer": answer, "scoring": scoring}
	return m
}

func buzzAndValidate(t *testing.T, r *Room, ctrl, c *testClient, correct bool) answerResult {
	t.Helper()
	send(r, c, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": correct})
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	return res
}

func TestAttemptsAndReboundOthers(t *testing.T) {
	r := newRoom(t, withAnswerRules(obj{"attempts": 2, "rebound": "others"}, obj{"type": "fixed", "max": 10, "reboundBonus": 15}))
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	buzzAndValidate(t, r, ctrl, alice, false)
	send(r, alice, "buzz", nil)
	expectError(t, alice, "rebound")
	buzzAndValidate(t, r, ctrl, bob, false)
	// bob took the hand, so alice may use her second attempt.
	buzzAndValidate(t, r, ctrl, alice, false)
	send(r, alice, "buzz", nil)
	expectError(t, alice, "no-attempt-left")

	if res := buzzAndValidate(t, r, ctrl, bob, true); res.Points != 25 {
		t.Fatalf("points = %d, want 10 + 15 rebound bonus", res.Points)
	}
}

func TestReboundAllAndNone(t *testing.T) {
	r := newRoom(t, withAnswerRules(obj{"attempts": 2, "rebound": "all"}, nil))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	buzzAndValidate(t, r, ctrl, alice, false)
	buzzAndValidate(t, r, ctrl, alice, false)

	r = newRoom(t, withAnswerRules(obj{"rebound": "none"}, nil))
	alice = newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ = startGame(t, r, alice, bob)
	buzzAndValidate(t, r, ctrl, alice, false)
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "missed" {
		t.Fatalf("track-end = %+v, want missed: nobody else gets the hand", end)
	}
}

func TestWrongLockout(t *testing.T) {
	r := newRoom(t, withAnswerRules(obj{"attempts": 2, "rebound": "all", "wrongLockout": 0.3}, nil))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)

	buzzAndValidate(t, r, ctrl, alice, false)
	var lock struct {
		Name     string
		Duration float64
	}
	expect(t, ctrl, "lockout", &lock)
	if lock.Name != "alice" || lock.Duration != 0.3 {
		t.Fatalf("lockout = %+v", lock)
	}
	start := time.Now()
	send(r, alice, "buzz", nil)
	expectError(t, alice, "locked")

	// The lockout counts track time: a long pause ignores the first timer, a short one makes the next fire early.
	time.Sleep(60 * time.Millisecond)
	send(r, ctrl, "pause", nil)
	time.Sleep(300 * time.Millisecond)
	send(r, ctrl, "resume", nil)
	time.Sleep(60 * time.Millisecond)
	send(r, ctrl, "pause", nil)
	time.Sleep(60 * time.Millisecond)
	send(r, ctrl, "resume", nil)
	expect(t, alice, "game-resumed", nil)
	expect(t, alice, "game-resumed", nil)
	expect(t, alice, "buzz-available", nil)
	if waited := time.Since(start); waited < 600*time.Millisecond {
		t.Fatalf("lockout lifted after %v, want 300 ms of play plus the pauses", waited)
	}
	buzzAndValidate(t, r, ctrl, alice, true)
}

func TestPauseOnBuzzOff(t *testing.T) {
	m := withAnswerRules(obj{"pauseOnBuzz": false, "rebound": "all", "attempts": 3}, nil)
	m["rules"].(obj)["duration"] = 0.3
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)

	buzzAndValidate(t, r, ctrl, alice, false)
	// The clock keeps running while alice holds the hand: time runs out during her answer.
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "time" {
		t.Fatalf("track-end = %+v, want time", end)
	}
}

func TestSimultaneousAttempts(t *testing.T) {
	m := withAnswerRules(obj{"mode": "simultaneous", "via": "device", "attempts": 2, "wrongLockout": 0.05}, nil)
	m["tracks"].(list)[0].(obj)["guesses"] = list{obj{"label": "Titre", "type": "text", "answers": list{"Take On Me"}}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	send(r, bob, "answer", obj{"value": "Europe"})
	var res struct {
		Correct   bool
		Final     bool
		Remaining int
	}
	expect(t, bob, "answer-result", &res)
	if res.Correct || res.Final || res.Remaining != 1 {
		t.Fatalf("first answer = %+v, want one attempt left", res)
	}
	send(r, bob, "answer", obj{"value": "Take on me"})
	expectError(t, bob, "locked")
	expectNo(t, r, ctrl, "answered")

	time.Sleep(100 * time.Millisecond)
	send(r, bob, "answer", obj{"value": "Take on me"})
	expect(t, bob, "answer-result", &res)
	if !res.Correct || !res.Final {
		t.Fatalf("second answer = %+v, want correct and final", res)
	}
	expect(t, ctrl, "answered", nil)
}
