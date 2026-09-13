package game

import (
	"encoding/json"
	"testing"
)

type snapshotMsg struct {
	State      string                 `json:"state"`
	Lobby      lobbyUpdate            `json:"lobby"`
	Configured *struct{ Pack string } `json:"configured"`
	Scores     []result               `json:"scores"`
	Paused     *gamePaused            `json:"paused"`
	Round      *struct{ Name string } `json:"round"`
	Track      *trackStart            `json:"track"`
	TrackState string                 `json:"trackState"`
	Elapsed    float64                `json:"elapsed"`
	Found      []string               `json:"found"`
	TrackEnd   *trackEnd              `json:"trackEnd"`
	Holder     *buzzAccepted          `json:"holder"`
	CanBuzz    *bool                  `json:"canBuzz"`
}

// rejoin connects a client (optionally with a token) and returns the state it receives.
func rejoin(t *testing.T, r *Room, role string, data any) (*testClient, snapshotMsg) {
	t.Helper()
	c := join(r, role, data)
	var s snapshotMsg
	expect(t, c, "state", &s)
	return c, s
}

func TestSnapshotInLobby(t *testing.T) {
	r := newRoom(t, nil)
	newPlayer(t, r, "alice")

	_, s := rejoin(t, r, RoleControl, nil)
	if s.State != stateLobby || len(s.Lobby.Players) != 1 || s.Configured != nil || s.Track != nil || s.CanBuzz != nil {
		t.Fatalf("lobby snapshot = %+v", s)
	}
	ctrl := newControl(t, r)
	send(r, ctrl, "configure", obj{"pack": "basic"})
	expect(t, ctrl, "configured", nil)
	if _, s = rejoin(t, r, RoleHost, nil); s.Configured == nil || s.Configured.Pack != "basic" {
		t.Fatalf("configured snapshot = %+v", s)
	}
}

func TestSnapshotDuringGame(t *testing.T) {
	m := basicManifest(2)
	m["rounds"] = list{obj{"name": "Manche 1", "selection": "sequence", "tracks": list{"t1", "t2"}}}
	r := newRoom(t, m)
	alice := newPlayer(t, r, "alice")
	bob := join(r, RolePlayer, nil)
	var w struct {
		Token string `json:"token"`
	}
	expect(t, bob, "welcome", &w)
	send(r, bob, "identify", obj{"name": "bob"})
	expect(t, bob, "lobby-update", nil)
	ctrl, _ := startGame(t, r, alice, bob)

	// alice holds the hand and control pauses: a reloaded host screen gets the whole picture.
	send(r, alice, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "pause", nil)
	expect(t, ctrl, "game-paused", nil)
	_, s := rejoin(t, r, RoleHost, nil)
	if s.State != statePaused || s.Paused == nil || s.Paused.Reason != "control" || s.Round == nil || s.Round.Name != "Manche 1" ||
		s.Track == nil || s.Track.Media != "/media/a.mp3" || s.TrackState != trackLive || s.Holder == nil || s.Holder.Name != "alice" {
		t.Fatalf("paused snapshot = %+v", s)
	}
	if s.Elapsed <= 0 || s.Elapsed > 1 {
		t.Fatalf("elapsed = %v, want the short time played before the buzz", s.Elapsed)
	}
	if s.CanBuzz != nil {
		t.Fatal("canBuzz is only sent to players")
	}

	// Control sees the answers; bob, reconnecting while alice holds the hand, cannot buzz.
	_, s = rejoin(t, r, RoleControl, nil)
	var guesses []struct{ Answers []string }
	json.Unmarshal(s.Track.Guesses, &guesses)
	if len(guesses) != 1 || guesses[0].Answers[0] != "x" {
		t.Fatalf("control snapshot guesses = %s", s.Track.Guesses)
	}
	bob, s = rejoin(t, r, RolePlayer, obj{"token": w.Token})
	if s.CanBuzz == nil || *s.CanBuzz {
		t.Fatalf("bob canBuzz = %v, want false while alice answers", s.CanBuzz)
	}

	// After alice's wrong answer, bob may buzz but alice may not.
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)
	send(r, ctrl, "validate", obj{"correct": false})
	expect(t, ctrl, "buzz-available", nil)
	if bob, s = rejoin(t, r, RolePlayer, obj{"token": w.Token}); !*s.CanBuzz || s.Elapsed <= 0 {
		t.Fatalf("bob snapshot = %+v, want canBuzz and a running clock", s)
	}

	// Once the track is found, the snapshot carries the answers.
	send(r, bob, "buzz", nil)
	expect(t, ctrl, "buzz-accepted", nil)
	send(r, ctrl, "validate", obj{"correct": true})
	expect(t, ctrl, "track-end", nil)
	_, s = rejoin(t, r, RoleControl, nil)
	if s.TrackState != trackEnded || s.TrackEnd == nil || s.TrackEnd.Reason != "found" || len(s.Found) != 1 || s.Found[0] != "Titre" {
		t.Fatalf("ended snapshot = %+v", s)
	}
	if len(s.Scores) != 2 || s.Scores[0].Name != "bob" || s.Scores[0].Score == 0 {
		t.Fatalf("scores = %+v, want bob first with points", s.Scores)
	}
}
