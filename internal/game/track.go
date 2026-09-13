package game

import (
	"log/slog"
	"maps"
	mrand "math/rand/v2"
	"net/url"
	"slices"
	"time"

	"github.com/gwenaf/linos/internal/pack"
)

const (
	trackLoading = "loading"
	trackLive    = "live"
	trackEnded   = "ended"
)

// rules are the effective values for a track, after merging pack and round rules over the defaults.
type rules struct {
	duration     time.Duration
	answerTime   time.Duration
	mode         string // buzz or simultaneous
	via          string // oral or device
	fuzziness    float64
	scoringType  string
	max, min     int
	ranks        []int
	wrongPenalty int
}

var defaultRules = rules{
	duration:    30 * time.Second,
	answerTime:  10 * time.Second,
	mode:        "buzz",
	via:         "oral",
	fuzziness:   0.2,
	scoringType: "speed",
	ranks:       []int{100, 60, 30},
	max:         100,
	min:         20,
}

func (r rules) with(p *pack.Rules) rules {
	if p == nil {
		return r
	}
	if p.Duration != nil {
		r.duration = seconds(*p.Duration)
	}
	if a := p.Answer; a != nil {
		if a.AnswerTime != nil {
			r.answerTime = seconds(*a.AnswerTime)
		}
		if a.Mode != nil {
			r.mode = *a.Mode
		}
		if a.Via != nil {
			r.via = *a.Via
		}
		if a.Fuzziness != nil {
			r.fuzziness = *a.Fuzziness
		}
	}
	if p.Scoring != nil {
		r = r.withScoring(p.Scoring)
	}
	return r
}

func (r rules) withScoring(s *pack.Scoring) rules {
	if s.Type != nil {
		r.scoringType = *s.Type
	}
	if s.Max != nil {
		r.max = *s.Max
	}
	if s.Min != nil {
		r.min = *s.Min
	}
	if s.WrongPenalty != nil {
		r.wrongPenalty = *s.WrongPenalty
	}
	if len(s.Ranks) > 0 {
		r.ranks = s.Ranks
	}
	return r
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

type playItem struct {
	round int // -1 when the pack has no rounds
	track *pack.Track
	rules rules
}

func (r *Room) track() *pack.Track {
	return r.playlist[r.current].track
}

// buildPlaylist flattens the rounds into the ordered list of tracks to play.
func (r *Room) buildPlaylist() []playItem {
	m := &r.loaded.Load().pack.Manifest
	base := defaultRules.with(m.Rules)
	if len(m.Rounds) == 0 {
		items := make([]playItem, len(m.Tracks))
		for i := range m.Tracks {
			items[i] = playItem{-1, &m.Tracks[i], base}
		}
		return items
	}

	byID := map[string]*pack.Track{}
	for i := range m.Tracks {
		byID[m.Tracks[i].ID] = &m.Tracks[i]
	}
	used := map[string]bool{}
	var items []playItem
	for ri, rd := range m.Rounds {
		ids := rd.Tracks
		if rd.Selection == "random" {
			ids = randomTracks(m, rd, used)
		}
		rr := base.with(rd.Rules)
		for _, id := range ids {
			used[id] = true
			items = append(items, playItem{ri, byID[id], rr})
		}
	}
	return items
}

// randomTracks draws up to rd.Count tracks from the round themes, skipping tracks already played.
func randomTracks(m *pack.Manifest, rd pack.Round, used map[string]bool) []string {
	var ids []string
	for _, t := range m.Tracks {
		inTheme := slices.ContainsFunc(t.Themes, func(th string) bool { return slices.Contains(rd.Themes, th) })
		if inTheme && !used[t.ID] {
			ids = append(ids, t.ID)
		}
	}
	mrand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	return ids[:min(rd.Count, len(ids))]
}

func (r *Room) nextTrack() {
	r.current++
	if r.current >= len(r.playlist) {
		r.endGame("ended")
		return
	}
	it := r.playlist[r.current]
	if it.round >= 0 && (r.current == 0 || r.playlist[r.current-1].round != it.round) {
		r.broadcast(NewMessage("round-start", r.roundPayload()))
	}

	slog.Info("track started", "index", r.current, "track", it.track.ID)
	r.rules = it.rules
	r.trackSeq++
	r.trackState = trackLoading
	r.found = map[string]bool{}
	r.answered = map[string]map[any]bool{}
	r.correctCount = map[string]int{}
	r.pendingPoints = nil
	r.holder, r.candidates, r.buzzOpen = nil, nil, false
	r.attempted = map[any]bool{}
	r.turn++
	r.paused, r.clockStops = 0, 0
	r.sendTrackStart()
}

type publicGuess struct {
	Label     string   `json:"label"`
	Type      string   `json:"type"`
	Choices   []string `json:"choices,omitempty"`
	ChoicesAt float64  `json:"choicesAt,omitempty"`
}

func (r *Room) roundPayload() map[string]any {
	i := r.playlist[r.current].round
	return map[string]any{"index": i, "name": r.loaded.Load().pack.Manifest.Rounds[i].Name}
}

// trackPayload tells players what to guess, the host how to play the media, and control the answers too.
func (r *Room) trackPayload(role string) map[string]any {
	it := r.playlist[r.current]
	t := it.track
	guesses := make([]publicGuess, len(t.Guesses))
	for i, g := range t.Guesses {
		guesses[i] = publicGuess{g.Label, g.Type, g.Choices, g.ChoicesAt}
	}
	public := map[string]any{
		"index":    r.current,
		"total":    len(r.playlist),
		"round":    it.round,
		"duration": it.rules.duration.Seconds(),
		"guesses":  guesses,
		"mode":     it.rules.mode,
		"via":      it.rules.via,
	}
	rate := t.PlaybackRate
	if rate == 0 {
		rate = 1
	}
	host := maps.Clone(public)
	host["media"] = (&url.URL{Path: "/media/" + t.Media}).EscapedPath()
	host["start"] = t.Start
	host["playbackRate"] = rate
	host["reveal"] = t.Reveal
	host["hints"] = t.Hints
	control := maps.Clone(host)
	control["guesses"] = t.Guesses

	switch role {
	case RoleHost:
		return host
	case RoleControl:
		return control
	}
	return public
}

func (r *Room) sendTrackStart() {
	for c := range r.clients {
		r.send(c, NewMessage("track-start", r.trackPayload(c.role)))
	}
}

func (r *Room) mediaStarted(c *Client) {
	if r.trackState != trackLoading {
		r.sendError(c, "wrong-state", "no track is loading")
		return
	}
	slog.Debug("media started", "index", r.current)
	r.trackState = trackLive
	r.trackStart = time.Now()
	r.paused = 0
	r.broadcast(NewMessage("timer-start", map[string]any{"index": r.current, "duration": r.rules.duration.Seconds()}))
	if r.rules.mode == "buzz" {
		r.openBuzz()
	}
	r.armTrackTimer()
}

// elapsed is the track time actually played: pauses and answers, finished or ongoing, excluded.
func (r *Room) elapsed() time.Duration {
	e := time.Since(r.trackStart) - r.paused
	if r.clockStops > 0 {
		e -= time.Since(r.pausedAt)
	}
	return e
}

func (r *Room) armTrackTimer() {
	seq := r.trackSeq
	time.AfterFunc(r.rules.duration-r.elapsed(), func() { r.events <- event{kind: evTrackTimer, turn: seq} })
}

// trackTimer ends the track once its duration has been played; a timer made early by a pause is re-armed.
func (r *Room) trackTimer(seq int) {
	if seq != r.trackSeq || r.trackState != trackLive || r.clockStops > 0 {
		return
	}
	if r.elapsed() < r.rules.duration {
		r.armTrackTimer()
		return
	}
	r.endTrack("time")
}

func (r *Room) endTrack(reason string) {
	slog.Info("track ended", "index", r.current, "reason", reason)
	r.trackState = trackEnded
	r.endReason = reason
	r.turn++
	r.holder, r.candidates, r.buzzOpen = nil, nil, false
	r.broadcast(NewMessage("track-end", r.trackEndPayload()))
	for _, s := range r.pendingPoints {
		if s.points != 0 {
			r.addPoints(s.player, s.points)
		}
	}
	r.pendingPoints = nil
}

func (r *Room) trackEndPayload() map[string]any {
	return map[string]any{"index": r.current, "reason": r.endReason, "guesses": r.track().Guesses}
}

func (r *Room) skip() {
	if r.trackState != trackEnded {
		r.endTrack("skipped")
	}
	r.nextTrack()
}
