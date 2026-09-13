package game

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigure(t *testing.T) {
	r := newRoom(t, nil)
	impossible := basicManifest(1)
	impossible["rules"] = obj{"scoring": obj{"type": "rank"}}
	writePack(t, r.packsDir, "later", impossible)
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
	if e.Code != "invalid-pack" || !strings.Contains(e.Message, "rank scoring needs simultaneous answers") {
		t.Fatalf("error = %+v, want the impossible combination", e)
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
	impossible := basicManifest(1)
	impossible["rules"] = obj{"scoring": obj{"type": "rank"}}
	writePack(t, r.packsDir, "later", impossible)
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
	if later.Title != "Basic" || !strings.Contains(later.Error, "rank scoring needs simultaneous answers") {
		t.Errorf("later = %+v, want its title and the impossible combination", later)
	}

	missing := filepath.Join(t.TempDir(), "missing")
	empty := NewRoom(missing)
	if empty.PacksDir() != missing {
		t.Fatalf("PacksDir = %q, want %q", empty.PacksDir(), missing)
	}
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
