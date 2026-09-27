package game

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxNameLen = 32
	maxTeams   = 8
)

type player struct {
	token  string
	name   string
	score  int
	ready  bool
	team   *team
	client *Client
}

type team struct {
	name  string
	score int
}

// unit is who buzzes, gets excluded and scores: the team when the player has one, else the player.
func unit(p *player) any {
	if p.team != nil {
		return p.team
	}
	return p
}

func (r *Room) joinPlayer(c *Client, token string) {
	p := r.players[token]
	if p == nil {
		p = &player{token: newToken()}
		r.players[p.token] = p
	}
	if c.player != p {
		r.detach(c)
	}
	old := p.client
	p.client, c.player = c, p
	delete(r.tolerated, p)
	if old != nil && old != c {
		r.drop(old)
	}
}

func (r *Room) identify(c *Client, m Message) {
	var d struct {
		Name string `json:"name"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	name, ok := r.validName(c, d.Name)
	if !ok {
		return
	}
	for _, p := range r.players {
		if p != c.player && strings.EqualFold(p.name, name) {
			r.sendError(c, "name-taken", "name already taken")
			return
		}
	}
	c.player.name = name
	r.broadcastLobby()
}

func (r *Room) joinTeam(c *Client, m Message) {
	var d struct {
		Team string `json:"team"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	var t *team
	if strings.TrimSpace(d.Team) != "" {
		name, ok := r.validName(c, d.Team)
		if !ok {
			return
		}
		if t = r.teamByName(name); t == nil {
			if len(r.teams) >= maxTeams {
				r.sendError(c, "too-many-teams", "team limit reached")
				return
			}
			t = &team{name: name}
			r.teams = append(r.teams, t)
		}
	}
	r.setTeam(c.player, t)
	r.broadcastLobby()
}

// setTeam moves a player and deletes the team it left when that team is empty and has no points.
func (r *Room) setTeam(p *player, t *team) {
	old := p.team
	p.team = t
	if old != nil && old != t && old.score == 0 && !r.hasMembers(old) {
		r.teams = slices.DeleteFunc(r.teams, func(x *team) bool { return x == old })
	}
}

func (r *Room) setReady(c *Client, m Message) {
	var d struct {
		Ready *bool `json:"ready"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	if d.Ready == nil {
		r.sendError(c, "bad-data", "ready requires ready")
		return
	}
	if c.player.name == "" {
		r.sendError(c, "not-identified", "identify first")
		return
	}
	c.player.ready = *d.Ready
	r.broadcastLobby()
}

func (r *Room) kick(c *Client, m Message) {
	var d struct {
		Name string `json:"name"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	p := r.playerByName(d.Name)
	if p == nil {
		r.sendError(c, "unknown-player", "no player named "+d.Name)
		return
	}
	slog.Info("player kicked", "player", p.name)
	r.broadcast(NewMessage("kicked", map[string]string{"name": p.name}))
	u := unit(p)
	delete(r.players, p.token)
	delete(r.tolerated, p)
	r.setTeam(p, nil)
	r.candidates = slices.DeleteFunc(r.candidates, func(q *player) bool { return q == p })
	if r.holder == p {
		r.holder = nil
		r.turn++
		r.releaseHolderClock()
		r.openBuzz()
	}
	r.unitsChanged(u)
	if p.client != nil {
		r.drop(p.client)
		return
	}
	r.connectionsChanged()
}

// unitsChanged unblocks a theme pick or wagers waiting for a unit that just left the game.
func (r *Room) unitsChanged(left any) {
	switch {
	case r.trackState == trackPicking && r.picker == left && !slices.Contains(r.playingUnits(), left):
		r.picks--
		if !r.requestPick() {
			r.nextTrack()
		}
	case r.trackState == trackWagering:
		r.startIfAllWagered()
	}
}

// invite is a single-use join code: a gamemaster invite, or a player's reconnection invite.
type invite struct {
	expiry time.Time
	player *player
}

func (r *Room) createInvite(c *Client, typ string, p *player) {
	slog.Info("invite created", "type", typ)
	code := newToken()
	r.invites[code] = invite{time.Now().Add(r.inviteTTL), p}
	d := map[string]any{"code": code, "expiresIn": r.inviteTTL.Seconds()}
	if p != nil {
		d["name"] = p.name
	}
	r.send(c, NewMessage(typ, d))
}

// reconnectInvite lets a player whose phone lost its token take their place back.
func (r *Room) reconnectInvite(c *Client, m Message) {
	var d struct {
		Name string `json:"name"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	p := r.playerByName(d.Name)
	if p == nil {
		r.sendError(c, "unknown-player", "no player named "+d.Name)
		return
	}
	r.createInvite(c, "reconnect-invite", p)
}

// broadcastLobby sends players and teams; in the lobby it also switches between lobby and ready.
func (r *Room) broadcastLobby() {
	if slices.Contains(lobbyStates, r.state) {
		connected, allReady := 0, true
		for _, p := range r.players {
			if p.name != "" && p.client != nil {
				connected++
				allReady = allReady && p.ready
			}
		}
		r.state = stateLobby
		if connected > 0 && allReady {
			r.state = stateReady
		}
	}
	r.broadcast(NewMessage("lobby-update", r.lobbyPayload()))
}

func (r *Room) lobbyPayload() map[string]any {
	type entry struct {
		Name      string `json:"name"`
		Team      string `json:"team,omitempty"`
		Ready     bool   `json:"ready"`
		Connected bool   `json:"connected"`
	}
	players := []entry{}
	for _, p := range r.players {
		if p.name == "" {
			continue
		}
		e := entry{Name: p.name, Ready: p.ready, Connected: p.client != nil}
		if p.team != nil {
			e.Team = p.team.name
		}
		players = append(players, e)
	}
	slices.SortFunc(players, func(a, b entry) int { return strings.Compare(a.Name, b.Name) })
	teams := []string{}
	for _, t := range r.teams {
		teams = append(teams, t.name)
	}
	return map[string]any{"players": players, "teams": teams, "state": r.state}
}

func (r *Room) validName(c *Client, raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		r.sendError(c, "invalid-name", "name must be 1 to 32 characters")
		return "", false
	}
	return name, true
}

func (r *Room) teamByName(name string) *team {
	for _, t := range r.teams {
		if strings.EqualFold(t.name, name) {
			return t
		}
	}
	return nil
}

func (r *Room) playerByName(name string) *player {
	for _, p := range r.players {
		if p.name != "" && p.name == name {
			return p
		}
	}
	return nil
}

func (r *Room) hasMembers(t *team) bool {
	for _, p := range r.players {
		if p.team == t {
			return true
		}
	}
	return false
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
