package game

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gwenaf/linos/internal/pack"
)

type loadedPack struct {
	name string
	pack *pack.Pack
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
	r.send(c, NewMessage("packs", map[string]any{"packs": list}))
}

func (r *Room) configure(c *Client, m Message) {
	var d struct {
		Pack string `json:"pack"`
	}
	if !r.decode(c, m, &d) {
		return
	}
	if d.Pack == "" || d.Pack == "." || d.Pack == ".." || filepath.Base(d.Pack) != d.Pack {
		r.sendError(c, "invalid-pack", "pack must be a name from the packs folder")
		return
	}
	p, err := pack.Open(filepath.Join(r.packsDir, d.Pack))
	if err == nil {
		if err = unsupported(&p.Manifest); err != nil {
			p.Close()
		}
	}
	if err != nil {
		r.sendError(c, "invalid-pack", err.Error())
		return
	}

	media := map[string]bool{p.Manifest.Cover: true}
	for _, t := range p.Manifest.Tracks {
		media[t.Media] = true
	}
	if old := r.loaded.Swap(&loadedPack{name: d.Pack, pack: p, media: media}); old != nil {
		// ponytail: a media download still streaming from the old pack is cut; reconfiguring only happens in the lobby.
		old.pack.Close()
	}

	rounds := []string{}
	for _, rd := range p.Manifest.Rounds {
		rounds = append(rounds, rd.Name)
	}
	r.broadcast(NewMessage("configured", map[string]any{
		"pack":   d.Pack,
		"title":  p.Manifest.Title,
		"author": p.Manifest.Author,
		"rounds": rounds,
		"tracks": len(p.Manifest.Tracks),
	}))
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
		if a.Mode != nil && *a.Mode != "buzz" {
			fail("%s.answer.mode %q", at, *a.Mode)
		}
		if a.Via != nil && *a.Via != "oral" {
			fail("%s.answer.via %q", at, *a.Via)
		}
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
	if s.Type != nil && *s.Type != "speed" && *s.Type != "fixed" {
		fail("%s.type %q", at, *s.Type)
	}
	if s.ReboundBonus != nil && *s.ReboundBonus != 0 {
		fail("%s.reboundBonus", at)
	}
}
