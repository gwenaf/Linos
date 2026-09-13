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
	found := []string{}
	for label := range r.found {
		found = append(found, label)
	}
	sort.Strings(found)
	s["track"] = r.trackPayload(c.role)
	s["trackState"] = r.trackState
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
		s["canBuzz"] = r.state == stateInProgress && r.buzzOpen && p.name != "" && !r.attempted[unit(p)]
	}
	return s
}
