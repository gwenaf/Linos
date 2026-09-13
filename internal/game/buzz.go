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
	if refusal := r.buzzRefusal(u); refusal != "" {
		r.sendError(c, refusal, refusals[refusal])
		return
	}
	if !r.buzzOpen || slices.ContainsFunc(r.candidates, func(q *player) bool { return unit(q) == u }) {
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
	wu := unit(winner)
	r.tries[wu]++
	if r.reboundBlocked != wu {
		// Another team took the hand: the team that failed before may buzz again.
		r.reboundBlocked = nil
	}
	r.turn++
	// Candidates tied inside the window share the first buzz time; the clock stops while the holder answers.
	r.holderElapsed = r.firstBuzz.Sub(r.trackStart) - r.paused
	if r.rules.pauseOnBuzz {
		r.stopClock()
	}

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
	u := unit(p)
	r.holder = nil
	r.turn++
	r.releaseHolderClock()

	points := r.pointsFor(u, g, correct, r.holderElapsed)
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
	case correct && len(r.found) == len(r.track().Guesses):
		r.endTrack("found")
	case correct:
		// A new guess is up for grabs: everyone may buzz again.
		r.newGuessCycle()
		r.openBuzz()
	default:
		r.wrongs++
		r.lock(u)
		switch r.rules.rebound {
		case "none":
			r.endTrack("missed")
			return
		case "others":
			r.reboundBlocked = u
		}
		r.openBuzz()
	}
}

var refusals = map[string]string{
	"not-allowed":     "you cannot answer this track now",
	"no-attempt-left": "no attempt left on this guess",
	"rebound":         "another team gets the hand after your mistake",
	"locked":          "wait for your lockout to end",
}

// buzzRefusal gives the reason unit u may not buzz now, or "" when it may.
func (r *Room) buzzRefusal(u any) string {
	switch {
	case !r.mayPlay(u):
		return "not-allowed"
	case r.tries[u] >= r.rules.attempts:
		return "no-attempt-left"
	case u == r.reboundBlocked:
		return "rebound"
	case r.locked(u):
		return "locked"
	}
	return ""
}

// newGuessCycle clears attempts, rebound and lockouts when a guess opens.
func (r *Room) newGuessCycle() {
	r.tries = map[any]int{}
	r.reboundBlocked = nil
	r.lockedUntil = map[any]time.Duration{}
	r.wrongs = 0
}

// releaseHolderClock restarts the clock stopped by a buzz, when the rules stop it.
func (r *Room) releaseHolderClock() {
	if r.rules.pauseOnBuzz {
		r.startClock()
	}
}

func (r *Room) locked(u any) bool {
	until, ok := r.lockedUntil[u]
	return ok && r.elapsed() < until
}

// lock keeps unit u from answering for the wrong-answer lockout, counted in track time played.
func (r *Room) lock(u any) {
	if r.rules.wrongLockout <= 0 {
		return
	}
	r.lockedUntil[u] = r.elapsed() + r.rules.wrongLockout
	l := unitPayload(u)
	l["duration"] = r.rules.wrongLockout.Seconds()
	r.broadcast(NewMessage("lockout", l))
	r.armLockout(u)
}

func (r *Room) armLockout(u any) {
	seq := r.trackSeq
	time.AfterFunc(r.lockedUntil[u]-r.elapsed(), func() { r.events <- event{kind: evLockout, turn: seq, unit: u} })
}

// lockoutTimer lifts a lockout once played, and tells the unit it may buzz again.
func (r *Room) lockoutTimer(seq int, u any) {
	until, ok := r.lockedUntil[u]
	if seq != r.trackSeq || !ok || r.trackState != trackLive || r.clockStops > 0 {
		return
	}
	if r.elapsed() < until {
		r.armLockout(u)
		return
	}
	delete(r.lockedUntil, u)
	if r.buzzOpen && r.state == stateInProgress && r.buzzRefusal(u) == "" {
		r.sendToUnit(u, NewMessage("buzz-available", nil))
	}
}

func (r *Room) sendToUnit(u any, m Message) {
	for c := range r.clients {
		if c.player != nil && unit(c.player) == u {
			r.send(c, m)
		}
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
		if c.player == nil || r.buzzRefusal(unit(c.player)) == "" {
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
			for u := range r.lockedUntil {
				r.armLockout(u)
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
