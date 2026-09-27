package pack

import (
	"archive/zip"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

type validator struct {
	p    *Pack
	errs []error
}

func (v *validator) fail(format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf(format, args...))
}

// validate reports every problem at once, so a pack author can fix them in one pass.
func (p *Pack) validate() error {
	m := &p.Manifest
	v := &validator{p: p}

	if m.Version < 1 || m.Version > SupportedVersion {
		v.fail("version %d is not supported (this build reads up to %d)", m.Version, SupportedVersion)
	}
	if strings.TrimSpace(m.Title) == "" {
		v.fail("title is required")
	}
	if m.Cover != "" {
		v.file("cover", m.Cover)
	}

	c := m.Game.Control
	for _, a := range c.Allowed {
		v.oneOf("game.control.allowed", a, "master", "auto")
	}
	if c.Default != "" && !slices.Contains(c.Allowed, c.Default) {
		v.fail("game.control.default %q is not in game.control.allowed", c.Default)
	}
	pl := m.Game.Players
	v.oneOf("game.players.teams", pl.Teams, "", "none", "optional", "required")
	if pl.MaxTeams > 0 && pl.MinTeams > pl.MaxTeams {
		v.fail("game.players.minTeams exceeds maxTeams")
	}
	v.oneOf("game.end.type", m.Game.End.Type, "", "rounds", "score", "elimination")
	if m.Game.End.Type == "score" && m.Game.End.Target <= 0 {
		v.fail("game.end.target must be positive when game.end.type is score")
	}
	v.oneOf("game.tiebreak.type", m.Game.Tiebreak.Type, "", "sudden-death")
	v.rules("rules", m.Rules)

	themes := map[string]bool{}
	for i, t := range m.Themes {
		switch {
		case t.ID == "":
			v.fail("themes[%d].id is required", i)
		case themes[t.ID]:
			v.fail("themes[%d].id %q is duplicated", i, t.ID)
		}
		themes[t.ID] = true
	}
	v.refs("game.tiebreak.themes", m.Game.Tiebreak.Themes, themes)

	if len(m.Tracks) == 0 {
		v.fail("at least one track is required")
	}
	tracks := map[string]bool{}
	for i, t := range m.Tracks {
		at := fmt.Sprintf("tracks[%d]", i)
		switch {
		case t.ID == "":
			v.fail("%s.id is required", at)
		case tracks[t.ID]:
			v.fail("%s.id %q is duplicated", at, t.ID)
		}
		tracks[t.ID] = true
		v.refs(at+".themes", t.Themes, themes)
		v.file(at+".media", t.Media)
		if t.Start < 0 || t.Duration < 0 || t.PlaybackRate < 0 {
			v.fail("%s: start, duration and playbackRate cannot be negative", at)
		}
		for j, r := range t.Reveal {
			v.reveal(fmt.Sprintf("%s.reveal[%d]", at, j), r)
		}
		if len(t.Guesses) == 0 {
			v.fail("%s: at least one guess is required", at)
		}
		labels := map[string]bool{}
		for j, g := range t.Guesses {
			gat := fmt.Sprintf("%s.guesses[%d]", at, j)
			v.guess(gat, g)
			if labels[g.Label] {
				v.fail("%s.label %q is duplicated", gat, g.Label)
			}
			labels[g.Label] = true
		}
	}

	for i, r := range m.Rounds {
		at := fmt.Sprintf("rounds[%d]", i)
		v.oneOf(at+".picker", r.Picker, "", "rotation")
		v.rules(at+".rules", r.Rules)
		switch r.Selection {
		case "sequence":
			if len(r.Tracks) == 0 {
				v.fail("%s: a sequence round needs tracks", at)
			}
			v.refs(at+".tracks", r.Tracks, tracks)
		case "random", "theme-pick":
			if len(r.Themes) == 0 {
				v.fail("%s: a %s round needs themes", at, r.Selection)
			}
			v.refs(at+".themes", r.Themes, themes)
			if r.Count <= 0 {
				v.fail("%s.count must be positive", at)
			}
		default:
			v.oneOf(at+".selection", r.Selection, "sequence", "random", "theme-pick")
		}
	}
	return errors.Join(v.errs...)
}

func (v *validator) reveal(at string, r Reveal) {
	neg := func(x *float64) bool { return x != nil && *x < 0 }
	if r.At < 0 || neg(r.Blur) || neg(r.Pixelate) {
		v.fail("%s: at, blur and pixelate cannot be negative", at)
	}
	if r.Grayscale != nil && (*r.Grayscale < 0 || *r.Grayscale > 1) {
		v.fail("%s.grayscale must be between 0 and 1", at)
	}
	if r.Image != nil && *r.Image != "" {
		v.file(at+".image", *r.Image)
	}
}

func (v *validator) guess(at string, g Guess) {
	if strings.TrimSpace(g.Label) == "" {
		v.fail("%s.label is required", at)
	}
	v.scoring(at+".scoring", g.Scoring)
	switch g.Type {
	case "text":
		if len(g.Answers) == 0 {
			v.fail("%s: a text guess needs answers", at)
		}
	case "choice":
		if answer, ok := g.Answer.(string); !ok || !slices.Contains(g.Choices, answer) {
			v.fail("%s.answer must be one of choices", at)
		}
	case "number":
		if _, ok := g.Answer.(float64); !ok {
			v.fail("%s.answer must be a number", at)
		}
	default:
		v.oneOf(at+".type", g.Type, "text", "choice", "number")
	}
}

func (v *validator) rules(at string, r *Rules) {
	if r == nil {
		return
	}
	if a := r.Answer; a != nil {
		v.optionalOneOf(at+".answer.mode", a.Mode, "buzz", "simultaneous")
		v.optionalOneOf(at+".answer.via", a.Via, "oral", "device")
		v.optionalOneOf(at+".answer.rebound", a.Rebound, "none", "others", "all")
		if a.Fuzziness != nil && (*a.Fuzziness < 0 || *a.Fuzziness > 1) {
			v.fail("%s.answer.fuzziness must be between 0 and 1", at)
		}
		if a.Attempts != nil && *a.Attempts < 1 {
			v.fail("%s.answer.attempts must be at least 1", at)
		}
		if a.WrongLockout != nil && *a.WrongLockout < 0 {
			v.fail("%s.answer.wrongLockout cannot be negative", at)
		}
	}
	v.scoring(at+".scoring", r.Scoring)
}

func (v *validator) scoring(at string, s *Scoring) {
	if s == nil {
		return
	}
	v.optionalOneOf(at+".type", s.Type, "speed", "fixed", "rank", "wager")
	if s.Max != nil && s.Min != nil && *s.Min > *s.Max {
		v.fail("%s.min exceeds max", at)
	}
}

func (v *validator) file(field, name string) {
	if !validPath(name) {
		v.fail("%s %q is not a valid relative path", field, name)
		return
	}
	if v.p.zipFiles == nil {
		if _, err := fs.Stat(v.p.fsys, name); err != nil {
			v.fail("%s %q not found", field, name)
		}
		return
	}
	zf, ok := v.p.zipFiles[name]
	if !ok {
		v.fail("%s %q not found", field, name)
		return
	}
	if zf.Method != zip.Store {
		v.fail("%s %q must be stored uncompressed in the archive", field, name)
	}
	if _, err := zf.DataOffset(); err != nil {
		v.fail("%s %q is corrupted: %v", field, name, err)
	}
}

func (v *validator) refs(field string, ids []string, known map[string]bool) {
	for _, id := range ids {
		if !known[id] {
			v.fail("%s: unknown id %q", field, id)
		}
	}
}

func (v *validator) oneOf(field, value string, allowed ...string) {
	if !slices.Contains(allowed, value) {
		v.fail("%s %q must be one of %s", field, value, strings.Join(allowed, ", "))
	}
}

func (v *validator) optionalOneOf(field string, value *string, allowed ...string) {
	if value != nil {
		v.oneOf(field, *value, allowed...)
	}
}
