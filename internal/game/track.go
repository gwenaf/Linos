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
	headStart    time.Duration
	exclusive    bool
	othersBonus  int
	doubleJokers int
	attempts     int
	rebound      string // none, others or all
	reboundBonus int
	wrongLockout time.Duration
	pauseOnBuzz  bool
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
	attempts:    1,
	rebound:     "others",
	pauseOnBuzz: true,
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
		if a.Attempts != nil {
			r.attempts = *a.Attempts
		}
		if a.Rebound != nil {
			r.rebound = *a.Rebound
		}
		if a.WrongLockout != nil {
			r.wrongLockout = seconds(*a.WrongLockout)
		}
		if a.PauseOnBuzz != nil {
			r.pauseOnBuzz = *a.PauseOnBuzz
		}
	}
	if o := p.Owner; o != nil {
		if o.HeadStart != nil {
			r.headStart = seconds(*o.HeadStart)
		}
		if o.Exclusive != nil {
			r.exclusive = *o.Exclusive
		}
		if o.OthersBonus != nil {
			r.othersBonus = *o.OthersBonus
		}
	}
	if p.Jokers != nil && p.Jokers.Double != nil {
		r.doubleJokers = *p.Jokers.Double
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
	if s.ReboundBonus != nil {
		r.reboundBonus = *s.ReboundBonus
	}
	return r
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

type playItem struct {
	round int         // -1 when the pack has no rounds, and for tiebreak tracks
	track *pack.Track // nil until picked, in theme-pick rounds
	rules rules
}

func (r *Room) track() *pack.Track {
	return r.playlist[r.current].track
}

// buildPlaylist flattens the rounds into the ordered list of tracks to play; theme-pick tracks are chosen later.
func (r *Room) buildPlaylist() []playItem {
	m := r.manifest()
	base := defaultRules.with(m.Rules)
	r.used = map[string]bool{}
	if len(m.Rounds) == 0 {
		items := make([]playItem, len(m.Tracks))
		for i := range m.Tracks {
			items[i] = playItem{-1, &m.Tracks[i], base}
			r.used[m.Tracks[i].ID] = true
		}
		return items
	}

	byID := map[string]*pack.Track{}
	for i := range m.Tracks {
		byID[m.Tracks[i].ID] = &m.Tracks[i]
	}
	var items []playItem
	for ri, rd := range m.Rounds {
		rr := base.with(rd.Rules)
		if rd.Selection == "theme-pick" {
			for range rd.Count {
				items = append(items, playItem{ri, nil, rr})
			}
			continue
		}
		ids := rd.Tracks
		if rd.Selection == "random" {
			ids = randomTracks(m, rd, r.used)
		}
		for _, id := range ids {
			r.used[id] = true
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

// nextTrack moves to the next playlist item, closing the round it leaves and adding tiebreak tracks at the end.
func (r *Room) nextTrack() {
	prev := -1
	if r.current >= 0 {
		prev = r.playlist[r.current].round
	}
	r.current++
	if prev >= 0 && (r.current >= len(r.playlist) || r.playlist[r.current].round != prev) && r.endRound(prev) {
		return
	}
	if r.current >= len(r.playlist) && !r.addTiebreak() {
		r.endGame("ended")
		return
	}
	it := r.playlist[r.current]
	if it.round >= 0 && it.round != prev {
		r.picks = 0
		r.broadcast(NewMessage("round-start", r.roundPayload()))
	}
	r.rules = it.rules
	r.trackSeq++
	r.turn++
	r.owner, r.picker, r.wagers = nil, nil, nil
	r.doubled = map[any]bool{}
	r.holder, r.candidates, r.buzzOpen = nil, nil, false
	r.prepareTrack()
}

func (r *Room) startTrack() {
	slog.Info("track started", "index", r.current, "track", r.track().ID)
	r.trackState = trackLoading
	r.found = map[string]bool{}
	r.answered = map[string]map[any]bool{}
	r.correctCount = map[string]int{}
	r.answerTries = map[string]map[any]int{}
	r.pendingPoints = nil
	r.newGuessCycle()
	r.paused, r.clockStops = 0, 0
	r.headStartOver = r.owner == nil || r.rules.headStart == 0
	if d := r.track().Duration; d > 0 {
		r.rules.duration = seconds(d)
	}
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
	return map[string]any{"index": i, "name": r.manifest().Rounds[i].Name}
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
		"index":       r.current,
		"total":       len(r.playlist),
		"round":       it.round,
		"duration":    r.rules.duration.Seconds(),
		"guesses":     guesses,
		"mode":        it.rules.mode,
		"via":         it.rules.via,
		"attempts":    it.rules.attempts,
		"rebound":     it.rules.rebound,
		"pauseOnBuzz": it.rules.pauseOnBuzz,
	}
	if r.owner != nil {
		public["owner"] = unitPayload(r.owner)
		public["headStart"] = it.rules.headStart.Seconds()
		public["exclusive"] = it.rules.exclusive
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
	if !r.headStartOver {
		r.armHeadStart()
	}
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

// endTrack reveals the answers, applies the points kept hidden during simultaneous answers
// and ends the game if a unit reached the target score.
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
	r.scoreReached()
}

func (r *Room) trackEndPayload() map[string]any {
	return map[string]any{"index": r.current, "reason": r.endReason, "guesses": r.track().Guesses}
}

// skip ends the current track if it started, and moves on; a pending theme pick or wager is dropped.
func (r *Room) skip() {
	if r.trackState == trackLoading || r.trackState == trackLive {
		r.endTrack("skipped")
	}
	if r.state == stateInProgress {
		r.nextTrack()
	}
}
