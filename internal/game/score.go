package game

import (
	"math"
	"slices"
	"time"
)

// Manifest defaults until pack rules are loaded.
var scoring = struct {
	max, min     int
	duration     time.Duration
	wrongPenalty int
}{max: 100, min: 20, duration: 30 * time.Second}

// speedPoints decreases linearly from hi at 0 to lo at duration, then stays at lo.
func speedPoints(hi, lo int, duration, elapsed time.Duration) int {
	f := min(max(float64(elapsed)/float64(duration), 0), 1)
	return hi - int(math.Round(float64(hi-lo)*f))
}

func (r *Room) addPoints(p *player, points int) {
	if p.team != nil {
		p.team.score += points
		r.broadcastTeamScore(p.team)
		return
	}
	p.score += points
	r.broadcast(NewMessage("score-update", map[string]any{"name": p.name, "score": p.score}))
}

func (r *Room) broadcastTeamScore(t *team) {
	r.broadcast(NewMessage("score-update", map[string]any{"team": t.name, "score": t.score}))
}

func (r *Room) scoreAdjust(c *Client, m Message) {
	var d struct {
		Name  string `json:"name"`
		Team  string `json:"team"`
		Delta int    `json:"delta"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	if d.Team != "" {
		t := r.teamByName(d.Team)
		if t == nil {
			r.sendError(c, "unknown-team", "no team named "+d.Team)
			return
		}
		t.score += d.Delta
		r.broadcastTeamScore(t)
		return
	}
	p := r.playerByName(d.Name)
	if p == nil {
		r.sendError(c, "unknown-player", "no player named "+d.Name)
		return
	}
	r.addPoints(p, d.Delta)
}

type result struct {
	Name  string `json:"name,omitempty"`
	Team  string `json:"team,omitempty"`
	Score int    `json:"score"`
}

func (r *Room) results() []result {
	res := []result{}
	for _, t := range r.teams {
		res = append(res, result{Team: t.name, Score: t.score})
	}
	for _, p := range r.players {
		if p.name != "" && p.team == nil {
			res = append(res, result{Name: p.name, Score: p.score})
		}
	}
	slices.SortStableFunc(res, func(a, b result) int { return b.Score - a.Score })
	return res
}
