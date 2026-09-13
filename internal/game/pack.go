package game

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gwenaf/linos/internal/pack"
)

type loadedPack struct {
	name    string
	control string // master or auto
	pack    *pack.Pack
	// media lists the files the manifest references; only those are served.
	media map[string]bool
}

// Media opens a media file of the configured pack. Safe to call from any goroutine.
func (r *Room) Media(name string) (io.ReadSeekCloser, error) {
	lp := r.loaded.Load()
	if lp == nil || !lp.media[name] {
		return nil, fs.ErrNotExist
	}
	return lp.pack.Media(name)
}

func (r *Room) listPacks(c *Client) {
	type info struct {
		Name  string `json:"name"`
		Title string `json:"title,omitempty"`
		Error string `json:"error,omitempty"`
	}
	list := []info{}
	entries, _ := os.ReadDir(r.packsDir) // a missing packs folder simply means no packs
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) != ".linospack" {
			continue
		}
		it := info{Name: e.Name()}
		p, err := pack.Open(filepath.Join(r.packsDir, e.Name()))
		if err == nil {
			it.Title = p.Manifest.Title
			err = unsupported(&p.Manifest)
			p.Close()
		}
		if err != nil {
			it.Error = err.Error()
		}
		list = append(list, it)
	}
	r.send(c, NewMessage("packs", map[string]any{"dir": r.packsDir, "packs": list}))
}

func (r *Room) configure(c *Client, m Message) {
	var d struct {
		Pack    string `json:"pack"`
		Control string `json:"control"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	if d.Pack == "" || d.Pack == "." || d.Pack == ".." || filepath.Base(d.Pack) != d.Pack {
		r.sendError(c, "invalid-pack", "pack must be a name from the packs folder")
		return
	}
	control := d.Control
	p, err := pack.Open(filepath.Join(r.packsDir, d.Pack))
	if err == nil {
		if control == "" {
			control = cmp.Or(p.Manifest.Game.Control.Default, "master")
		}
		if err = errors.Join(unsupported(&p.Manifest), checkControl(&p.Manifest, control)); err != nil {
			p.Close()
		}
	}
	if err != nil {
		slog.Warn("pack refused", "pack", d.Pack, "error", err)
		r.sendError(c, "invalid-pack", err.Error())
		return
	}
	slog.Info("pack configured", "pack", d.Pack, "title", p.Manifest.Title, "control", control)

	media := map[string]bool{p.Manifest.Cover: true}
	for _, t := range p.Manifest.Tracks {
		media[t.Media] = true
	}
	if old := r.loaded.Swap(&loadedPack{name: d.Pack, control: control, pack: p, media: media}); old != nil {
		// ponytail: a media download still streaming from the old pack is cut; reconfiguring only happens in the lobby.
		old.pack.Close()
	}

	r.broadcast(NewMessage("configured", r.configuredPayload()))
}

func (r *Room) configuredPayload() map[string]any {
	lp := r.loaded.Load()
	rounds := []string{}
	for _, rd := range lp.pack.Manifest.Rounds {
		rounds = append(rounds, rd.Name)
	}
	return map[string]any{
		"pack":    lp.name,
		"control": lp.control,
		"title":   lp.pack.Manifest.Title,
		"author":  lp.pack.Manifest.Author,
		"rounds":  rounds,
		"tracks":  len(lp.pack.Manifest.Tracks),
	}
}

// unsupported lists the manifest features this build does not play yet, so a pack is refused rather than misplayed.
func unsupported(m *pack.Manifest) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format+": not supported yet", args...))
	}

	if t := m.Game.End.Type; t != "" && t != "rounds" {
		fail("game.end.type %q", t)
	}
	if m.Game.Tiebreak.Type != "" {
		fail("game.tiebreak")
	}
	checkRules("rules", m.Rules, fail)
	for i, rd := range m.Rounds {
		at := fmt.Sprintf("rounds[%d]", i)
		if rd.Selection == "theme-pick" {
			fail("%s.selection theme-pick", at)
		}
		if rd.Eliminate > 0 {
			fail("%s.eliminate", at)
		}
		checkRules(at+".rules", rd.Rules, fail)
	}
	for i, t := range m.Tracks {
		for j, g := range t.Guesses {
			checkScoring(fmt.Sprintf("tracks[%d].guesses[%d].scoring", i, j), g.Scoring, fail)
		}
	}
	return errors.Join(errs...)
}

func checkRules(at string, r *pack.Rules, fail func(string, ...any)) {
	if r == nil {
		return
	}
	if a := r.Answer; a != nil {
		if a.Attempts != nil && *a.Attempts != 1 {
			fail("%s.answer.attempts %d", at, *a.Attempts)
		}
		if a.Rebound != nil && *a.Rebound != "others" {
			fail("%s.answer.rebound %q", at, *a.Rebound)
		}
		if a.PauseOnBuzz != nil && !*a.PauseOnBuzz {
			fail("%s.answer.pauseOnBuzz false", at)
		}
	}
	if o := r.Owner; o != nil && (o.HeadStart != nil && *o.HeadStart > 0 || o.Exclusive != nil && *o.Exclusive || o.OthersBonus != nil && *o.OthersBonus != 0) {
		fail("%s.owner", at)
	}
	if j := r.Jokers; j != nil && j.Double != nil && *j.Double > 0 {
		fail("%s.jokers", at)
	}
	checkScoring(at+".scoring", r.Scoring, fail)
}

func checkScoring(at string, s *pack.Scoring, fail func(string, ...any)) {
	if s == nil {
		return
	}
	if s.Type != nil && *s.Type == "wager" {
		fail("%s.type %q", at, *s.Type)
	}
	if s.ReboundBonus != nil && *s.ReboundBonus != 0 {
		fail("%s.reboundBonus", at)
	}
}

// checkControl refuses the control and answer combinations that cannot be played.
func checkControl(m *pack.Manifest, control string) error {
	if control != "master" && control != "auto" {
		return fmt.Errorf("control %q must be master or auto", control)
	}
	if allowed := m.Game.Control.Allowed; len(allowed) > 0 && !slices.Contains(allowed, control) {
		return fmt.Errorf("control %q is not allowed by this pack (allowed: %s)", control, strings.Join(allowed, ", "))
	}
	base := defaultRules.with(m.Rules)
	played := map[string]rules{"rules": base}
	if len(m.Rounds) > 0 {
		played = map[string]rules{}
		for i, rd := range m.Rounds {
			played[fmt.Sprintf("rounds[%d]", i)] = base.with(rd.Rules)
		}
	}
	var errs []error
	for at, r := range played {
		if r.mode == "simultaneous" && r.via == "oral" {
			errs = append(errs, fmt.Errorf("%s: simultaneous answers must be given on the phones (answer.via device)", at))
		}
		if control == "auto" && r.via == "oral" {
			errs = append(errs, fmt.Errorf("%s: oral answers need a gamemaster (control master)", at))
		}
		if r.scoringType == "rank" && r.mode != "simultaneous" {
			errs = append(errs, fmt.Errorf("%s: rank scoring needs simultaneous answers", at))
		}
	}
	return errors.Join(errs...)
}
