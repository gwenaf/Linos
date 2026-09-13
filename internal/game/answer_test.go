package game

import (
	"strings"
	"testing"
)

type submitted struct {
	Name    string `json:"name"`
	Guess   string `json:"guess"`
	Value   string `json:"value"`
	Correct bool   `json:"correct"`
	Points  int    `json:"points"`
}

// deviceManifest is the basic pack whose single guess is answered on the phones.
func deviceManifest(n int, answer obj) obj {
	m := basicManifest(n)
	for _, t := range m["tracks"].(list) {
		t.(obj)["guesses"] = list{obj{"label": "Titre", "type": "text", "answers": list{"Take On Me"}}}
	}
	m["rules"] = obj{"answer": answer}
	return m
}

func TestBuzzThenAnswerOnPhoneAuto(t *testing.T) {
	r := newRoom(t, deviceManifest(2, obj{"via": "device", "fuzziness": 0.3}))
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, host := startGameWith(t, r, obj{"pack": "basic", "control": "auto"}, alice, bob)

	send(r, alice, "answer", obj{"value": "take on me"})
	expectError(t, alice, "not-your-turn")
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, alice, "answer", obj{"guess": "Artiste", "value": "a-ha"})
	expectError(t, alice, "unknown-guess")

	send(r, alice, "answer", obj{"value": "Take on mi"})
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	if res.Name != "alice" || !res.Correct || res.Guess != "Titre" {
		t.Fatalf("result = %+v, want alice right despite the typo", res)
	}
	expect(t, ctrl, "track-end", nil)
	send(r, ctrl, "validate", obj{"correct": true})
	expectError(t, ctrl, "auto-control")

	send(r, ctrl, "skip", nil)
	playTrack(t, r, ctrl, host)
	send(r, bob, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, bob, "answer", obj{"value": "Final Countdown"})
	expect(t, ctrl, "answer-result", &res)
	if res.Name != "bob" || res.Correct {
		t.Fatalf("result = %+v, want bob wrong", res)
	}
	expect(t, alice, "buzz-available", nil)
}

func TestBuzzThenAnswerOnPhoneWithGamemaster(t *testing.T) {
	r := newRoom(t, deviceManifest(1, obj{"via": "device"}))
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, bob, "answer", obj{"value": "take on me"})
	expectError(t, bob, "not-your-turn")

	send(r, alice, "answer", obj{"value": "Take On Me"})
	var sub submitted
	expect(t, ctrl, "answer-submitted", &sub)
	if sub.Name != "alice" || sub.Value != "Take On Me" || !sub.Correct {
		t.Fatalf("submitted = %+v, want alice's answer with a correct suggestion", sub)
	}
	// One answer per hand: control decides.
	send(r, alice, "answer", obj{"value": "again"})
	expectError(t, alice, "not-your-turn")
	send(r, ctrl, "validate", obj{"correct": true, "guess": "Titre"})
	expect(t, ctrl, "track-end", nil)
}

func TestAnswerErrors(t *testing.T) {
	r := newRoom(t, nil)
	alice := newPlayer(t, r, "alice")
	ctrl, host := startGame(t, r, alice)

	send(r, alice, "answer", "x")
	expectError(t, alice, "bad-data")
	send(r, alice, "answer", obj{"value": "x"})
	expectError(t, alice, "oral-answers")
	anon := join(r, RolePlayer, nil)
	expect(t, anon, "welcome", nil)
	send(r, anon, "answer", obj{"value": "x"})
	expectError(t, anon, "not-identified")

	send(r, ctrl, "skip", nil)
	expect(t, host, "track-start", nil)
	send(r, alice, "answer", obj{"value": "x"})
	expectError(t, alice, "wrong-state")
}

func TestSimultaneousAnswers(t *testing.T) {
	m := basicManifest(1)
	m["rules"] = obj{
		"answer":  obj{"mode": "simultaneous", "via": "device"},
		"scoring": obj{"type": "rank", "ranks": list{10, 5}, "wrongPenalty": 2},
	}
	m["tracks"].(list)[0].(obj)["guesses"] = list{
		obj{"label": "Titre", "type": "text", "answers": list{"Take On Me"}},
		obj{"label": "Année", "type": "number", "answer": 1985, "tolerance": 0, "scoring": obj{"type": "fixed", "max": 7}},
		obj{"label": "Artiste", "type": "text", "answers": list{"a-ha"}, "scoring": obj{"type": "speed"}},
	}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	send(r, alice, "join-team", obj{"team": "rouge"})
	bob := newPlayer(t, r, "bob")
	carol := join(r, RolePlayer, nil)
	var w struct {
		Token string `json:"token"`
	}
	expect(t, carol, "welcome", &w)
	send(r, carol, "identify", obj{"name": "carol"})
	expect(t, carol, "lobby-update", nil)
	ctrl, _ := startGame(t, r, alice, bob, carol)

	send(r, alice, "buzz", nil)
	expectError(t, alice, "no-buzz")

	answer := func(c *testClient, guess, value string, want submitted) {
		t.Helper()
		send(r, c, "answer", obj{"guess": guess, "value": value})
		var got submitted
		expect(t, c, "answer-result", &got)
		if got.Guess != want.Guess || got.Correct != want.Correct || (want.Points != -1 && got.Points != want.Points) {
			t.Fatalf("%s answered %q: %+v, want %+v", got.Name, value, got, want)
		}
	}
	answer(alice, "Titre", "take on me", submitted{Guess: "Titre", Correct: true, Points: 10})
	answer(bob, "Titre", "Take On Me", submitted{Guess: "Titre", Correct: true, Points: 5})
	answer(carol, "Titre", "take on me", submitted{Guess: "Titre", Correct: true, Points: 0})
	send(r, alice, "answer", obj{"guess": "Titre", "value": "again"})
	expectError(t, alice, "unknown-guess")

	// Other players learn that alice answered, never whether she was right.
	var ans struct{ Name, Guess string }
	expect(t, ctrl, "answered", &ans)
	if ans.Name != "alice" || ans.Guess != "Titre" {
		t.Fatalf("answered = %+v", ans)
	}
	expectNo(t, r, ctrl, "score-update")

	answer(alice, "", "1985", submitted{Guess: "Année", Correct: true, Points: 7})
	answer(alice, "", "a-ha", submitted{Guess: "Artiste", Correct: true, Points: -1})
	answer(bob, "Année", "1990", submitted{Guess: "Année", Correct: false, Points: -2})
	answer(bob, "Artiste", "Europe", submitted{Guess: "Artiste", Correct: false, Points: -2})

	// A reloaded phone knows what it already answered.
	carol = join(r, RolePlayer, obj{"token": w.Token})
	var s struct {
		Answered  []string `json:"answered"`
		CanAnswer bool     `json:"canAnswer"`
	}
	expect(t, carol, "state", &s)
	if strings.Join(s.Answered, ",") != "Titre" || !s.CanAnswer {
		t.Fatalf("carol snapshot = %+v", s)
	}

	answer(carol, "", "1984", submitted{Guess: "Année", Correct: false, Points: -2})
	answer(carol, "", "Europe", submitted{Guess: "Artiste", Correct: false, Points: -2})
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "answered" {
		t.Fatalf("track-end = %+v, want answered once everyone answered everything", end)
	}
	var score scoreUpdate
	for score.Team != "rouge" || score.Score < 100 { // points arrive one answer at a time
		expect(t, ctrl, "score-update", &score)
	}
	if score.Score < 10+7+95 {
		t.Fatalf("rouge (alice) score = %d, want rank 10 + fixed 7 + speed ~100", score.Score)
	}
}

func TestConfigureControl(t *testing.T) {
	r := newRoom(t, nil)
	write := func(name string, game obj, rules obj, rounds list) {
		m := basicManifest(1)
		if game != nil {
			m["game"] = game
		}
		if rules != nil {
			m["rules"] = rules
		}
		if rounds != nil {
			m["rounds"] = rounds
		}
		writePack(t, r.packsDir, name, m)
	}
	device := obj{"answer": obj{"via": "device"}}
	write("device-auto", obj{"control": obj{"allowed": list{"master", "auto"}, "default": "auto"}}, device, nil)
	write("master-only", obj{"control": obj{"allowed": list{"master"}}}, device, nil)
	write("simultaneous-oral", nil, obj{"answer": obj{"mode": "simultaneous"}}, nil)
	write("rank-buzz", nil, obj{"answer": obj{"via": "device"}, "scoring": obj{"type": "rank"}}, nil)
	write("oral-round", nil, device, list{obj{"name": "R", "selection": "sequence", "tracks": list{"t1"}, "rules": obj{"answer": obj{"via": "oral"}}}})
	ctrl := newControl(t, r)

	cases := []struct {
		pack, control, want string
	}{
		{"basic", "robot", "must be master or auto"},
		{"master-only", "auto", "not allowed by this pack"},
		{"basic", "auto", "rules: oral answers need a gamemaster"},
		{"simultaneous-oral", "", "simultaneous answers must be given on the phones"},
		{"rank-buzz", "", "rank scoring needs simultaneous answers"},
		{"oral-round", "auto", "rounds[0]: oral answers need a gamemaster"},
	}
	for _, tc := range cases {
		send(r, ctrl, "configure", obj{"pack": tc.pack, "control": tc.control})
		var e struct{ Code, Message string }
		expect(t, ctrl, "error", &e)
		if e.Code != "invalid-pack" || !strings.Contains(e.Message, tc.want) {
			t.Errorf("%s/%s: error = %+v, want %q", tc.pack, tc.control, e, tc.want)
		}
	}

	send(r, ctrl, "configure", obj{"pack": "device-auto"})
	var conf struct{ Control string }
	expect(t, ctrl, "configured", &conf)
	if conf.Control != "auto" {
		t.Fatalf("control = %q, want the pack default auto", conf.Control)
	}
}
