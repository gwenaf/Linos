package game

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gwenaf/linos/internal/pack"
)

// Edge cases found by mutation testing (scripts/mutation.sh): bounds taken exactly, a value at zero,
// a state left on the wrong side of a condition, and timers armed after the clock has run.

// newPlayerWithToken is newPlayer that also returns the session token, to reconnect the player.
func newPlayerWithToken(t *testing.T, r *Room, name string) (*testClient, string) {
	t.Helper()
	c := join(r, RolePlayer, nil)
	var w struct {
		Token string `json:"token"`
	}
	expect(t, c, "welcome", &w)
	send(r, c, "identify", map[string]string{"name": name})
	expect(t, c, "lobby-update", nil)
	return c, w.Token
}

// playerState is the part of a player's state snapshot these tests read.
type playerState struct {
	Jokers    int       `json:"jokers"`
	CanAnswer bool      `json:"canAnswer"`
	Tiebreak  []unitMsg `json:"tiebreak"`
}

func reconnect(t *testing.T, r *Room, token string) playerState {
	t.Helper()
	c := join(r, RolePlayer, obj{"token": token})
	var s playerState
	expect(t, c, "state", &s)
	return s
}

func TestNameOfMaximumLength(t *testing.T) {
	r := newRoom(t, nil)
	c := join(r, RolePlayer, nil)
	expect(t, c, "welcome", nil)
	name := strings.Repeat("é", maxNameLen)
	send(r, c, "identify", map[string]string{"name": name})
	lobby := awaitLobby(t, c, func(l lobbyUpdate) bool { return len(l.Players) == 1 })
	if lobby.Players[0].Name != name {
		t.Fatalf("players = %+v, want the %d-letter name accepted", lobby.Players, maxNameLen)
	}
}

func TestMatchBounds(t *testing.T) {
	// From 4 letters on, typos are tolerated: one in 4 is within a 50 % fuzziness.
	daho := &pack.Guess{Type: "text", Answers: []string{"Daho"}}
	if !matches(daho, "Dahi", 0.5) {
		t.Error("a 4-letter answer must tolerate typos")
	}
	// The tolerance is inclusive: 2 typos in 10 letters at 20 %.
	floyd := &pack.Guess{Type: "text", Answers: []string{"Pink Floyd"}}
	if !matches(floyd, "pinc floid", 0.2) || matches(floyd, "pinc fliid", 0.2) {
		t.Error("2 typos in 10 letters must pass at 20 %, 3 must not")
	}
}

func TestReadyNeedsAConnectedPlayer(t *testing.T) {
	r := newRoom(t, nil)
	ctrl := newControl(t, r)
	// Nobody identified yet: "everyone is ready" holds for no one, the room stays in the lobby.
	anon := join(r, RolePlayer, nil)
	expect(t, anon, "welcome", nil)
	settle(t, r, ctrl)
	_, s := rejoin(t, r, RoleControl, nil)
	if s.State != stateLobby {
		t.Fatalf("state = %s with nobody identified, want lobby", s.State)
	}
}

func TestRejoiningAsAnotherPlayerLeavesTheFirst(t *testing.T) {
	r := newRoom(t, nil)
	ctrl := newControl(t, r)
	c := newPlayer(t, r, "alice")
	send(r, c, "join", nil)
	awaitLobby(t, ctrl, func(l lobbyUpdate) bool {
		return len(l.Players) == 1 && l.Players[0].Name == "alice" && !l.Players[0].Connected
	})
}

func TestSkipAfterTheTrackEnded(t *testing.T) {
	r := newRoom(t, basicManifest(2))
	alice := newPlayer(t, r, "alice")
	ctrl, host := startGame(t, r, alice)
	buzzAndValidate(t, r, ctrl, alice, true)
	expect(t, host, "track-end", nil)
	send(r, ctrl, "skip", nil)
	for {
		m := <-host.inbox
		if m.Type == "track-end" {
			t.Fatal("an ended track must not end twice")
		}
		if m.Type == "track-start" {
			return
		}
	}
}

func TestJokersLeftInSnapshot(t *testing.T) {
	m := basicManifest(1)
	m["rules"] = obj{"jokers": obj{"double": 2}}
	r := newRoom(t, m)
	alice, token := newPlayerWithToken(t, r, "alice")
	ctrl, host := launch(t, r, obj{"pack": "basic"}, alice)
	expect(t, host, "track-start", nil)
	send(r, alice, "use-joker", obj{"type": "double"})
	expect(t, ctrl, "joker-used", nil)
	if s := reconnect(t, r, token); s.Jokers != 1 {
		t.Fatalf("jokers = %d, want 1 of 2 left", s.Jokers)
	}
}

func TestEliminationBounds(t *testing.T) {
	m := themedManifest("fr", "fr")
	m["rounds"] = list{
		obj{"name": "R1", "selection": "sequence", "tracks": list{"t1"}},
		// More eliminated than units: one stays.
		obj{"name": "R2", "selection": "sequence", "tracks": list{"t2"}, "eliminate": 5},
	}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, host := startGame(t, r, alice, bob)

	send(r, ctrl, "skip", nil)
	expect(t, ctrl, "round-end", nil)
	expectNo(t, r, ctrl, "eliminated")

	playTrack(t, r, ctrl, host)
	send(r, ctrl, "skip", nil)
	var out struct{ Units []unitMsg }
	expect(t, ctrl, "eliminated", &out)
	if len(out.Units) != 1 {
		t.Fatalf("eliminated = %+v, want one of the two", out)
	}
}

func TestTargetScoreReachedExactly(t *testing.T) {
	m := basicManifest(2)
	m["game"] = obj{"end": obj{"type": "score", "target": 60}}
	m["rules"] = obj{"scoring": obj{"type": "fixed", "max": 60}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	buzzAndValidate(t, r, ctrl, alice, true)
	expect(t, ctrl, "game-end", nil)
}

func TestNoReboundBonusWithoutAMistake(t *testing.T) {
	r := newRoom(t, withAnswerRules(nil, obj{"type": "fixed", "max": 10, "reboundBonus": 15}))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	if res := buzzAndValidate(t, r, ctrl, alice, true); res.Points != 10 {
		t.Fatalf("points = %d, want 10 without rebound bonus", res.Points)
	}
}

func TestNoLockoutByDefault(t *testing.T) {
	r := newRoom(t, withAnswerRules(obj{"attempts": 2, "rebound": "all"}, nil))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	buzzAndValidate(t, r, ctrl, alice, false)
	expectNo(t, r, ctrl, "lockout")
	buzzAndValidate(t, r, ctrl, alice, true)
}

func TestKickedHolderLetsTheFailedTeamBuzz(t *testing.T) {
	r := newRoom(t, withAnswerRules(obj{"attempts": 2, "rebound": "others"}, nil))
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl, _ := startGame(t, r, alice, bob)
	buzzAndValidate(t, r, ctrl, alice, false)
	send(r, bob, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "kick", obj{"name": "bob"})
	expect(t, ctrl, "kicked", nil)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
}

func TestPickerLeavingHandsOverToTheNext(t *testing.T) {
	m := themedManifest("fr", "fr")
	m["rounds"] = list{obj{"name": "Choix", "selection": "theme-pick", "themes": list{"fr"}, "count": 2}}
	r := newRoom(t, m)
	names := []string{"alice", "bob", "carol", "dave"}
	var players []*testClient
	for _, n := range names {
		players = append(players, newPlayer(t, r, n))
	}
	ctrl, _ := launch(t, r, obj{"pack": "basic"}, players...)
	var req struct{ Picker unitMsg }
	expect(t, ctrl, "theme-pick-request", &req)
	if req.Picker.Name != "alice" {
		t.Fatalf("first picker = %+v, want alice", req.Picker)
	}
	send(r, ctrl, "kick", obj{"name": "alice"})
	expect(t, ctrl, "theme-pick-request", &req)
	if req.Picker.Name != "bob" {
		t.Fatalf("picker = %+v, want bob, next after alice", req.Picker)
	}
}

func TestHolderCanAnswerAfterReconnecting(t *testing.T) {
	r := newRoom(t, deviceManifest(1, obj{"via": "device"}))
	alice, token := newPlayerWithToken(t, r, "alice")
	ctrl, _ := startGameWith(t, r, obj{"pack": "basic", "control": "auto"}, alice)
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	if s := reconnect(t, r, token); !s.CanAnswer {
		t.Fatal("the holder must still be able to answer after reconnecting")
	}
}

func TestRanksKeptWhenAGuessScoringHasNone(t *testing.T) {
	m := deviceManifest(1, obj{"mode": "simultaneous", "via": "device"})
	m["rules"].(obj)["scoring"] = obj{"type": "rank", "ranks": list{50, 20}}
	m["tracks"].(list)[0].(obj)["guesses"].(list)[0].(obj)["scoring"] = obj{"max": 5}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGameWith(t, r, obj{"pack": "basic", "control": "auto"}, alice)
	send(r, alice, "answer", obj{"value": "take on me"})
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	if res.Points != 50 {
		t.Fatalf("points = %d, want the pack's first rank, 50", res.Points)
	}
}

// pauseAfter plays for played, then pauses for paused and resumes.
func pauseAfter(t *testing.T, r *Room, ctrl *testClient, played, paused time.Duration) {
	t.Helper()
	time.Sleep(played)
	send(r, ctrl, "pause", nil)
	expect(t, ctrl, "game-paused", nil)
	time.Sleep(paused)
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)
}

// The timers below are armed once part of the track has been played: they must wait for what is
// left, not more. Bounds leave a wide margin for slow machines.

func TestTrackTimerAfterAPause(t *testing.T) {
	r := newRoom(t, withRules(basicManifest(1), obj{"duration": 1}))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	pauseAfter(t, r, ctrl, 500*time.Millisecond, 50*time.Millisecond)
	resumed := time.Now()
	expect(t, ctrl, "track-end", nil)
	if waited := time.Since(resumed); waited > 900*time.Millisecond {
		t.Fatalf("track ended %v after resuming, want the 0.5 s left", waited)
	}
}

func TestHeadStartAfterAPause(t *testing.T) {
	m := themedManifest("fr")
	m["rules"] = obj{"owner": obj{"headStart": 1}}
	m["rounds"] = list{obj{"name": "Choix", "selection": "theme-pick", "themes": list{"fr"}, "count": 1}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	ctrl, host := launch(t, r, obj{"pack": "basic"}, alice)
	expect(t, ctrl, "theme-pick-request", nil)
	send(r, alice, "pick-theme", obj{"theme": "fr"})
	playTrack(t, r, ctrl, host)
	pauseAfter(t, r, ctrl, 500*time.Millisecond, 50*time.Millisecond)
	resumed := time.Now()
	expect(t, ctrl, "head-start-over", nil)
	if waited := time.Since(resumed); waited > 900*time.Millisecond {
		t.Fatalf("head start over %v after resuming, want the 0.5 s left", waited)
	}
}

func TestLockoutLiftedLateInTheTrack(t *testing.T) {
	r := newRoom(t, withAnswerRules(obj{"attempts": 2, "rebound": "all", "wrongLockout": 0.3}, nil))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	time.Sleep(500 * time.Millisecond)
	buzzAndValidate(t, r, ctrl, alice, false)
	// On alice's phone: the buzz-available of the reopened buzz comes first, the one that matters
	// follows the lockout.
	expect(t, alice, "lockout", nil)
	locked := time.Now()
	expect(t, alice, "buzz-available", nil)
	if waited := time.Since(locked); waited > 800*time.Millisecond {
		t.Fatalf("lockout lifted after %v, want 0.3 s", waited)
	}
}

// Speed points count track time only: a pause before answering costs nothing.
func TestSpeedPointsIgnorePauses(t *testing.T) {
	speed := obj{"type": "speed", "max": 100, "min": 0}

	r := newRoom(t, withRules(basicManifest(1), obj{"duration": 2, "scoring": speed}))
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	pauseAfter(t, r, ctrl, 200*time.Millisecond, 600*time.Millisecond)
	if res := buzzAndValidate(t, r, ctrl, alice, true); res.Points < 70 {
		t.Fatalf("buzz points = %d, want about 90 for 0.2 s of a 2 s track", res.Points)
	}

	m := deviceManifest(1, obj{"mode": "simultaneous", "via": "device"})
	m["rules"].(obj)["duration"], m["rules"].(obj)["scoring"] = 2, speed
	r = newRoom(t, m)
	alice = newPlayer(t, r, "alice")
	ctrl, _ = startGameWith(t, r, obj{"pack": "basic", "control": "auto"}, alice)
	pauseAfter(t, r, ctrl, 200*time.Millisecond, 600*time.Millisecond)
	send(r, alice, "answer", obj{"value": "take on me"})
	var res answerResult
	expect(t, ctrl, "answer-result", &res)
	if res.Points < 70 {
		t.Fatalf("answer points = %d, want about 90 for 0.2 s of a 2 s track", res.Points)
	}
}

// A configured archive stays open: its media are read from it during the game.
func TestArchivePackServesMedia(t *testing.T) {
	r := newRoom(t, nil)
	f, err := os.Create(filepath.Join(r.packsDir, "zipped.linospack"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	manifest, _ := json.Marshal(basicManifest(1))
	for name, body := range map[string][]byte{"manifest.json": manifest, "a.mp3": []byte("audio")} {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		w.Write(body)
	}
	zw.Close()
	f.Close()

	ctrl := newControl(t, r)
	send(r, ctrl, "configure", obj{"pack": "zipped.linospack"})
	expect(t, ctrl, "configured", nil)
	m, err := r.Media("a.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if b, err := io.ReadAll(m); err != nil || string(b) != "audio" {
		t.Fatalf("media = %q, %v", b, err)
	}
	// Another pack closes the archive, so the test folder can be removed (Windows locks open files).
	send(r, ctrl, "configure", obj{"pack": "basic"})
	expect(t, ctrl, "configured", nil)
}

// Buzzes of two teams in the same window are both candidates: either may win the draw.
func TestBuzzWindowDrawsAmongTeams(t *testing.T) {
	winners := map[string]bool{}
	for range 30 {
		r := newRoom(t, nil)
		alice := newPlayer(t, r, "alice")
		bob := newPlayer(t, r, "bob")
		ctrl, _ := startGame(t, r, alice, bob)
		send(r, alice, "buzz", nil)
		send(r, bob, "buzz", nil)
		var acc buzzAccepted
		expect(t, ctrl, "buzz-accepted", &acc)
		winners[acc.Name] = true
	}
	if !winners["alice"] || !winners["bob"] {
		t.Fatalf("winners over 30 draws = %v, want both (the odds of missing one are 2^-29)", winners)
	}
}
