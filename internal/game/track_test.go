package game

import (
	"encoding/json"
	"testing"
	"time"
)

type trackStart struct {
	Index        int             `json:"index"`
	Total        int             `json:"total"`
	Round        int             `json:"round"`
	Media        string          `json:"media"`
	PlaybackRate float64         `json:"playbackRate"`
	Guesses      json.RawMessage `json:"guesses"`
}

type trackEnd struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// expectNo fails if a message of that type arrives before everything sent so far is processed.
func expectNo(t *testing.T, r *Room, c *testClient, typ string) {
	t.Helper()
	send(r, c, "sync", nil)
	for {
		m := <-c.inbox
		if m.Type == typ {
			t.Fatalf("unexpected %s: %s", typ, m.Data)
		}
		if m.Type == "error" {
			return
		}
	}
}

func withRules(m obj, rules obj) obj {
	m["rules"] = rules
	return m
}

func TestTrackFlow(t *testing.T) {
	r := newRoom(t, basicManifest(2))
	alice := newPlayer(t, r, "alice")
	ctrl := newControl(t, r)
	send(r, ctrl, "configure", obj{"pack": "basic"})
	host := join(r, RoleHost, nil)
	send(r, alice, "ready", map[string]bool{"ready": true})
	expectState(t, ctrl, stateReady)
	send(r, ctrl, "start-game", nil)

	var ts trackStart
	expect(t, alice, "track-start", &ts)
	if ts.Index != 0 || ts.Total != 2 || ts.Round != -1 || ts.Media != "" || string(ts.Guesses) != `[{"label":"Titre","type":"text"}]` {
		t.Fatalf("player track-start = %+v %s, want guesses without answers", ts, ts.Guesses)
	}
	expect(t, host, "track-start", &ts)
	if ts.Media != "/media/a.mp3" || ts.PlaybackRate != 1 {
		t.Fatalf("host track-start = %+v, want media url and rate 1", ts)
	}
	expect(t, ctrl, "track-start", &ts)
	var guesses []struct {
		Answers []string `json:"answers"`
	}
	json.Unmarshal(ts.Guesses, &guesses)
	if len(guesses) != 1 || guesses[0].Answers[0] != "x" {
		t.Fatalf("control guesses = %s, want answers", ts.Guesses)
	}

	// The buzzer only opens once the host plays the media.
	send(r, alice, "buzz", nil)
	expectNo(t, r, ctrl, "buzz-accepted")
	send(r, host, "media-started", nil)
	expect(t, ctrl, "timer-start", nil)
	send(r, host, "media-started", nil)
	expectError(t, host, "wrong-state")

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true})
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end != (trackEnd{0, "found"}) {
		t.Fatalf("track-end = %+v, want track 0 found", end)
	}

	send(r, ctrl, "skip", nil)
	playTrack(t, r, ctrl, host)
	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "track-end", &end)
	if end != (trackEnd{1, "skipped"}) {
		t.Fatalf("track-end = %+v, want track 1 skipped", end)
	}
	var over gameEnd
	expect(t, ctrl, "game-end", &over)
	if over.Reason != "ended" {
		t.Fatalf("game-end = %+v, want ended after the last track", over)
	}
	expectState(t, ctrl, stateLobby)
}

func TestTrackTimeUp(t *testing.T) {
	r := newRoom(t, withRules(basicManifest(2), obj{"duration": 0.05}))
	alice := newPlayer(t, r, "alice")
	ctrl, host := startGame(t, r, alice)

	// The first track's timer fires during the second track and is ignored.
	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "track-end", nil)
	expect(t, host, "track-start", nil)
	time.Sleep(100 * time.Millisecond)
	expectNo(t, r, ctrl, "track-end")

	send(r, host, "media-started", nil)
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end != (trackEnd{1, "time"}) {
		t.Fatalf("track-end = %+v, want track 1 time", end)
	}
}

func TestTrackDurationOverridesRules(t *testing.T) {
	m := withRules(basicManifest(1), obj{"duration": 30})
	m["tracks"].(list)[0].(obj)["duration"] = 0.05
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "time" {
		t.Fatalf("track-end = %+v, want the track's own 50 ms duration", end)
	}
}

func TestTrackTimerStopsDuringPause(t *testing.T) {
	r := newRoom(t, withRules(basicManifest(1), obj{"duration": 0.2}))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)

	time.Sleep(50 * time.Millisecond)
	send(r, ctrl, "pause", nil)
	time.Sleep(250 * time.Millisecond)
	expectNo(t, r, ctrl, "track-end")

	send(r, ctrl, "resume", nil)
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "time" {
		t.Fatalf("track-end = %+v, want time after resuming", end)
	}
}

func TestTrackTimerStopsWhileAnswering(t *testing.T) {
	r := newRoom(t, withRules(basicManifest(1), obj{"duration": 0.2}))
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	time.Sleep(50 * time.Millisecond)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	time.Sleep(100 * time.Millisecond)
	send(r, ctrl, "validate", obj{"correct": false})
	// The original timer fires while less than 200 ms were played and is re-armed.
	time.Sleep(80 * time.Millisecond)
	expectNo(t, r, ctrl, "track-end")
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "time" {
		t.Fatalf("track-end = %+v, want time", end)
	}
}

func TestMultipleGuesses(t *testing.T) {
	m := withRules(basicManifest(1), obj{"scoring": obj{"wrongPenalty": 5}})
	track := m["tracks"].(list)[0].(obj)
	track["guesses"] = list{
		obj{"label": "Artiste", "type": "text", "answers": list{"a-ha"}},
		obj{"label": "Titre", "type": "choice", "choices": list{"Take On Me", "Hunting High"}, "answer": "Take On Me",
			"scoring": obj{"type": "fixed", "max": 50, "min": 10}},
	}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	send(r, bob, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": false})
	var score scoreUpdate
	expect(t, ctrl, "score-update", &score)
	if score != (scoreUpdate{"bob", "", -5}) {
		t.Fatalf("score = %+v, want bob -5 (wrong penalty)", score)
	}

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true, "guess": "Titre"})
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	if res != (answerResult{"alice", "Titre", true, 50}) {
		t.Fatalf("result = %+v, want alice Titre fixed 50", res)
	}

	// A guess is still up: everyone, bob included, may buzz again.
	expect(t, bob, "buzz-available", nil)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true, "guess": "Titre"})
	expectError(t, ctrl, "unknown-guess")
	send(r, ctrl, "validate", obj{"correct": true})
	expect(t, ctrl, "answer-result", &res)
	if res.Guess != "Artiste" {
		t.Fatalf("result = %+v, want the remaining guess Artiste", res)
	}
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "found" {
		t.Fatalf("track-end = %+v, want found", end)
	}
}

func TestRounds(t *testing.T) {
	m := basicManifest(4)
	m["themes"] = list{obj{"id": "fr", "name": "FR"}, obj{"id": "en", "name": "EN"}}
	tracks := m["tracks"].(list)
	tracks[2].(obj)["themes"] = list{"en"}
	tracks[2].(obj)["playbackRate"] = 1.25
	m["rounds"] = list{
		obj{"name": "Seq", "selection": "sequence", "tracks": list{"t1"}},
		// t1 is already played: one track is drawn among t2 and t4.
		obj{"name": "Rand", "selection": "random", "themes": list{"fr"}, "count": 1, "rules": obj{"duration": 12}},
		// Only t3 is English: the count is capped to what exists.
		obj{"name": "English", "selection": "random", "themes": list{"en"}, "count": 5},
	}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl := newControl(t, r)
	send(r, ctrl, "configure", obj{"pack": "basic"})
	var conf struct {
		Rounds []string `json:"rounds"`
	}
	expect(t, ctrl, "configured", &conf)
	if len(conf.Rounds) != 3 {
		t.Fatalf("configured rounds = %v", conf.Rounds)
	}
	host := join(r, RoleHost, nil)
	send(r, alice, "ready", map[string]bool{"ready": true})
	expectState(t, ctrl, stateReady)
	send(r, ctrl, "start-game", nil)

	type roundStart struct {
		Index int    `json:"index"`
		Name  string `json:"name"`
	}
	var game struct {
		Tracks int `json:"tracks"`
	}
	expect(t, ctrl, "game-start", &game)
	if game.Tracks != 3 {
		t.Fatalf("playlist = %d tracks, want t1, then t2 (t1 already played), then t3", game.Tracks)
	}
	for i, name := range []string{"Seq", "Rand", "English"} {
		var rs roundStart
		expect(t, ctrl, "round-start", &rs)
		if rs != (roundStart{i, name}) {
			t.Fatalf("round-start = %+v, want %d %s", rs, i, name)
		}
		var ts struct {
			trackStart
			Duration float64 `json:"duration"`
		}
		expect(t, host, "track-start", &ts)
		if name == "Rand" && ts.Duration != 12 {
			t.Fatalf("round rules not applied: duration %v", ts.Duration)
		}
		if name == "English" && ts.PlaybackRate != 1.25 {
			t.Fatalf("playbackRate = %v, want 1.25", ts.PlaybackRate)
		}
		send(r, ctrl, "skip", nil)
	}
	expect(t, ctrl, "game-end", nil)
}

func TestEmptyPlaylist(t *testing.T) {
	m := basicManifest(1)
	m["themes"] = list{obj{"id": "fr", "name": "FR"}, obj{"id": "en", "name": "EN"}}
	m["rounds"] = list{obj{"name": "Nobody", "selection": "random", "themes": list{"en"}, "count": 1}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl := newControl(t, r)
	send(r, ctrl, "configure", obj{"pack": "basic"})
	join(r, RoleHost, nil)
	send(r, alice, "ready", map[string]bool{"ready": true})
	expectState(t, ctrl, stateReady)
	send(r, ctrl, "start-game", nil)
	expectError(t, ctrl, "empty-playlist")
}
