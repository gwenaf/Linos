package game

import (
	"reflect"
	"testing"
	"time"
)

type scoreUpdate struct {
	Name  string `json:"name"`
	Team  string `json:"team"`
	Score int    `json:"score"`
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

func TestScoreAdjust(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	newPlayer(t, r, "bob")
	send(r, alice, "join-team", map[string]string{"team": "rouge"})
	ctrl := newControl(t, r)

	send(r, ctrl, "score-adjust", "x")
	expectError(t, ctrl, "bad-data")
	send(r, ctrl, "score-adjust", map[string]any{"team": "nope", "delta": 1})
	expectError(t, ctrl, "unknown-team")
	send(r, ctrl, "score-adjust", map[string]any{"name": "nope", "delta": 1})
	expectError(t, ctrl, "unknown-player")

	var score scoreUpdate
	send(r, ctrl, "score-adjust", map[string]any{"team": "ROUGE", "delta": 7})
	expect(t, ctrl, "score-update", &score)
	if score != (scoreUpdate{"", "rouge", 7}) {
		t.Fatalf("score = %+v, want rouge 7", score)
	}
	send(r, ctrl, "score-adjust", map[string]any{"name": "alice", "delta": 3})
	expect(t, ctrl, "score-update", &score)
	if score != (scoreUpdate{"", "rouge", 10}) {
		t.Fatalf("score = %+v, want alice's points credited to rouge", score)
	}
	send(r, ctrl, "score-adjust", map[string]any{"name": "bob", "delta": -4})
	score = scoreUpdate{}
	expect(t, ctrl, "score-update", &score)
	if score != (scoreUpdate{"bob", "", -4}) {
		t.Fatalf("score = %+v, want bob -4", score)
	}
}

func TestResults(t *testing.T) {
	rouge := &team{name: "rouge", score: 5}
	r := &Room{
		teams: []*team{rouge},
		players: map[string]*player{
			"a": {name: "alice", score: 99, team: rouge},
			"b": {name: "bob", score: 12},
			"c": {score: 50},
		},
	}
	want := []result{{Name: "bob", Score: 12}, {Team: "rouge", Score: 5}}
	if got := r.results(); !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %+v, want %+v", got, want)
	}
}

func TestTeamScoring(t *testing.T) {
	r := NewRoom()
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	carol := newPlayer(t, r, "carol")
	send(r, alice, "join-team", map[string]string{"team": "rouge"})
	send(r, bob, "join-team", map[string]string{"team": "rouge"})
	send(r, carol, "join-team", map[string]string{"team": "bleu"})
	sync(t, r, carol)
	ctrl, _ := startGame(t, r, alice, bob, carol)

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", map[string]bool{"correct": false})
	expect(t, ctrl, "buzz-available", nil)

	// alice's team already tried: bob is excluded with her, carol takes the hand.
	send(r, bob, "buzz", nil)
	send(r, carol, "buzz", nil)
	var acc buzzAccepted
	expect(t, ctrl, "buzz-accepted", &acc)
	if acc != (buzzAccepted{"carol", "bleu"}) {
		t.Fatalf("winner = %+v, want carol of bleu", acc)
	}

	send(r, ctrl, "validate", map[string]bool{"correct": true})
	var score scoreUpdate
	expect(t, ctrl, "score-update", &score)
	if score.Team != "bleu" || score.Name != "" || score.Score < 95 {
		t.Fatalf("score = %+v, want team bleu credited", score)
	}
}
