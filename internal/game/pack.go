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

// PacksDir is the folder the room lists and loads packs from.
func (r *Room) PacksDir() string { return r.packsDir }

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
			err = checkControl(&p.Manifest, cmp.Or(p.Manifest.Game.Control.Default, "master"))
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
		if err = checkControl(&p.Manifest, control); err != nil {
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
