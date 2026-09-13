package game

import "slices"

const (
	stateLobby          = "lobby"
	stateReady          = "ready"
	stateInProgress     = "in-progress"
	statePaused         = "paused"
	stateTechnicalPause = "technical-pause"
)

func (r *Room) startGame(c *Client) {
	if r.loaded.Load() == nil {
		r.sendError(c, "no-pack", "configure a pack first")
		return
	}
	if !r.hostConnected() {
		r.sendError(c, "no-host", "open the host screen first")
		return
	}
	playlist := r.buildPlaylist()
	if len(playlist) == 0 {
		r.sendError(c, "empty-playlist", "the rounds select no track")
		return
	}
	for _, t := range r.teams {
		t.score = 0
	}
	for _, p := range r.players {
		p.score = 0
		if p.client == nil {
			r.tolerated[p] = true
		}
	}
	r.state = stateInProgress
	r.playlist, r.current = playlist, -1
	r.broadcast(NewMessage("game-start", map[string]any{
		"title":  r.loaded.Load().pack.Manifest.Title,
		"tracks": len(playlist),
	}))
	r.nextTrack()
}

// freeze leaves in-progress: pending buzzes are discarded, the holder keeps the hand and the clock stops.
func (r *Room) freeze(state string) {
	r.state = state
	r.turn++
	r.candidates = nil
	r.stopClock()
}

// resume continues without the players still disconnected; they are tolerated until they reconnect.
func (r *Room) resume() {
	if !r.hostConnected() {
		r.state = stateTechnicalPause
		_, players := r.missing()
		r.broadcastTechnicalPause(true, players)
		return
	}
	for _, p := range r.players {
		if p.client == nil {
			r.tolerated[p] = true
		}
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

// connectionsChanged refreshes the lobby, or pauses and resumes a running game as the host or players come and go.
func (r *Room) connectionsChanged() {
	switch r.state {
	case stateLobby, stateReady:
		r.broadcastLobby()
	case stateInProgress, stateTechnicalPause:
		host, players := r.missing()
		switch {
		case host || len(players) > 0:
			if r.state == stateInProgress {
				r.freeze(stateTechnicalPause)
			}
			r.broadcastTechnicalPause(host, players)
		case r.state == stateTechnicalPause:
			r.resume()
		}
	}
}

func (r *Room) missing() (host bool, players []string) {
	players = []string{}
	for _, p := range r.players {
		if p.name != "" && p.client == nil && !r.tolerated[p] {
			players = append(players, p.name)
		}
	}
	slices.Sort(players)
	return !r.hostConnected(), players
}

func (r *Room) broadcastTechnicalPause(hostMissing bool, missingPlayers []string) {
	r.broadcast(NewMessage("game-paused", map[string]any{
		"reason":         "technical",
		"hostMissing":    hostMissing,
		"missingPlayers": missingPlayers,
	}))
}

// endGame publishes the results and returns everyone to the lobby, not ready.
func (r *Room) endGame(reason string) {
	r.broadcast(NewMessage("game-end", map[string]any{"reason": reason, "results": r.results()}))
	r.state = stateLobby
	r.turn++
	r.trackSeq++
	r.trackState = ""
	r.holder = nil
	r.candidates = nil
	r.buzzOpen = false
	r.tolerated = map[*player]bool{}
	for _, p := range r.players {
		p.ready = false
	}
	r.broadcastLobby()
}

func (r *Room) hostConnected() bool {
	for c := range r.clients {
		if c.role == RoleHost {
			return true
		}
	}
	return false
}
