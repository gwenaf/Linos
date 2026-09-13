package game

import (
	"log/slog"
	mrand "math/rand/v2"
	"slices"
	"time"

	"github.com/gwenaf/linos/internal/pack"
)

const buzzWindow = 30 * time.Millisecond

func (r *Room) buzz(c *Client, at time.Time) {
	p := c.player
	if p.name == "" {
		r.sendError(c, "not-identified", "identify first")
		return
	}
	if r.rules.mode == "simultaneous" {
		r.sendError(c, "no-buzz", "everyone answers on their phone for this track")
		return
	}
	u := unit(p)
	if !r.mayPlay(u) {
		r.sendError(c, "not-allowed", "you cannot answer this track now")
		return
	}
	if !r.buzzOpen || r.attempted[u] || slices.ContainsFunc(r.candidates, func(q *player) bool { return unit(q) == u }) {
		return
	}
	if len(r.candidates) == 0 {
		r.firstBuzz = at
		turn := r.turn
		time.AfterFunc(buzzWindow, func() { r.events <- event{kind: evBuzzWindowClosed, turn: turn} })
	} else if at.Sub(r.firstBuzz) > buzzWindow {
		// ponytail: a buzz received in-window but queued after the window closed is dropped; sub-millisecond race.
		return
	}
	r.candidates = append(r.candidates, p)
}

func (r *Room) closeBuzzWindow() {
	if len(r.candidates) == 0 {
		return
	}
	winner := r.candidates[mrand.IntN(len(r.candidates))]
	slog.Info("buzz accepted", "player", winner.name, "candidates", len(r.candidates))
	r.candidates = nil
	r.buzzOpen = false
	r.holder = winner
	r.holderAnswered = false
	r.attempted[unit(winner)] = true
	r.turn++
	// Candidates tied inside the window share the first buzz time; the clock stops while the holder answers.
	r.holderElapsed = r.firstBuzz.Sub(r.trackStart) - r.paused
	r.stopClock()

	r.broadcast(NewMessage("buzz-accepted", r.holderPayload()))
	blocked := NewMessage("buzz-blocked", nil)
	for _, p := range r.players {
		if p != winner {
			r.send(p.client, blocked)
		}
	}
	r.armAnswerTimer()
}

func (r *Room) armAnswerTimer() {
	turn := r.turn
	time.AfterFunc(r.rules.answerTime, func() { r.events <- event{kind: evAnswerTimeout, turn: turn} })
}

func (r *Room) validate(c *Client, m Message) {
	if r.loaded.Load().control == "auto" {
		r.sendError(c, "auto-control", "answers are validated automatically in this game")
		return
	}
	if r.holder == nil {
		r.sendError(c, "no-pending-answer", "no answer to validate")
		return
	}
	var d struct {
		Correct *bool  `json:"correct"`
		Guess   string `json:"guess"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	if d.Correct == nil {
		r.sendError(c, "bad-data", "validate requires correct")
		return
	}
	g := r.pendingGuess(d.Guess)
	if *d.Correct && g == nil {
		r.sendError(c, "unknown-guess", "no guess left named "+d.Guess)
		return
	}
	r.resolveAnswer(*d.Correct, g)
}

// pendingGuess returns the guess not found yet with that label, or the first one left when the label is empty.
func (r *Room) pendingGuess(label string) *pack.Guess {
	guesses := r.track().Guesses
	for i := range guesses {
		if !r.found[guesses[i].Label] && (label == "" || guesses[i].Label == label) {
			return &guesses[i]
		}
	}
	return nil
}

func (r *Room) resolveAnswer(correct bool, g *pack.Guess) {
	p := r.holder
	r.holder = nil
	r.turn++
	r.startClock()

	points := r.pointsFor(unit(p), g, correct, r.holderElapsed)
	label := ""
	if correct {
		label = g.Label
		r.found[label] = true
	}
	slog.Info("answer resolved", "player", p.name, "guess", label, "correct", correct, "points", points)
	r.broadcast(NewMessage("answer-result", map[string]any{"name": p.name, "guess": label, "correct": correct, "points": points}))
	if points != 0 {
		r.addPoints(p, points)
	}

	switch {
	case !correct:
		r.openBuzz()
	case len(r.found) == len(r.track().Guesses):
		r.endTrack("found")
	default:
		// A new guess is up for grabs: everyone may buzz again.
		r.attempted = map[any]bool{}
		r.openBuzz()
	}
}

// openBuzz reopens the buzzer; players are only told while the game is running.
func (r *Room) openBuzz() {
	r.buzzOpen = true
	if r.state != stateInProgress {
		return
	}
	available := NewMessage("buzz-available", nil)
	for c := range r.clients {
		if c.player == nil || !r.attempted[unit(c.player)] && r.mayPlay(unit(c.player)) {
			r.send(c, available)
		}
	}
}

// stopClock and startClock nest: a holder answering during a game pause stops the clock once.
func (r *Room) stopClock() {
	if r.clockStops == 0 {
		r.pausedAt = time.Now()
	}
	r.clockStops++
}

func (r *Room) startClock() {
	r.clockStops--
	if r.clockStops == 0 {
		r.paused += time.Since(r.pausedAt)
		if r.trackState == trackLive {
			r.armTrackTimer()
			if !r.headStartOver {
				r.armHeadStart()
			}
		}
	}
}

func (r *Room) holderPayload() map[string]any {
	h := map[string]any{"name": r.holder.name, "answerTime": r.rules.answerTime.Seconds()}
	if r.holder.team != nil {
		h["team"] = r.holder.team.name
	}
	return h
}
