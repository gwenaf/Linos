package game

import (
	"slices"
	"sort"
)

// snapshot is the full picture sent to a client on join, so a reloaded page resumes where the game is.
func (r *Room) snapshot(c *Client) map[string]any {
	s := map[string]any{
		"state":  r.state,
		"lobby":  r.lobbyPayload(),
		"scores": r.results(),
	}
	if r.loaded.Load() != nil {
		s["configured"] = r.configuredPayload()
	}
	if !slices.Contains(runningStates, r.state) {
		return s
	}

	if r.state != stateInProgress {
		s["paused"] = r.pausedPayload()
	}
	if r.playlist[r.current].round >= 0 {
		s["round"] = r.roundPayload()
	}
	s["eliminated"] = unitPayloads(sortedUnits(r.eliminated))
	if r.tiebreak != nil {
		s["tiebreak"] = unitPayloads(sortedUnits(r.tiebreak))
	}
	s["trackState"] = r.trackState
	if p := c.player; p != nil {
		u := unit(p)
		s["jokers"] = max(r.jokerLimit-r.jokersUsed[u], 0)
		s["doubled"] = r.doubled[u]
	}

	switch r.trackState {
	case trackPicking:
		s["pick"] = r.pickPayload()
		return s
	case trackWagering:
		s["wager"] = r.wagerPayload()
		return s
	}

	found := []string{}
	for label := range r.found {
		found = append(found, label)
	}
	sort.Strings(found)
	s["track"] = r.trackPayload(c.role)
	s["found"] = found
	switch r.trackState {
	case trackLive:
		s["elapsed"] = r.elapsed().Seconds()
	case trackEnded:
		s["trackEnd"] = r.trackEndPayload()
	}
	if r.holder != nil {
		s["holder"] = r.holderPayload()
	}
	if p := c.player; p != nil {
		u := unit(p)
		s["canBuzz"] = r.state == stateInProgress && r.buzzOpen && p.name != "" && r.buzzRefusal(u) == ""
		answered := []string{}
		for _, g := range r.track().Guesses {
			if r.answered[g.Label][u] {
				answered = append(answered, g.Label)
			}
		}
		s["answered"] = answered
		s["canAnswer"] = r.state == stateInProgress && r.trackState == trackLive && r.rules.via == "device" && r.mayPlay(u) &&
			(r.rules.mode == "simultaneous" || r.holder == p && !r.holderAnswered)
	}
	return s
}
