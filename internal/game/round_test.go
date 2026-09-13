package game

import (
	"reflect"
	"testing"
	"time"
)

// themedManifest has tracks t1..tn with the given themes, and themes fr and en.
func themedManifest(themes ...string) obj {
	m := basicManifest(len(themes))
	for i, th := range themes {
		m["tracks"].(list)[i].(obj)["themes"] = list{th}
	}
	m["themes"] = list{obj{"id": "fr", "name": "FR"}, obj{"id": "en", "name": "EN"}}
	return m
}

type unitMsg struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

func TestThemePick(t *testing.T) {
	m := themedManifest("fr", "fr", "en")
	m["rounds"] = list{obj{
		"name": "Choix", "selection": "theme-pick", "themes": list{"fr", "en"}, "count": 4,
		"rules": obj{"owner": obj{"headStart": 0.1, "othersBonus": 20}, "scoring": obj{"type": "fixed", "max": 10}},
	}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	send(r, alice, "join-team", obj{"team": "rouge"})
	bob := newPlayer(t, r, "bob")
	carol := newPlayer(t, r, "carol")
	ctrl, host := launch(t, r, obj{"pack": "basic"}, alice, bob, carol)

	var pick struct {
		Picker unitMsg `json:"picker"`
		Themes []struct{ ID string }
	}
	expect(t, ctrl, "theme-pick-request", &pick)
	if pick.Picker.Name != "bob" || len(pick.Themes) != 2 {
		t.Fatalf("first pick = %+v, want bob (first by name) choosing among fr and en", pick)
	}
	var s snapshotMsg
	if _, s = rejoin(t, r, RoleControl, nil); s.TrackState != trackPicking || s.Track != nil {
		t.Fatalf("snapshot while picking = %+v", s)
	}

	// The picker leaves: the next team picks instead.
	send(r, ctrl, "kick", obj{"name": "bob"})
	expect(t, ctrl, "theme-pick-request", &pick)
	if pick.Picker.Name != "carol" {
		t.Fatalf("pick after kick = %+v, want carol", pick)
	}
	send(r, alice, "pick-theme", obj{"theme": "en"})
	expectError(t, alice, "not-your-pick")
	send(r, carol, "pick-theme", "x")
	expectError(t, carol, "bad-data")
	send(r, carol, "pick-theme", obj{"theme": "jazz"})
	expectError(t, carol, "unknown-theme")
	send(r, carol, "pick-theme", obj{"theme": "en"})
	var picked struct {
		Name, Theme string
	}
	expect(t, ctrl, "theme-picked", &picked)
	if picked.Name != "carol" || picked.Theme != "en" {
		t.Fatalf("theme-picked = %+v", picked)
	}

	// carol owns the track: alice waits for the head start, then earns the bonus for stealing it.
	var ts struct {
		Owner     unitMsg `json:"owner"`
		HeadStart float64 `json:"headStart"`
	}
	expect(t, host, "track-start", &ts)
	if ts.Owner.Name != "carol" || ts.HeadStart != 0.1 {
		t.Fatalf("track-start = %+v, want carol owner with 0.1 s head start", ts)
	}
	send(r, host, "media-started", nil)
	expect(t, carol, "buzz-available", nil)
	send(r, alice, "buzz", nil)
	expectError(t, alice, "not-allowed")
	expect(t, ctrl, "head-start-over", nil)
	expect(t, alice, "buzz-available", nil)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true})
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	if res.Points != 30 {
		t.Fatalf("points = %d, want 10 + 20 bonus", res.Points)
	}

	// Next picks rotate; en is used up, then fr runs out and the fourth pick is skipped.
	for _, picker := range []*testClient{alice, carol} {
		send(r, ctrl, "skip", nil)
		expect(t, ctrl, "theme-pick-request", &pick)
		if len(pick.Themes) != 1 || pick.Themes[0].ID != "fr" {
			t.Fatalf("themes = %+v, want only fr left", pick.Themes)
		}
		send(r, picker, "pick-theme", obj{"theme": "fr"})
		expect(t, host, "track-start", nil)
	}
	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "round-end", nil)
	expect(t, ctrl, "game-end", nil)
}

func TestExclusiveOwner(t *testing.T) {
	m := themedManifest("fr")
	m["rules"] = obj{"answer": obj{"mode": "simultaneous", "via": "device"}, "owner": obj{"exclusive": true}}
	m["rounds"] = list{obj{"name": "Choix", "selection": "theme-pick", "themes": list{"fr"}, "count": 1}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, host := launch(t, r, obj{"pack": "basic"}, alice, bob)

	expect(t, ctrl, "theme-pick-request", nil)
	send(r, alice, "pick-theme", obj{"theme": "fr"})
	expect(t, host, "track-start", nil)
	send(r, host, "media-started", nil)
	expect(t, ctrl, "timer-start", nil)

	send(r, bob, "answer", obj{"value": "x"})
	expectError(t, bob, "not-allowed")
	send(r, alice, "answer", obj{"value": "x"})
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "answered" {
		t.Fatalf("track-end = %+v, want answered: only the owner plays", end)
	}
}

func TestHeadStartFollowsTheClock(t *testing.T) {
	m := themedManifest("fr")
	m["rules"] = obj{"owner": obj{"headStart": 0.2}}
	m["rounds"] = list{obj{"name": "Choix", "selection": "theme-pick", "themes": list{"fr"}, "count": 1}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, host := launch(t, r, obj{"pack": "basic"}, alice, bob)
	expect(t, ctrl, "theme-pick-request", nil)
	send(r, alice, "pick-theme", obj{"theme": "fr"})
	expect(t, host, "track-start", nil)
	send(r, host, "media-started", nil)
	start := time.Now()

	// A pause during the head start delays it: the first timer fires early and is re-armed,
	// the one armed on resume ends the head start, the last one finds it already over.
	time.Sleep(50 * time.Millisecond)
	send(r, ctrl, "pause", nil)
	time.Sleep(50 * time.Millisecond)
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "head-start-over", nil)
	if waited := time.Since(start); waited < 240*time.Millisecond {
		t.Fatalf("head start over after %v, want it to exclude the 50 ms pause", waited)
	}
	expect(t, bob, "buzz-available", nil)
	time.Sleep(50 * time.Millisecond)
	settle(t, r, ctrl)
}

func TestJokersAndWagers(t *testing.T) {
	m := themedManifest("fr", "fr")
	m["rules"] = obj{"jokers": obj{"double": 2}}
	m["game"] = obj{"tiebreak": obj{"type": "sudden-death"}}
	m["rounds"] = list{
		obj{"name": "Tubes", "selection": "sequence", "tracks": list{"t1"}, "rules": obj{"scoring": obj{"type": "fixed", "max": 50}}},
		obj{"name": "Finale", "selection": "sequence", "tracks": list{"t2"}, "rules": obj{"scoring": obj{"type": "wager"}, "jokers": obj{"double": 0}}},
	}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, host := launch(t, r, obj{"pack": "basic"}, alice, bob)
	expect(t, host, "track-start", nil)

	anon := join(r, RolePlayer, nil)
	expect(t, anon, "welcome", nil)
	send(r, anon, "use-joker", obj{"type": "double"})
	expectError(t, anon, "not-identified")
	send(r, bob, "use-joker", obj{"type": "triple"})
	expectError(t, bob, "unknown-joker")
	send(r, bob, "use-joker", "x")
	expectError(t, bob, "bad-data")
	send(r, alice, "use-joker", obj{"type": "double"})
	var used struct{ Name, Type string }
	expect(t, ctrl, "joker-used", &used)
	if used.Name != "alice" || used.Type != "double" {
		t.Fatalf("joker-used = %+v", used)
	}
	send(r, alice, "use-joker", obj{"type": "double"})
	expectError(t, alice, "no-joker")

	send(r, host, "media-started", nil)
	expect(t, ctrl, "timer-start", nil)
	send(r, bob, "use-joker", obj{"type": "double"})
	expectError(t, bob, "wrong-state")
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true})
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	if res.Points != 100 {
		t.Fatalf("points = %d, want 50 doubled", res.Points)
	}

	// Final: everyone bets up to their score before the track starts.
	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "round-end", nil)
	var wager struct {
		Limits []struct {
			Name   string
			Max    int
			Placed bool
		}
	}
	expect(t, ctrl, "wager-request", &wager)
	if len(wager.Limits) != 2 || wager.Limits[0].Max != 100 || wager.Limits[1].Max != 0 {
		t.Fatalf("wager-request = %+v, want alice up to 100 and bob 0", wager)
	}
	send(r, bob, "use-joker", obj{"type": "double"})
	expectError(t, bob, "no-joker")
	var s snapshotMsg
	if _, s = rejoin(t, r, RoleHost, nil); s.TrackState != trackWagering || s.Track != nil {
		t.Fatalf("snapshot while wagering = %+v", s)
	}

	send(r, alice, "wager", obj{"amount": 150})
	expectError(t, alice, "invalid-wager")
	send(r, alice, "wager", obj{})
	expectError(t, alice, "bad-data")
	send(r, alice, "wager", "x")
	expectError(t, alice, "bad-data")
	send(r, alice, "wager", obj{"amount": 60})
	expect(t, ctrl, "wagered", nil)
	// bob never bets and leaves: the track starts without him.
	send(r, ctrl, "kick", obj{"name": "bob"})
	expect(t, host, "track-start", nil)
	send(r, alice, "wager", obj{"amount": 10})
	expectError(t, alice, "wrong-state")

	send(r, host, "media-started", nil)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": false})
	expect(t, ctrl, "answer-result", &res)
	if res.Points != -60 {
		t.Fatalf("wrong answer = %d, want the wager lost", res.Points)
	}
	send(r, ctrl, "skip", nil)
	var end gameEnd
	expect(t, ctrl, "game-end", &end)
	if !reflect.DeepEqual(end.Results, []result{{Name: "alice", Score: 40}}) {
		t.Fatalf("results = %+v, want alice 100 - 60", end.Results)
	}
}

func TestEliminationEndsTheGame(t *testing.T) {
	m := themedManifest("fr", "fr", "fr")
	m["game"] = obj{"end": obj{"type": "elimination"}}
	m["rounds"] = list{
		obj{"name": "R1", "selection": "sequence", "tracks": list{"t1"}, "eliminate": 1},
		obj{"name": "R2", "selection": "sequence", "tracks": list{"t2"}, "eliminate": 1},
		obj{"name": "R3", "selection": "sequence", "tracks": list{"t3"}},
	}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	carol := newPlayer(t, r, "carol")
	ctrl, host := startGame(t, r, alice, bob, carol)

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true})
	send(r, ctrl, "skip", nil)
	var out struct{ Units []unitMsg }
	expect(t, ctrl, "eliminated", &out)
	if len(out.Units) != 1 || out.Units[0].Name != "bob" {
		t.Fatalf("eliminated = %+v, want bob (tied with carol, first by name)", out)
	}

	playTrack(t, r, ctrl, host)
	send(r, bob, "buzz", nil)
	expectError(t, bob, "not-allowed")
	var s snapshotMsg
	if _, s = rejoin(t, r, RoleControl, nil); len(s.Scores) != 3 {
		t.Fatalf("snapshot = %+v", s)
	}
	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "eliminated", &out)
	if out.Units[0].Name != "carol" {
		t.Fatalf("eliminated = %+v, want carol", out)
	}
	var end gameEnd
	expect(t, ctrl, "game-end", &end)
	if end.Results[0].Name != "alice" {
		t.Fatalf("results = %+v, want alice winning alone", end.Results)
	}
}

func TestTargetScoreEndsTheGame(t *testing.T) {
	m := basicManifest(3)
	m["game"] = obj{"end": obj{"type": "score", "target": 50}}
	m["rules"] = obj{"scoring": obj{"type": "fixed", "max": 60}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	send(r, alice, "join-team", obj{"team": "rouge"})
	ctrl, host := startGame(t, r, alice)

	send(r, ctrl, "skip", nil)
	playTrack(t, r, ctrl, host)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true})
	expect(t, ctrl, "track-end", nil)
	var end gameEnd
	expect(t, ctrl, "game-end", &end)
	if end.Results[0].Score != 60 {
		t.Fatalf("results = %+v, want the game over at 60 points", end.Results)
	}
}

func TestTiebreak(t *testing.T) {
	m := themedManifest("fr", "en", "en")
	m["game"] = obj{"tiebreak": obj{"type": "sudden-death", "themes": list{"en"}}}
	m["rounds"] = list{obj{"name": "R1", "selection": "sequence", "tracks": list{"t1"}}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	carol := newPlayer(t, r, "carol")
	ctrl, host := startGame(t, r, alice, bob, carol)

	send(r, ctrl, "score-adjust", obj{"name": "alice", "delta": 10})
	send(r, ctrl, "score-adjust", obj{"name": "bob", "delta": 10})
	send(r, ctrl, "skip", nil)
	var tb struct{ Units []unitMsg }
	expect(t, ctrl, "tiebreak", &tb)
	if len(tb.Units) != 2 || tb.Units[0].Name != "alice" || tb.Units[1].Name != "bob" {
		t.Fatalf("tiebreak = %+v, want alice and bob", tb)
	}
	playTrack(t, r, ctrl, host)
	send(r, carol, "buzz", nil)
	expectError(t, carol, "not-allowed")
	var s snapshotMsg
	if _, s = rejoin(t, r, RoleControl, nil); s.TrackState != trackLive {
		t.Fatalf("snapshot = %+v", s)
	}

	// Nobody scores: another sudden-death track, then no track is left and the game ends tied.
	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "tiebreak", nil)
	playTrack(t, r, ctrl, host)
	send(r, ctrl, "skip", nil)
	var end gameEnd
	expect(t, ctrl, "game-end", &end)
	if end.Results[0].Score != 10 || end.Results[1].Score != 10 {
		t.Fatalf("results = %+v, want a tie left as is", end.Results)
	}
}

func TestLastPickerLeaves(t *testing.T) {
	m := themedManifest("fr")
	m["rounds"] = list{obj{"name": "Choix", "selection": "theme-pick", "themes": list{"fr"}, "count": 1}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl, _ := launch(t, r, obj{"pack": "basic"}, alice)
	expect(t, ctrl, "theme-pick-request", nil)
	send(r, ctrl, "kick", obj{"name": "alice"})
	expect(t, ctrl, "round-end", nil)
	expect(t, ctrl, "game-end", nil)
}
