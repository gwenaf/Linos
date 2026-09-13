package game

import (
	"fmt"
	"strings"
	"testing"
)

type lobbyUpdate struct {
	Players []struct {
		Name      string `json:"name"`
		Team      string `json:"team"`
		Ready     bool   `json:"ready"`
		Connected bool   `json:"connected"`
	} `json:"players"`
	Teams []string `json:"teams"`
	State string   `json:"state"`
}

// awaitLobby reads lobby updates until one matches, skipping those sent before the awaited change.
func awaitLobby(t *testing.T, c *testClient, match func(lobbyUpdate) bool) lobbyUpdate {
	t.Helper()
	for {
		var lobby lobbyUpdate
		expect(t, c, "lobby-update", &lobby)
		if match(lobby) {
			return lobby
		}
	}
}

func teamsAre(names ...string) func(lobbyUpdate) bool {
	return func(l lobbyUpdate) bool { return strings.Join(l.Teams, ",") == strings.Join(names, ",") }
}

func TestReconnectWithToken(t *testing.T) {
	r := newRoom(t, nil)
	type welcome struct {
		Token string `json:"token"`
		Name  string `json:"name"`
	}

	c := join(r, RolePlayer, nil)
	var first welcome
	expect(t, c, "welcome", &first)
	send(r, c, "identify", map[string]string{"name": "alice"})
	expect(t, c, "lobby-update", nil)
	r.Disconnect(c.Client)

	c2 := join(r, RolePlayer, map[string]string{"token": first.Token})
	var again welcome
	expect(t, c2, "welcome", &again)
	if again.Token != first.Token || again.Name != "alice" {
		t.Fatalf("reconnect = %+v, want token kept and name alice", again)
	}

	c3 := join(r, RolePlayer, map[string]string{"token": "forged"})
	var forged welcome
	expect(t, c3, "welcome", &forged)
	if forged.Token == "forged" || forged.Token == first.Token || forged.Name != "" {
		t.Fatalf("unknown token must create a new player, got %+v", forged)
	}
}

func TestIdentifyErrors(t *testing.T) {
	r := newRoom(t, nil)
	newPlayer(t, r, "alice")
	bob := join(r, RolePlayer, nil)
	expect(t, bob, "welcome", nil)

	send(r, bob, "identify", "x")
	expectError(t, bob, "bad-data")
	send(r, bob, "identify", map[string]string{"name": "   "})
	expectError(t, bob, "invalid-name")
	send(r, bob, "identify", map[string]string{"name": strings.Repeat("a", 33)})
	expectError(t, bob, "invalid-name")
	send(r, bob, "identify", map[string]string{"name": "ALICE"})
	expectError(t, bob, "name-taken")
}

func TestTeams(t *testing.T) {
	r := newRoom(t, nil)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl := newControl(t, r)

	send(r, alice, "join-team", "x")
	expectError(t, alice, "bad-data")
	send(r, alice, "join-team", map[string]string{"team": strings.Repeat("t", 33)})
	expectError(t, alice, "invalid-name")

	send(r, alice, "join-team", map[string]string{"team": "rouge"})
	send(r, bob, "join-team", map[string]string{"team": "ROUGE"})
	lobby := awaitLobby(t, ctrl, func(l lobbyUpdate) bool { return l.Players[1].Team != "" })
	if len(lobby.Teams) != 1 || lobby.Players[0].Team != "rouge" || lobby.Players[1].Team != "rouge" {
		t.Fatalf("lobby = %+v, want alice and bob in rouge", lobby)
	}

	// A team keeps existing while it has members or points.
	send(r, alice, "join-team", map[string]string{"team": ""})
	lobby = awaitLobby(t, ctrl, func(l lobbyUpdate) bool { return l.Players[0].Team == "" })
	if len(lobby.Teams) != 1 {
		t.Fatalf("teams = %v, want rouge kept for bob", lobby.Teams)
	}
	send(r, ctrl, "score-adjust", map[string]any{"team": "rouge", "delta": 5})
	send(r, bob, "join-team", map[string]string{"team": "bleu"})
	send(r, alice, "join-team", map[string]string{"team": "bleu"})
	awaitLobby(t, ctrl, teamsAre("rouge", "bleu"))
	send(r, bob, "join-team", map[string]string{"team": ""})
	lobby = awaitLobby(t, ctrl, func(l lobbyUpdate) bool { return l.Players[1].Team == "" })
	if strings.Join(lobby.Teams, ",") != "rouge,bleu" {
		t.Fatalf("teams = %v, want rouge (points) and bleu (alice)", lobby.Teams)
	}
	send(r, alice, "join-team", map[string]string{"team": ""})
	awaitLobby(t, ctrl, teamsAre("rouge"))

	// Teams with points survive once left, so alice can create them one after another.
	for i := range maxTeams - 1 {
		name := fmt.Sprintf("team%d", i)
		send(r, alice, "join-team", map[string]string{"team": name})
		send(r, ctrl, "score-adjust", map[string]any{"team": name, "delta": 1})
	}
	sync(t, r, alice)
	send(r, bob, "join-team", map[string]string{"team": "one-too-many"})
	expectError(t, bob, "too-many-teams")
}

func TestReady(t *testing.T) {
	r := newRoom(t, nil)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	anon := join(r, RolePlayer, nil)
	expect(t, anon, "welcome", nil)

	send(r, alice, "ready", "x")
	expectError(t, alice, "bad-data")
	send(r, alice, "ready", nil)
	expectError(t, alice, "bad-data")
	send(r, anon, "ready", map[string]bool{"ready": true})
	expectError(t, anon, "not-identified")

	send(r, alice, "ready", map[string]bool{"ready": true})
	var lobby lobbyUpdate
	expect(t, alice, "lobby-update", &lobby)
	if lobby.State != stateLobby || !lobby.Players[0].Ready {
		t.Fatalf("lobby = %+v, want alice ready, room still in lobby", lobby)
	}
	send(r, bob, "ready", map[string]bool{"ready": true})
	expectState(t, alice, stateReady)
	send(r, bob, "ready", map[string]bool{"ready": false})
	expectState(t, alice, stateLobby)

	// Disconnected players do not block readiness.
	r.Disconnect(bob.Client)
	expect(t, alice, "lobby-update", &lobby)
	if lobby.State != stateReady || lobby.Players[1].Connected {
		t.Fatalf("lobby = %+v, want ready with bob disconnected", lobby)
	}
}

func TestKickInLobby(t *testing.T) {
	r := newRoom(t, nil)
	alice := newPlayer(t, r, "alice")
	bob := newPlayer(t, r, "bob")
	ctrl := newControl(t, r)

	send(r, ctrl, "kick", "x")
	expectError(t, ctrl, "bad-data")
	send(r, ctrl, "kick", map[string]string{"name": "nobody"})
	expectError(t, ctrl, "unknown-player")

	send(r, ctrl, "kick", map[string]string{"name": "alice"})
	var kicked struct {
		Name string `json:"name"`
	}
	expect(t, alice, "kicked", &kicked)
	if kicked.Name != "alice" {
		t.Fatalf("kicked = %q, want alice", kicked.Name)
	}
	expectClosed(t, alice)

	r.Disconnect(bob.Client)
	sync(t, r, ctrl)
	send(r, ctrl, "kick", map[string]string{"name": "bob"})
	var lobby lobbyUpdate
	expect(t, ctrl, "kicked", nil)
	expect(t, ctrl, "lobby-update", &lobby)
	if len(lobby.Players) != 0 {
		t.Fatalf("players = %+v, want none", lobby.Players)
	}
}
