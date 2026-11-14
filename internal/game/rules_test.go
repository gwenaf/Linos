package game

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// twoGuesses is a pack of n tracks to find by artist and title, fixed 10 points each.
func twoGuesses(n int, answer obj) obj {
	m := basicManifest(n)
	for _, t := range m["tracks"].(list) {
		t.(obj)["guesses"] = list{
			obj{"label": "Artiste", "type": "text", "answers": list{"a-ha"}},
			obj{"label": "Titre", "type": "text", "answers": list{"Take On Me"}},
		}
	}
	m["rules"] = obj{"answer": answer, "scoring": obj{"type": "fixed", "max": 10, "wrongPenalty": 5}}
	return m
}

type multiResult struct {
	answerResult
	Guesses []string `json:"guesses"`
}

func validateFound(t *testing.T, r *Room, ctrl, c *testClient, guesses ...string) multiResult {
	t.Helper()
	send(r, c, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true, "guesses": guesses})
	var res multiResult
	expect(t, ctrl, "answer-result", &res)
	return res
}

func TestValidateSeveralGuessesAtOnce(t *testing.T) {
	r := newRoom(t, twoGuesses(1, nil))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)

	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true, "guesses": list{"Artiste", "Artiste"}})
	expectError(t, ctrl, "unknown-guess")
	send(r, ctrl, "validate", obj{"correct": true, "guesses": list{"Album"}})
	expectError(t, ctrl, "unknown-guess")
	send(r, ctrl, "validate", obj{"correct": true, "guesses": list{"Titre", "Artiste"}})
	var res multiResult
	expect(t, ctrl, "answer-result", &res)
	if !res.Correct || res.Points != 20 || res.Guess != "Titre" || !reflect.DeepEqual(res.Guesses, []string{"Titre", "Artiste"}) {
		t.Fatalf("answer-result = %+v, want both guesses for 2 × 10 points", res)
	}
	var end trackEnd
	expect(t, ctrl, "track-end", &end)
	if end.Reason != "found" {
		t.Fatalf("track-end = %+v, want found", end)
	}
}

func TestPartialAnswerPaysEachGuess(t *testing.T) {
	r := newRoom(t, twoGuesses(1, obj{"incompletePenalty": 3}))
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)
	if res := validateFound(t, r, ctrl, alice, "Artiste"); !res.Correct || res.Points != 7 {
		t.Fatalf("partial answer = %+v, want 10 minus the 3-point incomplete penalty", res)
	}
	// The title is still to find: the buzz reopens for everyone, alice included.
	if res := validateFound(t, r, ctrl, bob, "Titre"); res.Points != 10 {
		t.Fatalf("last guess = %+v, want 10 and no penalty: nothing left", res)
	}
	expect(t, ctrl, "track-end", nil)
}

func TestAllOrNothingAnswer(t *testing.T) {
	r := newRoom(t, twoGuesses(1, obj{"partial": "all", "rebound": "all", "attempts": 2}))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	if res := validateFound(t, r, ctrl, alice, "Artiste"); res.Correct || res.Points != -5 || len(res.Guesses) != 0 {
		t.Fatalf("incomplete answer = %+v, want it wrong: the 5-point penalty", res)
	}
	if res := validateFound(t, r, ctrl, alice, "Artiste", "Titre"); !res.Correct || res.Points != 20 {
		t.Fatalf("whole answer = %+v, want 20", res)
	}
}

// A phone answers one guess at a time: all-or-nothing does not apply to it.
func TestPhoneAnswersIgnoreAllOrNothing(t *testing.T) {
	m := twoGuesses(1, obj{"partial": "all", "via": "device", "incompletePenalty": 3})
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGameWith(t, r, obj{"pack": "basic", "control": "auto"}, alice)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, alice, "answer", obj{"guess": "Artiste", "value": "a-ha"})
	var res multiResult
	expect(t, ctrl, "answer-result", &res)
	if !res.Correct || res.Points != 10 {
		t.Fatalf("phone answer = %+v, want the artist found for 10, no penalty", res)
	}
}

// themePickGame starts a game whose single round is a theme pick over fr (t1 to t3) and en (t4);
// a name "alice:aigles" puts alice in team aigles.
func themePickGame(t *testing.T, round obj, rules obj, names ...string) (r *Room, ctrl, host *testClient, players []*testClient) {
	t.Helper()
	m := themedManifest("fr", "fr", "fr", "en")
	for i, tr := range m["tracks"].(list) {
		tr.(obj)["guesses"] = list{obj{"label": "Titre", "type": "text", "answers": list{"t" + string(rune('1'+i))}}}
	}
	m["rules"] = rules
	round["name"], round["selection"], round["themes"] = "Choix", "theme-pick", list{"fr", "en"}
	m["rounds"] = list{round}
	r = newRoom(t, m)
	for _, n := range names {
		name, team, _ := strings.Cut(n, ":")
		p := newPlayer(t, r, name)
		if team != "" {
			send(r, p, "join-team", obj{"team": team})
		}
		players = append(players, p)
	}
	ctrl, host = launch(t, r, obj{"pack": "basic"}, players...)
	return r, ctrl, host, players
}

type ownedStart struct {
	Total   int `json:"total"`
	Owner   *unitMsg
	Guesses []struct {
		Answers []string `json:"answers"`
	} `json:"guesses"`
}

func TestWholeThemePick(t *testing.T) {
	r, ctrl, host, players := themePickGame(t, obj{"count": 2, "pickScope": "theme"}, nil, "alice", "bob")
	alice, bob := players[0], players[1]

	expect(t, ctrl, "theme-pick-request", nil)
	send(r, alice, "pick-theme", obj{"theme": "fr"})
	// The whole French theme plays in pack order, alice owning each track.
	for _, want := range []string{"t1", "t2", "t3"} {
		var ts ownedStart
		expect(t, ctrl, "track-start", &ts)
		if ts.Owner == nil || ts.Owner.Name != "alice" || ts.Guesses[0].Answers[0] != want || ts.Total != 4 {
			t.Fatalf("track-start = %+v, want %s owned by alice, 4 tracks in all", ts, want)
		}
		send(r, host, "media-started", nil)
		send(r, ctrl, "skip", nil)
	}
	var req struct{ Picker unitMsg }
	expect(t, ctrl, "theme-pick-request", &req)
	if req.Picker.Name != "bob" {
		t.Fatalf("second pick = %+v, want bob", req)
	}
	send(r, bob, "pick-theme", obj{"theme": "en"})
	var ts ownedStart
	expect(t, ctrl, "track-start", &ts)
	if ts.Owner.Name != "bob" || ts.Guesses[0].Answers[0] != "t4" {
		t.Fatalf("track-start = %+v, want t4 owned by bob", ts)
	}
	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "game-end", nil)
}

func TestOneTrackPerPickByDefault(t *testing.T) {
	r, ctrl, _, players := themePickGame(t, obj{"count": 1}, nil, "alice")
	expect(t, ctrl, "theme-pick-request", nil)
	send(r, players[0], "pick-theme", obj{"theme": "fr"})
	var ts ownedStart
	expect(t, ctrl, "track-start", &ts)
	if ts.Total != 1 {
		t.Fatalf("total = %d, want one track for the one pick", ts.Total)
	}
	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "game-end", nil)
}

// stealRules: fixed 10 points; a team finding on another's theme gets 10 × 2 + 5, the owner loses
// 3 plus those 25 points.
var stealRules = obj{
	"scoring": obj{"type": "fixed", "max": 10},
	"owner":   obj{"othersMultiplier": 2, "othersBonus": 5, "stolenPenalty": 3, "stolenLosesPoints": true},
}

func expectResults(t *testing.T, ctrl *testClient, want map[string]int) {
	t.Helper()
	var end gameEnd
	expect(t, ctrl, "game-end", &end)
	got := map[string]int{}
	for _, res := range end.Results {
		got[res.Name] = res.Score
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %v, want %v", got, want)
	}
}

func TestStolenPointsOnBuzz(t *testing.T) {
	r, ctrl, host, players := themePickGame(t, obj{"count": 1}, stealRules, "alice", "bob")
	alice, bob := players[0], players[1]
	expect(t, ctrl, "theme-pick-request", nil)
	send(r, alice, "pick-theme", obj{"theme": "fr"})
	playTrack(t, r, ctrl, host)
	if res := buzzAndValidate(t, r, ctrl, bob, true); res.Points != 25 {
		t.Fatalf("points = %d, want 10 × 2 + 5", res.Points)
	}
	send(r, ctrl, "skip", nil)
	expectResults(t, ctrl, map[string]int{"alice": -28, "bob": 25})
}

func TestOwnerFindingLosesNothing(t *testing.T) {
	r, ctrl, host, players := themePickGame(t, obj{"count": 1}, stealRules, "alice", "bob")
	alice := players[0]
	expect(t, ctrl, "theme-pick-request", nil)
	send(r, alice, "pick-theme", obj{"theme": "fr"})
	playTrack(t, r, ctrl, host)
	if res := buzzAndValidate(t, r, ctrl, alice, true); res.Points != 10 {
		t.Fatalf("owner points = %d, want plain 10", res.Points)
	}
	send(r, ctrl, "skip", nil)
	expectResults(t, ctrl, map[string]int{"alice": 10, "bob": 0})
}

// With simultaneous answers, the owner's loss is kept hidden like the points, until the track ends.
func TestStolenPointsOnSimultaneousAnswers(t *testing.T) {
	rules := obj{"answer": obj{"mode": "simultaneous", "via": "device"}}
	for k, v := range stealRules {
		rules[k] = v
	}
	m := themedManifest("fr")
	m["tracks"].(list)[0].(obj)["guesses"] = list{obj{"label": "Titre", "type": "text", "answers": list{"Take On Me"}}}
	m["rules"] = rules
	m["rounds"] = list{obj{"name": "Choix", "selection": "theme-pick", "themes": list{"fr"}, "count": 1}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, host := launch(t, r, obj{"pack": "basic", "control": "auto"}, alice, bob)
	expect(t, ctrl, "theme-pick-request", nil)
	send(r, alice, "pick-theme", obj{"theme": "fr"})
	playTrack(t, r, ctrl, host)
	send(r, bob, "answer", obj{"value": "take on me"})
	expect(t, ctrl, "answer-result", nil)
	expectNo(t, r, ctrl, "score-update")
	send(r, ctrl, "skip", nil)
	expectResults(t, ctrl, map[string]int{"alice": -28, "bob": 25})
}

func TestStolenPointsFromATeam(t *testing.T) {
	r, ctrl, host, players := themePickGame(t, obj{"count": 1}, stealRules, "alice:aigles", "bob")
	alice, bob := players[0], players[1]
	expect(t, ctrl, "theme-pick-request", nil)
	send(r, alice, "pick-theme", obj{"theme": "fr"})
	playTrack(t, r, ctrl, host)
	buzzAndValidate(t, r, ctrl, bob, true)
	var update struct {
		Team  string `json:"team"`
		Score int    `json:"score"`
	}
	for update.Team != "aigles" {
		expect(t, ctrl, "score-update", &update)
	}
	if update.Score != -28 {
		t.Fatalf("team aigles = %d, want -28", update.Score)
	}
}

func TestAnswerResultListsGuesses(t *testing.T) {
	r := newRoom(t, twoGuesses(1, nil))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": false})
	var raw json.RawMessage
	expect(t, ctrl, "answer-result", &raw)
	if !strings.Contains(string(raw), `"guesses":[]`) {
		t.Fatalf("wrong answer-result = %s, want an empty guesses list", raw)
	}
}
