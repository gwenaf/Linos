package pack

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeFFmpeg records the calls and writes the output file ffmpeg would; fail makes it fail.
func fakeFFmpeg(t *testing.T, calls *[][]string, fail error) {
	t.Helper()
	old := FFmpeg
	t.Cleanup(func() { FFmpeg = old })
	FFmpeg = func(args ...string) ([]byte, error) {
		*calls = append(*calls, args)
		if fail != nil && args[0] != "-version" {
			return []byte("bad input\n"), fail
		}
		if len(args) > 1 {
			os.WriteFile(args[len(args)-1], []byte("cut"), 0o644)
		}
		return nil, nil
	}
}

func exportManifest() obj {
	m := baseManifest()
	m["cover"] = "cover.jpg"
	m["rules"] = obj{"duration": 20}
	m["rounds"] = list{obj{"selection": "sequence", "tracks": list{"t1", "clip"}, "rules": obj{"duration": 25}}}
	track := firstTrack(m)
	track["start"], track["outro"] = 10, 5
	m["tracks"] = append(m["tracks"].(list),
		obj{"id": "clip", "media": "clip.mp4", "start": 3, "duration": 8, "playbackRate": 2,
			"reveal":  list{obj{"image": "pic.png"}, obj{"at": 4, "image": ""}},
			"guesses": list{obj{"label": "Titre", "type": "text", "answers": list{"x"}}}},
		obj{"id": "photo", "media": "photo.jpg",
			"guesses": list{obj{"label": "Titre", "type": "text", "answers": list{"x"}}}},
	)
	return m
}

func exportDir(t *testing.T) string {
	return writeDir(t, map[string]string{
		"manifest.json": mustJSON(t, exportManifest()),
		"a.mp3":         "song", "clip.mp4": "video", "photo.jpg": "p", "pic.png": "i", "cover.jpg": "c", "unused.mp3": "u",
	})
}

func TestExportCuts(t *testing.T) {
	var calls [][]string
	fakeFFmpeg(t, &calls, nil)
	dest := filepath.Join(t.TempDir(), "quiz.linospack")
	cut, err := Export(exportDir(t), dest)
	if err != nil || !cut {
		t.Fatalf("export = %v, %v", cut, err)
	}
	// The audio track plays 25 s at most (its round) plus a 5 s outro; the clip 8 s at double speed.
	joined := func(i int) string { return strings.Join(calls[i], " ") }
	if len(calls) != 3 || !strings.Contains(joined(1), "-ss 10.000") || !strings.Contains(joined(1), "-t 30.000 -vn -c:a copy") ||
		!strings.Contains(joined(2), "-ss 3.000") || !strings.Contains(joined(2), "-t 16.000 -c:v libx264") {
		t.Fatalf("ffmpeg calls = %q", calls)
	}

	p, err := Open(dest)
	if err != nil {
		t.Fatalf("exported pack does not open: %v", err)
	}
	defer p.Close()
	tracks := p.Manifest.Tracks
	if tracks[0].Media != "cut/001.mp3" || tracks[0].Start != 0 || tracks[1].Media != "cut/002.mp4" || tracks[2].Media != "photo.jpg" {
		t.Fatalf("tracks = %+v", tracks)
	}
	var names []string
	for name := range p.zipFiles {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"cover.jpg", "cut/001.mp3", "cut/002.mp4", "manifest.json", "photo.jpg", "pic.png"}; !slices.Equal(names, want) {
		t.Fatalf("archive = %q, want %q (unused media left out)", names, want)
	}
}

func TestExportWithoutFFmpeg(t *testing.T) {
	old := FFmpeg
	t.Cleanup(func() { FFmpeg = old })
	FFmpeg = func(args ...string) ([]byte, error) { return nil, errors.New("not found") }
	dest := filepath.Join(t.TempDir(), "quiz.linospack")
	cut, err := Export(exportDir(t), dest)
	if err != nil || cut {
		t.Fatalf("export = %v, %v, want media copied whole", cut, err)
	}
	p, err := Open(dest)
	if err != nil || p.Manifest.Tracks[0].Media != "a.mp3" || p.Manifest.Tracks[0].Start != 10 {
		t.Fatalf("exported pack = %v, %+v", err, p.Manifest.Tracks)
	}
	p.Close()
}

func TestExportErrors(t *testing.T) {
	var calls [][]string
	fakeFFmpeg(t, &calls, errors.New("exit status 1"))
	dest := filepath.Join(t.TempDir(), "quiz.linospack")

	_, err := Export(writeDir(t, map[string]string{"manifest.json": "{}"}), dest)
	expectErr(t, err, "title is required")
	_, err = Export(exportDir(t), filepath.Join(t.TempDir(), "missing", "quiz.linospack"))
	expectErr(t, err, "missing")
	_, err = Export(exportDir(t), dest)
	expectErr(t, err, "ffmpeg on track t1: exit status 1: bad input")

	// ffmpeg exits fine but writes nothing.
	FFmpeg = func(args ...string) ([]byte, error) { return nil, nil }
	_, err = Export(exportDir(t), dest)
	expectErr(t, err, "001.mp3")

	// The archive cannot be created in the work folder.
	FFmpeg = func(args ...string) ([]byte, error) {
		if len(args) > 1 {
			out := args[len(args)-1]
			os.WriteFile(out, nil, 0o644)
			os.Mkdir(filepath.Join(filepath.Dir(filepath.Dir(out)), "pack.linospack"), 0o755)
		}
		return nil, nil
	}
	_, err = Export(exportDir(t), dest)
	expectErr(t, err, "pack.linospack")

	// The destination cannot be replaced.
	fakeFFmpeg(t, &calls, nil)
	os.Mkdir(dest, 0o755)
	os.WriteFile(filepath.Join(dest, "keep"), nil, 0o644)
	if _, err = Export(exportDir(t), dest); err == nil {
		t.Fatal("export over a folder must fail")
	}
}

func TestFFmpegCommand(t *testing.T) {
	// Runs the real command: fails when ffmpeg is not installed, prints its version otherwise.
	out, err := FFmpeg("-version")
	if err == nil && !strings.Contains(string(out), "ffmpeg") {
		t.Fatalf("ffmpeg -version = %q", out)
	}
}
