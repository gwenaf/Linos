package game

import (
	"crypto/rand"
	"encoding/base64"
	"slices"
	"strings"
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

func (r *Room) joinPlayer(c *Client, m Message) bool {
	var d struct {
		Token string `json:"token"`
	}
	if !r.decode(c, m, &d) {
		return false
	}
	p := r.players[d.Token]
	if p == nil {
		p = &player{token: newToken()}
		r.players[p.token] = p
	}
	if c.player != nil && c.player != p && c.player.client == c {
		c.player.client = nil
	}
	if p.client != nil && p.client != c {
		r.drop(p.client)
	}
	p.client = c
	c.player = p
	return true
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
	old := c.player.team
	c.player.team = t
	if old != nil && old != t && old.score == 0 && !r.hasMembers(old) {
		r.teams = slices.DeleteFunc(r.teams, func(x *team) bool { return x == old })
	}
	r.broadcastLobby()
}

func (r *Room) broadcastLobby() {
	type entry struct {
		Name string `json:"name"`
		Team string `json:"team,omitempty"`
	}
	players := []entry{}
	for _, p := range r.players {
		if p.name == "" {
			continue
		}
		e := entry{Name: p.name}
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
	r.broadcast(NewMessage("lobby-update", map[string]any{"players": players, "teams": teams}))
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
