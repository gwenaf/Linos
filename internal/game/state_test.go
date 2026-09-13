package game

import (
	"reflect"
	"testing"
)

type gamePaused struct {
	Reason         string   `json:"reason"`
	HostMissing    bool     `json:"hostMissing"`
	MissingPlayers []string `json:"missingPlayers"`
}

type gameEnd struct {
	Reason  string   `json:"reason"`
	Results []result `json:"results"`
}

func TestGameLifecycle(t *testing.T) {
	r := newRoom(t, nil)
	ctrl := newControl(t, r)
	alice := newPlayer(t, r, "alice")

	send(r, alice, "buzz", nil)
	expectError(t, alice, "wrong-state")
	send(r, ctrl, "start-game", nil)
	expectError(t, ctrl, "wrong-state")
	send(r, alice, "ready", map[string]bool{"ready": true})
	expectState(t, ctrl, stateReady)
	send(r, ctrl, "start-game", nil)
	expectError(t, ctrl, "no-pack")
	send(r, ctrl, "configure", obj{"pack": "basic"})
	expect(t, ctrl, "configured", nil)
	send(r, ctrl, "start-game", nil)
	expectError(t, ctrl, "no-host")

	host := join(r, RoleHost, nil)
	expect(t, host, "welcome", nil)
	send(r, ctrl, "score-adjust", map[string]any{"name": "alice", "delta": 10})
	send(r, ctrl, "start-game", nil)
	expect(t, ctrl, "game-start", nil)
	send(r, alice, "join-team", map[string]string{"team": "late"})
	expectError(t, alice, "wrong-state")

	var paused gamePaused
	send(r, ctrl, "pause", nil)
	expect(t, ctrl, "game-paused", &paused)
	if paused.Reason != "control" {
		t.Fatalf("pause reason = %q, want control", paused.Reason)
	}
	send(r, alice, "buzz", nil)
	expectError(t, alice, "wrong-state")
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)

	send(r, ctrl, "end-game", nil)
	var end gameEnd
	expect(t, ctrl, "game-end", &end)
	if end.Reason != "ended" || !reflect.DeepEqual(end.Results, []result{{Name: "alice"}}) {
		t.Fatalf("game-end = %+v, want ended with alice reset to 0", end)
	}
	var lobby lobbyUpdate
	expect(t, ctrl, "lobby-update", &lobby)
	if lobby.State != stateLobby || lobby.Players[0].Ready {
		t.Fatalf("lobby = %+v, want back to lobby with alice not ready", lobby)
	}

	send(r, alice, "ready", map[string]bool{"ready": true})
	expectState(t, ctrl, stateReady)
	send(r, ctrl, "start-game", nil)
	expect(t, ctrl, "game-start", nil)
	send(r, ctrl, "abort", nil)
	expect(t, ctrl, "game-end", &end)
	if end.Reason != "aborted" {
		t.Fatalf("reason = %q, want aborted", end.Reason)
	}
	expectState(t, ctrl, stateLobby)
}

func TestTechnicalPauseHost(t *testing.T) {
	r := newRoom(t, nil)
	alice := newPlayer(t, r, "alice")
	ctrl, host := startGame(t, r, alice)

	var paused gamePaused
	r.Disconnect(host.Client)
	expect(t, ctrl, "game-paused", &paused)
	if !reflect.DeepEqual(paused, gamePaused{"technical", true, []string{}}) {
		t.Fatalf("paused = %+v, want technical with host missing", paused)
	}
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-paused", &paused)
	if !paused.HostMissing {
		t.Fatal("resume without host must stay in technical pause")
	}
	host = join(r, RoleHost, nil)
	expect(t, ctrl, "game-resumed", nil)

	// A host lost during a control pause turns the resume into a technical pause.
	send(r, ctrl, "pause", nil)
	expect(t, ctrl, "game-paused", nil)
	r.Disconnect(host.Client)
	sync(t, r, ctrl)
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-paused", &paused)
	if paused.Reason != "technical" || !paused.HostMissing {
		t.Fatalf("paused = %+v, want technical with host missing", paused)
	}
}

func TestTechnicalPausePlayer(t *testing.T) {
	r := newRoom(t, nil)
	alice := newPlayer(t, r, "alice")
	bob := join(r, RolePlayer, nil)
	var w struct {
		Token string `json:"token"`
	}
	expect(t, bob, "welcome", &w)
	send(r, bob, "identify", map[string]string{"name": "bob"})
	expect(t, bob, "lobby-update", nil)
	ctrl, _ := startGame(t, r, alice, bob)

	var paused gamePaused
	r.Disconnect(alice.Client)
	expect(t, ctrl, "game-paused", &paused)
	if !reflect.DeepEqual(paused.MissingPlayers, []string{"alice"}) || paused.HostMissing {
		t.Fatalf("paused = %+v, want alice missing", paused)
	}
	// Any connection change during the pause refreshes the missing list.
	newControl(t, r)
	expect(t, ctrl, "game-paused", nil)

	// Kicking the missing player resumes the game.
	send(r, ctrl, "kick", map[string]string{"name": "alice"})
	expect(t, ctrl, "kicked", nil)
	expect(t, ctrl, "game-resumed", nil)

	// Resuming without kick tolerates bob until he comes back.
	r.Disconnect(bob.Client)
	expect(t, ctrl, "game-paused", nil)
	send(r, ctrl, "resume", nil)
	expect(t, ctrl, "game-resumed", nil)

	bob = join(r, RolePlayer, map[string]string{"token": w.Token})
	expect(t, bob, "welcome", nil)
	r.Disconnect(bob.Client)
	expect(t, ctrl, "game-paused", &paused)
	if !reflect.DeepEqual(paused.MissingPlayers, []string{"bob"}) {
		t.Fatalf("paused = %+v, want bob missing again after reconnecting", paused)
	}
}
