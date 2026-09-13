package game

const (
	stateLobby          = "lobby"
	stateInProgress     = "in-progress"
	statePaused         = "paused"
	stateTechnicalPause = "technical-pause"
	stateEnded          = "game-ended"
	stateAborted        = "game-aborted"
)

func (r *Room) startGame(c *Client) {
	if !r.hostConnected() {
		r.sendError(c, "no-host", "open the host screen first")
		return
	}
	r.state = stateInProgress
	r.resetTrack()
	r.broadcast(NewMessage("game-start", nil))
	r.openBuzz()
}

// pause freezes the game: pending buzzes are discarded, the holder keeps the hand and the clock stops.
func (r *Room) pause(state string) {
	r.state = state
	r.turn++
	r.candidates = nil
	r.stopClock()
	reason := "control"
	if state == stateTechnicalPause {
		reason = "technical"
	}
	r.broadcast(NewMessage("game-paused", map[string]string{"reason": reason}))
}

func (r *Room) resume() {
	if !r.hostConnected() {
		if r.state != stateTechnicalPause {
			r.state = stateTechnicalPause
			r.broadcast(NewMessage("game-paused", map[string]string{"reason": "technical"}))
		}
		return
	}
	r.state = stateInProgress
	r.startClock()
	r.broadcast(NewMessage("game-resumed", nil))
	switch {
	case r.holder != nil:
		// ponytail: the holder gets a full answer time again after a pause.
		r.armAnswerTimer()
	case r.buzzOpen:
		r.openBuzz()
	}
}

func (r *Room) endGame(state string) {
	r.state = state
	r.turn++
	r.holder = nil
	r.candidates = nil
	r.buzzOpen = false
	reason := "ended"
	if state == stateAborted {
		reason = "aborted"
	}
	r.broadcast(NewMessage("game-end", map[string]any{"reason": reason, "results": r.results()}))
}

func (r *Room) hostConnected() bool {
	for c := range r.clients {
		if c.role == RoleHost {
			return true
		}
	}
	return false
}
