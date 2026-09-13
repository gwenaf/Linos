package game

import (
	"log/slog"
	"time"

	"github.com/gwenaf/linos/internal/pack"
)

type scored struct {
	player *player
	points int
}

// answer handles an answer typed or picked on a phone: by the buzz holder, or by everyone in simultaneous mode.
func (r *Room) answer(c *Client, m Message, at time.Time) {
	var d struct {
		Guess string `json:"guess"`
		Value string `json:"value"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	p := c.player
	switch {
	case p.name == "":
		r.sendError(c, "not-identified", "identify first")
		return
	case r.trackState != trackLive:
		r.sendError(c, "wrong-state", "no track is being played")
		return
	case r.rules.via != "device":
		r.sendError(c, "oral-answers", "answers are given aloud for this track")
		return
	}
	if r.rules.mode == "simultaneous" {
		r.simultaneousAnswer(c, d.Guess, d.Value, at)
		return
	}

	if r.holder != p || r.holderAnswered {
		r.sendError(c, "not-your-turn", "buzz first")
		return
	}
	g := r.pendingGuess(d.Guess)
	if g == nil {
		r.sendError(c, "unknown-guess", "no guess left named "+d.Guess)
		return
	}
	correct := matches(g, d.Value, r.rules.fuzziness)
	slog.Info("answer submitted", "player", p.name, "guess", g.Label, "value", d.Value, "correct", correct)
	if r.loaded.Load().control == "auto" {
		r.resolveAnswer(correct, g)
		return
	}
	// With a gamemaster, the computed verdict is only a suggestion: control validates.
	r.holderAnswered = true
	r.sendToControls(NewMessage("answer-submitted", submission(p, g, d.Value, correct)))
}

// simultaneousAnswer scores an answer right away but keeps the points hidden until the track ends,
// so players cannot tell who found it from the scores.
func (r *Room) simultaneousAnswer(c *Client, label, value string, at time.Time) {
	p := c.player
	u := unit(p)
	if !r.mayPlay(u) {
		r.sendError(c, "not-allowed", "you cannot answer this track now")
		return
	}
	g := r.unansweredGuess(u, label)
	if g == nil {
		r.sendError(c, "unknown-guess", "no guess left to answer named "+label)
		return
	}
	if r.answered[g.Label] == nil {
		r.answered[g.Label] = map[any]bool{}
	}
	r.answered[g.Label][u] = true

	correct := matches(g, value, r.rules.fuzziness)
	points := r.pointsFor(u, g, correct, at.Sub(r.trackStart)-r.paused)
	if correct {
		r.found[g.Label] = true
	}
	r.pendingPoints = append(r.pendingPoints, scored{p, points})
	slog.Info("answer submitted", "player", p.name, "guess", g.Label, "value", value, "correct", correct, "points", points)

	result := submission(p, g, value, correct)
	result["points"] = points
	msg := NewMessage("answer-result", result)
	for cl := range r.clients {
		if cl.role == RoleControl || cl.player != nil && unit(cl.player) == u {
			r.send(cl, msg)
		}
	}
	answered := map[string]any{"name": p.name, "guess": g.Label}
	if p.team != nil {
		answered["team"] = p.team.name
	}
	r.broadcast(NewMessage("answered", answered))

	if r.allAnswered() {
		r.endTrack("answered")
	}
}

func submission(p *player, g *pack.Guess, value string, correct bool) map[string]any {
	s := map[string]any{"name": p.name, "guess": g.Label, "value": value, "correct": correct}
	if p.team != nil {
		s["team"] = p.team.name
	}
	return s
}

// unansweredGuess returns the guess with that label, or the first one, that unit u has not answered yet.
func (r *Room) unansweredGuess(u any, label string) *pack.Guess {
	guesses := r.track().Guesses
	for i := range guesses {
		if (label == "" || guesses[i].Label == label) && !r.answered[guesses[i].Label][u] {
			return &guesses[i]
		}
	}
	return nil
}

// allAnswered reports whether every connected team or player in play has answered every guess.
func (r *Room) allAnswered() bool {
	units := r.playingUnits()
	for _, g := range r.track().Guesses {
		for _, u := range units {
			if !r.answered[g.Label][u] {
				return false
			}
		}
	}
	return true
}

func (r *Room) sendToControls(m Message) {
	for c := range r.clients {
		if c.role == RoleControl {
			r.send(c, m)
		}
	}
}
