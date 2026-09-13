package game

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gwenaf/linos/internal/pack"
)

func TestConfigure(t *testing.T) {
	r := newRoom(t, nil)
	unsupportedPack := basicManifest(1)
	unsupportedPack["rules"] = obj{"answer": obj{"attempts": 2}}
	writePack(t, r.packsDir, "later", unsupportedPack)
	ctrl := newControl(t, r)

	for _, name := range []string{"", ".", "..", "sub/basic", `sub\basic`} {
		send(r, ctrl, "configure", obj{"pack": name})
		expectError(t, ctrl, "invalid-pack")
	}
	send(r, ctrl, "configure", "x")
	expectError(t, ctrl, "bad-data")

	var e struct {
		Code, Message string
	}
	send(r, ctrl, "configure", obj{"pack": "missing"})
	expect(t, ctrl, "error", &e)
	if e.Code != "invalid-pack" {
		t.Fatalf("error = %+v, want invalid-pack", e)
	}
	send(r, ctrl, "configure", obj{"pack": "later"})
	expect(t, ctrl, "error", &e)
	if e.Code != "invalid-pack" || !strings.Contains(e.Message, "not supported yet") {
		t.Fatalf("error = %+v, want unsupported feature", e)
	}

	var conf struct {
		Pack, Title string
		Tracks      int
	}
	send(r, ctrl, "configure", obj{"pack": "basic"})
	expect(t, ctrl, "configured", &conf)
	if conf.Pack != "basic" || conf.Title != "Basic" || conf.Tracks != 3 {
		t.Fatalf("configured = %+v", conf)
	}
	// Reconfiguring replaces (and closes) the previous pack.
	send(r, ctrl, "configure", obj{"pack": "basic"})
	expect(t, ctrl, "configured", nil)
}

func TestListPacks(t *testing.T) {
	r := newRoom(t, nil)
	unsupportedPack := basicManifest(1)
	unsupportedPack["rules"] = obj{"answer": obj{"rebound": "none"}}
	writePack(t, r.packsDir, "later", unsupportedPack)
	os.WriteFile(filepath.Join(r.packsDir, "broken.linospack"), []byte("not a zip"), 0o644)
	os.WriteFile(filepath.Join(r.packsDir, "notes.txt"), []byte("ignored"), 0o644)
	ctrl := newControl(t, r)

	send(r, ctrl, "list-packs", nil)
	var got struct {
		Packs []struct {
			Name, Title, Error string
		}
	}
	expect(t, ctrl, "packs", &got)
	if len(got.Packs) != 3 {
		t.Fatalf("packs = %+v, want basic, broken and later", got.Packs)
	}
	basic, broken, later := got.Packs[0], got.Packs[1], got.Packs[2]
	if basic.Name != "basic" || basic.Title != "Basic" || basic.Error != "" {
		t.Errorf("basic = %+v", basic)
	}
	if broken.Name != "broken.linospack" || broken.Error == "" {
		t.Errorf("broken = %+v, want an error", broken)
	}
	if later.Title != "Basic" || !strings.Contains(later.Error, "rules.answer.rebound") {
		t.Errorf("later = %+v, want its title and the unsupported feature", later)
	}

	empty := NewRoom(filepath.Join(t.TempDir(), "missing"))
	ctrl2 := newControl(t, empty)
	send(empty, ctrl2, "list-packs", nil)
	expect(t, ctrl2, "packs", &got)
	if len(got.Packs) != 0 {
		t.Fatalf("packs = %+v, want none without a packs folder", got.Packs)
	}
}

func TestRoomMedia(t *testing.T) {
	r := newRoom(t, nil)
	if _, err := r.Media("a.mp3"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("media before configure = %v, want not exist", err)
	}
	ctrl := newControl(t, r)
	send(r, ctrl, "configure", obj{"pack": "basic"})
	expect(t, ctrl, "configured", nil)

	f, err := r.Media("a.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if b, _ := io.ReadAll(f); string(b) != "audio" {
		t.Fatalf("media = %q, want audio", b)
	}
	if _, err := r.Media("manifest.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("manifest must not be served (it holds the answers), got %v", err)
	}
}

func TestUnsupported(t *testing.T) {
	s := func(v string) *string { return &v }
	i := func(v int) *int { return &v }
	f := func(v float64) *float64 { return &v }
	b := func(v bool) *bool { return &v }
	rules := func(r pack.Rules) *pack.Rules { return &r }

	cases := []struct {
		name string
		m    pack.Manifest
		want string
	}{
		{"round rules", pack.Manifest{Rounds: []pack.Round{{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{Attempts: i(3)}})}}}, "rounds[0].rules.answer.attempts"},
		{"attempts", pack.Manifest{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{Attempts: i(2)}})}, "rules.answer.attempts"},
		{"rebound", pack.Manifest{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{Rebound: s("none")}})}, "rules.answer.rebound"},
		{"pauseOnBuzz", pack.Manifest{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{PauseOnBuzz: b(false)}})}, "rules.answer.pauseOnBuzz"},
		{"rebound bonus", pack.Manifest{Rules: rules(pack.Rules{Scoring: &pack.Scoring{ReboundBonus: i(5)}})}, "rules.scoring.reboundBonus"},
		{"guess scoring", pack.Manifest{Tracks: []pack.Track{{Guesses: []pack.Guess{{Scoring: &pack.Scoring{ReboundBonus: i(1)}}}}}}, "tracks[0].guesses[0].scoring.reboundBonus"},
	}
	for _, tc := range cases {
		if err := unsupported(&tc.m); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: unsupported = %v, want %q", tc.name, err, tc.want)
		}
	}

	supported := pack.Manifest{
		Game: pack.Game{End: pack.End{Type: "score", Target: 500}, Tiebreak: pack.Tiebreak{Type: "sudden-death"}},
		Rules: rules(pack.Rules{
			Answer:  &pack.AnswerRules{Mode: s("simultaneous"), Via: s("device"), Attempts: i(1), Rebound: s("others"), PauseOnBuzz: b(true)},
			Owner:   &pack.OwnerRules{HeadStart: f(10), Exclusive: b(true), OthersBonus: i(20)},
			Jokers:  &pack.Jokers{Double: i(1)},
			Scoring: &pack.Scoring{Type: s("wager"), ReboundBonus: i(0)},
		}),
		Rounds: []pack.Round{{Selection: "sequence"}, {Selection: "random"}, {Selection: "theme-pick", Eliminate: 1}},
	}
	if err := unsupported(&supported); err != nil {
		t.Fatalf("supported manifest refused: %v", err)
	}
}

func TestExampleManifestIsPlayable(t *testing.T) {
	example, err := os.ReadFile("../../docs/manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"manifest.json", "media/01.mp4", "media/02.mp4", "media/03.mp3", "media/04.jpg"} {
		path := filepath.Join(dir, "example", filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, example, 0o644)
	}
	r := NewRoom(dir)
	ctrl := newControl(t, r)
	for _, control := range []string{"master", "auto"} {
		send(r, ctrl, "configure", obj{"pack": "example", "control": control})
		var conf struct{ Control string }
		expect(t, ctrl, "configured", &conf)
		if conf.Control != control {
			t.Fatalf("configured = %+v, want %s", conf, control)
		}
	}
}
