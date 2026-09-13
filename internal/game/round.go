package game

import (
	"cmp"
	"log/slog"
	"maps"
	"math"
	mrand "math/rand/v2"
	"slices"
	"time"

	"github.com/gwenaf/linos/internal/pack"
)

const (
	trackPicking  = "picking"
	trackWagering = "wagering"
)

func (r *Room) manifest() *pack.Manifest {
	return &r.loaded.Load().pack.Manifest
}

func unitLabel(u any) string {
	if t, ok := u.(*team); ok {
		return t.name
	}
	return u.(*player).name
}

func unitPayload(u any) map[string]any {
	if t, ok := u.(*team); ok {
		return map[string]any{"team": t.name}
	}
	return map[string]any{"name": u.(*player).name}
}

func unitScore(u any) int {
	if t, ok := u.(*team); ok {
		return t.score
	}
	return u.(*player).score
}

func unitPayloads(units []any) []map[string]any {
	out := []map[string]any{}
	for _, u := range units {
		out = append(out, unitPayload(u))
	}
	return out
}

func sortedUnits(set map[any]bool) []any {
	units := slices.Collect(maps.Keys(set))
	slices.SortFunc(units, func(a, b any) int { return cmp.Compare(unitLabel(a), unitLabel(b)) })
	return units
}

// units lists the teams and solo players with a named member, not eliminated and part of the tiebreak
// if one runs, sorted by label; connected restricts to those with a member online.
func (r *Room) units(connected bool) []any {
	set := map[any]bool{}
	for _, p := range r.players {
		u := unit(p)
		if p.name != "" && (!connected || p.client != nil) && !r.eliminated[u] && (r.tiebreak == nil || r.tiebreak[u]) {
			set[u] = true
		}
	}
	return sortedUnits(set)
}

// inPlay tells whether unit u takes part in the current track at all.
func (r *Room) inPlay(u any) bool {
	return !r.eliminated[u] && (r.tiebreak == nil || r.tiebreak[u]) &&
		!(r.rules.exclusive && r.owner != nil && u != r.owner)
}

// mayPlay tells whether unit u may buzz or answer right now: in play and past the theme owner's head start.
func (r *Room) mayPlay(u any) bool {
	return r.inPlay(u) && (r.owner == nil || u == r.owner || r.headStartOver)
}

func (r *Room) playingUnits() []any {
	return slices.DeleteFunc(r.units(true), func(u any) bool { return !r.inPlay(u) })
}

// prepareTrack runs what comes before a track: the theme pick, then the wagers, then the track itself.
func (r *Room) prepareTrack() {
	it := r.playlist[r.current]
	switch {
	case it.track == nil:
		if !r.requestPick() {
			slog.Info("theme pick skipped: no theme or team left", "index", r.current)
			r.nextTrack()
		}
	case it.rules.scoringType == "wager" && r.wagers == nil && len(r.playingUnits()) > 0:
		r.trackState = trackWagering
		r.wagers = map[any]int{}
		r.broadcast(NewMessage("wager-request", r.wagerPayload()))
	default:
		r.startTrack()
	}
}

func (r *Room) requestPick() bool {
	units := r.playingUnits()
	if len(r.pickableThemes()) == 0 || len(units) == 0 {
		return false
	}
	r.picker = units[r.picks%len(units)]
	r.picks++
	r.trackState = trackPicking
	r.broadcast(NewMessage("theme-pick-request", r.pickPayload()))
	return true
}

// pickableThemes lists the round themes that still have an unplayed track.
func (r *Room) pickableThemes() []pack.Theme {
	m := r.manifest()
	ids := m.Rounds[r.playlist[r.current].round].Themes
	var themes []pack.Theme
	for _, th := range m.Themes {
		if slices.Contains(ids, th.ID) && len(r.unplayed(th.ID)) > 0 {
			themes = append(themes, th)
		}
	}
	return themes
}

func (r *Room) unplayed(theme string) []*pack.Track {
	m := r.manifest()
	var tracks []*pack.Track
	for i := range m.Tracks {
		if t := &m.Tracks[i]; !r.used[t.ID] && slices.Contains(t.Themes, theme) {
			tracks = append(tracks, t)
		}
	}
	return tracks
}

func (r *Room) pickPayload() map[string]any {
	return map[string]any{"picker": unitPayload(r.picker), "themes": r.pickableThemes()}
}

func (r *Room) pickTheme(c *Client, m Message) {
	var d struct {
		Theme string `json:"theme"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	if r.trackState != trackPicking || unit(c.player) != r.picker {
		r.sendError(c, "not-your-pick", "another team picks the theme")
		return
	}
	var tracks []*pack.Track
	if slices.ContainsFunc(r.pickableThemes(), func(th pack.Theme) bool { return th.ID == d.Theme }) {
		tracks = r.unplayed(d.Theme)
	}
	if len(tracks) == 0 {
		r.sendError(c, "unknown-theme", "no track left in theme "+d.Theme)
		return
	}
	t := tracks[mrand.IntN(len(tracks))]
	slog.Info("theme picked", "picker", unitLabel(r.picker), "theme", d.Theme, "track", t.ID)
	r.used[t.ID] = true
	r.playlist[r.current].track = t
	r.owner = r.picker
	picked := unitPayload(r.owner)
	picked["theme"] = d.Theme
	r.broadcast(NewMessage("theme-picked", picked))
	r.prepareTrack()
}

func (r *Room) wagerPayload() map[string]any {
	limits := []map[string]any{}
	for _, u := range r.playingUnits() {
		l := unitPayload(u)
		_, placed := r.wagers[u]
		l["max"] = max(unitScore(u), 0)
		l["placed"] = placed
		limits = append(limits, l)
	}
	return map[string]any{"limits": limits}
}

// wager records a bet up to the unit's score; the track starts once every playing unit has bet.
func (r *Room) wager(c *Client, m Message) {
	var d struct {
		Amount *int `json:"amount"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	u := unit(c.player)
	switch {
	case d.Amount == nil:
		r.sendError(c, "bad-data", "wager requires amount")
		return
	case r.trackState != trackWagering || c.player.name == "" || !r.inPlay(u):
		r.sendError(c, "wrong-state", "no wager expected from you")
		return
	case *d.Amount < 0 || *d.Amount > max(unitScore(u), 0):
		r.sendError(c, "invalid-wager", "bet between 0 and your score")
		return
	}
	r.wagers[u] = *d.Amount
	slog.Info("wager placed", "unit", unitLabel(u), "amount", *d.Amount)
	r.broadcast(NewMessage("wagered", unitPayload(u)))
	r.startIfAllWagered()
}

func (r *Room) startIfAllWagered() {
	for _, u := range r.playingUnits() {
		if _, ok := r.wagers[u]; !ok {
			return
		}
	}
	r.startTrack()
}

// useJoker doubles the unit's positive points on the coming track.
func (r *Room) useJoker(c *Client, m Message) {
	var d struct {
		Type string `json:"type"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	p := c.player
	u := unit(p)
	switch {
	case d.Type != "double":
		r.sendError(c, "unknown-joker", "only the double joker exists")
	case p.name == "":
		r.sendError(c, "not-identified", "identify first")
	case r.trackState == trackLive || r.trackState == trackEnded:
		r.sendError(c, "wrong-state", "jokers are played before the track starts")
	case r.rules.doubleJokers == 0 || r.jokersUsed[u] >= r.jokerLimit:
		r.sendError(c, "no-joker", "no double joker available")
	case r.doubled[u]:
		r.sendError(c, "no-joker", "this track is already doubled")
	default:
		r.jokersUsed[u]++
		r.doubled[u] = true
		slog.Info("joker used", "unit", unitLabel(u), "type", d.Type)
		used := unitPayload(u)
		used["type"] = d.Type
		r.broadcast(NewMessage("joker-used", used))
	}
}

// pointsFor scores an answer for unit u: rank, speed, fixed or wager, plus the bonus for beating the theme owner
// and the double joker.
func (r *Room) pointsFor(u any, g *pack.Guess, correct bool, elapsed time.Duration) int {
	gr := r.rules
	if g != nil && g.Scoring != nil {
		gr = gr.withScoring(g.Scoring)
	}
	var points int
	switch {
	case gr.scoringType == "wager":
		points = r.wagers[u]
		if !correct {
			points = -points
		}
	case !correct:
		points = -gr.wrongPenalty
	case gr.scoringType == "speed":
		points = speedPoints(gr.max, gr.min, r.rules.duration, elapsed)
	case gr.scoringType == "rank":
		if n := r.correctCount[g.Label]; n < len(gr.ranks) {
			points = gr.ranks[n]
		}
		r.correctCount[g.Label]++
	default:
		points = gr.max
	}
	if correct && r.owner != nil && u != r.owner {
		points += r.rules.othersBonus
	}
	if points > 0 && r.doubled[u] {
		points *= 2
	}
	return points
}

func (r *Room) armHeadStart() {
	seq := r.trackSeq
	time.AfterFunc(r.rules.headStart-r.elapsed(), func() { r.events <- event{kind: evHeadStart, turn: seq} })
}

// headStartTimer lets the other teams play once the owner's head start has been played.
func (r *Room) headStartTimer(seq int) {
	if seq != r.trackSeq || r.trackState != trackLive || r.clockStops > 0 || r.headStartOver {
		return
	}
	if r.elapsed() < r.rules.headStart {
		r.armHeadStart()
		return
	}
	r.headStartOver = true
	r.broadcast(NewMessage("head-start-over", nil))
	if r.buzzOpen {
		r.openBuzz()
	}
}

// endRound publishes the round results and eliminates its lowest teams; true when that ends the game.
func (r *Room) endRound(i int) bool {
	rd := r.manifest().Rounds[i]
	r.broadcast(NewMessage("round-end", map[string]any{"index": i, "name": rd.Name, "results": r.results()}))
	if rd.Eliminate <= 0 {
		return false
	}
	remaining := r.units(false)
	// units is sorted by label, so equal scores are eliminated in label order.
	slices.SortStableFunc(remaining, func(a, b any) int { return cmp.Compare(unitScore(a), unitScore(b)) })
	n := max(min(rd.Eliminate, len(remaining)-1), 0)
	out := remaining[:n]
	for _, u := range out {
		r.eliminated[u] = true
	}
	slog.Info("units eliminated", "round", i, "count", n)
	r.broadcast(NewMessage("eliminated", map[string]any{"units": unitPayloads(out)}))
	if r.manifest().Game.End.Type == "elimination" && len(remaining)-n <= 1 {
		r.endGame("ended")
		return true
	}
	return false
}

// addTiebreak appends a sudden-death track when the leaders are tied; only they may play it.
func (r *Room) addTiebreak() bool {
	m := r.manifest()
	if m.Game.Tiebreak.Type != "sudden-death" {
		return false
	}
	best, tied := math.MinInt, map[any]bool{}
	for _, u := range r.units(false) {
		switch s := unitScore(u); {
		case s > best:
			best, tied = s, map[any]bool{u: true}
		case s == best:
			tied[u] = true
		}
	}
	if len(tied) < 2 {
		return false
	}
	var tracks []*pack.Track
	for i := range m.Tracks {
		t := &m.Tracks[i]
		inTheme := len(m.Game.Tiebreak.Themes) == 0 || slices.ContainsFunc(t.Themes, func(th string) bool {
			return slices.Contains(m.Game.Tiebreak.Themes, th)
		})
		if inTheme && !r.used[t.ID] {
			tracks = append(tracks, t)
		}
	}
	if len(tracks) == 0 {
		return false
	}
	t := tracks[mrand.IntN(len(tracks))]
	r.used[t.ID] = true
	r.tiebreak = tied
	r.playlist = append(r.playlist, playItem{-1, t, defaultRules.with(m.Rules)})
	slog.Info("tiebreak", "units", len(tied), "track", t.ID)
	r.broadcast(NewMessage("tiebreak", map[string]any{"units": unitPayloads(sortedUnits(tied))}))
	return true
}

// scoreReached ends the game when a unit reaches the pack's target score; true when it did.
func (r *Room) scoreReached() bool {
	end := r.manifest().Game.End
	if end.Type != "score" {
		return false
	}
	for _, u := range r.units(false) {
		if unitScore(u) >= end.Target {
			r.endGame("ended")
			return true
		}
	}
	return false
}
