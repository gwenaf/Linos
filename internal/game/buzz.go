package game

import (
	mrand "math/rand/v2"
	"slices"
	"time"
)

const buzzWindow = 30 * time.Millisecond

func (r *Room) buzz(c *Client, at time.Time) {
	p := c.player
	if p.name == "" {
		r.sendError(c, "not-identified", "identify first")
		return
	}
	u := unit(p)
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
	r.candidates = nil
	r.buzzOpen = false
	r.holder = winner
	r.attempted[unit(winner)] = true
	r.turn++
	// Candidates tied inside the window share the first buzz time; the clock stops while the holder answers.
	r.holderElapsed = r.firstBuzz.Sub(r.trackStart) - r.paused
	r.stopClock()

	accepted := map[string]any{"name": winner.name, "answerTime": r.answerTime.Seconds()}
	if winner.team != nil {
		accepted["team"] = winner.team.name
	}
	r.broadcast(NewMessage("buzz-accepted", accepted))
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
	time.AfterFunc(r.answerTime, func() { r.events <- event{kind: evAnswerTimeout, turn: turn} })
}

func (r *Room) validate(c *Client, m Message) {
	if r.holder == nil {
		r.sendError(c, "no-pending-answer", "no answer to validate")
		return
	}
	var d struct {
		Correct *bool `json:"correct"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	if d.Correct == nil {
		r.sendError(c, "bad-data", "validate requires correct")
		return
	}
	r.resolveAnswer(*d.Correct)
}

func (r *Room) resolveAnswer(correct bool) {
	p := r.holder
	r.holder = nil
	r.turn++
	r.startClock()

	points := -scoring.wrongPenalty
	if correct {
		points = speedPoints(scoring.max, scoring.min, scoring.duration, r.holderElapsed)
	}
	r.broadcast(NewMessage("answer-result", map[string]any{"name": p.name, "correct": correct, "points": points}))
	if points != 0 {
		r.addPoints(p, points)
	}
	if !correct {
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
		if c.player == nil || !r.attempted[unit(c.player)] {
			r.send(c, available)
		}
	}
}

func (r *Room) resetTrack() {
	r.holder = nil
	r.candidates = nil
	r.attempted = map[any]bool{}
	r.turn++
	r.trackStart = time.Now()
	r.paused = 0
	r.clockStops = 0
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
	}
}
