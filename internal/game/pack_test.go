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
	unsupportedPack["game"] = obj{"tiebreak": obj{"type": "sudden-death"}}
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
	unsupportedPack["rules"] = obj{"jokers": obj{"double": 1}}
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
	if later.Title != "Basic" || !strings.Contains(later.Error, "rules.jokers") {
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
		{"end", pack.Manifest{Game: pack.Game{End: pack.End{Type: "score"}}}, `game.end.type "score"`},
		{"tiebreak", pack.Manifest{Game: pack.Game{Tiebreak: pack.Tiebreak{Type: "sudden-death"}}}, "game.tiebreak"},
		{"theme-pick", pack.Manifest{Rounds: []pack.Round{{Selection: "theme-pick"}}}, "rounds[0].selection theme-pick"},
		{"eliminate", pack.Manifest{Rounds: []pack.Round{{Eliminate: 1}}}, "rounds[0].eliminate"},
		{"round rules", pack.Manifest{Rounds: []pack.Round{{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{Mode: s("simultaneous")}})}}}, "rounds[0].rules.answer.mode"},
		{"via", pack.Manifest{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{Via: s("device")}})}, "rules.answer.via"},
		{"attempts", pack.Manifest{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{Attempts: i(2)}})}, "rules.answer.attempts"},
		{"rebound", pack.Manifest{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{Rebound: s("none")}})}, "rules.answer.rebound"},
		{"pauseOnBuzz", pack.Manifest{Rules: rules(pack.Rules{Answer: &pack.AnswerRules{PauseOnBuzz: b(false)}})}, "rules.answer.pauseOnBuzz"},
		{"owner", pack.Manifest{Rules: rules(pack.Rules{Owner: &pack.OwnerRules{HeadStart: f(10)}})}, "rules.owner"},
		{"jokers", pack.Manifest{Rules: rules(pack.Rules{Jokers: &pack.Jokers{Double: i(1)}})}, "rules.jokers"},
		{"scoring type", pack.Manifest{Rules: rules(pack.Rules{Scoring: &pack.Scoring{Type: s("rank")}})}, "rules.scoring.type"},
		{"rebound bonus", pack.Manifest{Rules: rules(pack.Rules{Scoring: &pack.Scoring{ReboundBonus: i(5)}})}, "rules.scoring.reboundBonus"},
		{"guess scoring", pack.Manifest{Tracks: []pack.Track{{Guesses: []pack.Guess{{Scoring: &pack.Scoring{Type: s("wager")}}}}}}, "tracks[0].guesses[0].scoring.type"},
	}
	for _, tc := range cases {
		if err := unsupported(&tc.m); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: unsupported = %v, want %q", tc.name, err, tc.want)
		}
	}

	supported := pack.Manifest{
		Game: pack.Game{End: pack.End{Type: "rounds"}},
		Rules: rules(pack.Rules{
			Answer:  &pack.AnswerRules{Mode: s("buzz"), Via: s("oral"), Attempts: i(1), Rebound: s("others"), PauseOnBuzz: b(true)},
			Owner:   &pack.OwnerRules{HeadStart: f(0), Exclusive: b(false), OthersBonus: i(0)},
			Jokers:  &pack.Jokers{Double: i(0)},
			Scoring: &pack.Scoring{Type: s("fixed"), ReboundBonus: i(0)},
		}),
		Rounds: []pack.Round{{Selection: "sequence"}, {Selection: "random"}},
	}
	if err := unsupported(&supported); err != nil {
		t.Fatalf("supported manifest refused: %v", err)
	}
}
