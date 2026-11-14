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
		Captain   bool   `json:"captain"`
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
	settle(t, r, alice)
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
	settle(t, r, ctrl)
	send(r, ctrl, "kick", map[string]string{"name": "bob"})
	var lobby lobbyUpdate
	expect(t, ctrl, "kicked", nil)
	expect(t, ctrl, "lobby-update", &lobby)
	if len(lobby.Players) != 0 {
		t.Fatalf("players = %+v, want none", lobby.Players)
	}
}

// Tricksters: names nobody can read are refused, hidden control characters dropped.
func TestUnreadableNames(t *testing.T) {
	r := newRoom(t, nil)
	c := join(r, RolePlayer, nil)
	expect(t, c, "welcome", nil)
	for _, name := range []string{"​", "ㅤㅤ", "⠀", " ‮ "} {
		send(r, c, "identify", map[string]string{"name": name})
		expectError(t, c, "invalid-name")
	}
	send(r, c, "identify", map[string]string{"name": "‮bob\n"})
	awaitLobby(t, c, func(l lobbyUpdate) bool { return len(l.Players) == 1 && l.Players[0].Name == "bob" })
}

// A connection that leaves without a name leaves no player behind: its token is forgotten.
func TestUnnamedPlayersAreForgotten(t *testing.T) {
	r := newRoom(t, nil)
	var w struct {
		Token string `json:"token"`
	}
	c := join(r, RolePlayer, nil)
	expect(t, c, "welcome", &w)
	r.Disconnect(c.Client)
	again := join(r, RolePlayer, map[string]string{"token": w.Token})
	var w2 struct {
		Token string `json:"token"`
	}
	expect(t, again, "welcome", &w2)
	if w2.Token == w.Token {
		t.Fatal("an unnamed player must be forgotten when it leaves")
	}
	// A named one is kept, to come back.
	send(r, again, "identify", map[string]string{"name": "carol"})
	expect(t, again, "lobby-update", nil)
	r.Disconnect(again.Client)
	back := join(r, RolePlayer, map[string]string{"token": w2.Token})
	var w3 struct {
		Name string `json:"name"`
	}
	expect(t, back, "welcome", &w3)
	if w3.Name != "carol" {
		t.Fatalf("named player lost on reconnect: %+v", w3)
	}
}

// Someone joining a running game then closing the page does not pause it; the next game counts them.
func TestLateJoinerLeavingDoesNotPause(t *testing.T) {
	r := newRoom(t, nil)
	alice := newPlayer(t, r, "alice")
	ctrl, _ := startGame(t, r, alice)
	late, token := newPlayerWithToken(t, r, "mallory")
	r.Disconnect(late.Client)
	expectNo(t, r, ctrl, "game-paused")

	send(r, ctrl, "end-game", nil)
	expect(t, ctrl, "game-end", nil)
	late = join(r, RolePlayer, map[string]string{"token": token})
	expect(t, late, "welcome", nil)
	startGame(t, r, alice, late)
	r.Disconnect(late.Client)
	var paused gamePaused
	expect(t, ctrl, "game-paused", &paused)
	if fmt.Sprint(paused.MissingPlayers) != "[mallory]" {
		t.Fatalf("paused = %+v, want mallory missing once she played from the start", paused)
	}
}

// The gamemaster sets the teams up: created teams stay when empty and are the only ones players may
// join; players are moved, captains named, teams deleted.
func TestTeamsByGamemaster(t *testing.T) {
	r := newRoom(t, nil)
	ctrl := newControl(t, r)
	alice := newPlayer(t, r, "alice")
	newPlayer(t, r, "bob")

	send(r, ctrl, "create-team", obj{"team": "Rouges"})
	send(r, ctrl, "create-team", obj{"team": "Bleus"})
	send(r, ctrl, "create-team", obj{"team": "rouges"}) // already there
	awaitLobby(t, ctrl, teamsAre("Rouges", "Bleus"))
	send(r, alice, "join-team", obj{"team": "Verts"})
	expectError(t, alice, "teams-fixed")
	send(r, alice, "join-team", obj{"team": "rouges"})
	send(r, ctrl, "move-player", obj{"name": "bob", "team": "Rouges"})
	send(r, ctrl, "set-captain", obj{"name": "bob"})
	l := awaitLobby(t, ctrl, func(l lobbyUpdate) bool { return len(l.Players) == 2 && l.Players[1].Captain })
	if l.Players[0].Team != "Rouges" || l.Players[0].Captain {
		t.Fatalf("lobby = %+v, want both in Rouges, bob captain", l)
	}
	// Leaving a created team keeps it, even empty.
	send(r, ctrl, "move-player", obj{"name": "alice", "team": ""})
	send(r, ctrl, "move-player", obj{"name": "bob", "team": "Bleus"})
	awaitLobby(t, ctrl, func(l lobbyUpdate) bool { return l.Players[1].Team == "Bleus" && len(l.Teams) == 2 })

	for _, bad := range []struct {
		typ  string
		data obj
		code string
	}{
		{"create-team", obj{"team": " "}, "invalid-name"},
		{"create-team", obj{"team": 1}, "bad-data"},
		{"delete-team", obj{"team": "Jaunes"}, "unknown-team"},
		{"delete-team", obj{"team": 1}, "bad-data"},
		{"move-player", obj{"name": "zed", "team": "Bleus"}, "unknown-player"},
		{"move-player", obj{"name": "bob", "team": "Jaunes"}, "unknown-team"},
		{"move-player", obj{"name": 1}, "bad-data"},
		{"set-captain", obj{"name": "zed"}, "unknown-player"},
		{"set-captain", obj{"name": "alice"}, "no-team"},
		{"set-captain", obj{"name": 1}, "bad-data"},
	} {
		send(r, ctrl, bad.typ, bad.data)
		expectError(t, ctrl, bad.code)
	}

	// Deleting a team leaves its members without one; with no created team left, players create theirs.
	send(r, ctrl, "delete-team", obj{"team": "Bleus"})
	send(r, ctrl, "delete-team", obj{"team": "Rouges"})
	awaitLobby(t, ctrl, func(l lobbyUpdate) bool { return len(l.Teams) == 0 && l.Players[1].Team == "" })
	send(r, alice, "join-team", obj{"team": "Verts"})
	awaitLobby(t, ctrl, teamsAre("Verts"))
	for i := range maxTeams - 1 {
		send(r, ctrl, "create-team", obj{"team": fmt.Sprintf("t%d", i)})
	}
	send(r, ctrl, "create-team", obj{"team": "one too many"})
	expectError(t, ctrl, "too-many-teams")
}
