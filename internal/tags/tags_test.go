package tags

import (
	"bytes"
	"mime/multipart"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gwenaf/linos/internal/pack"
)

// id3 builds a minimal ID3v2.3 tag with a title and an artist, followed by fake audio bytes.
func id3(title, artist string) []byte {
	frame := func(id, text string) []byte {
		body := append([]byte{0}, text...) // encoding byte 0: ISO-8859-1
		n := len(body)
		return append(append([]byte(id), byte(n>>24), byte(n>>16), byte(n>>8), byte(n), 0, 0), body...)
	}
	frames := append(frame("TIT2", title), frame("TPE1", artist)...)
	n := len(frames)
	header := []byte{'I', 'D', '3', 3, 0, 0, byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
	return append(append(header, frames...), make([]byte, 64)...)
}

type upload struct {
	name string
	data []byte
}

// body encodes files as a multipart upload; an empty name adds a plain form field.
func body(t *testing.T, files ...upload) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range files {
		if f.name == "" {
			w.WriteField("note", "ignored")
			continue
		}
		part, err := w.CreateFormFile("files", f.name)
		if err != nil {
			t.Fatal(err)
		}
		part.Write(f.data)
	}
	w.Close()
	return &buf, w.Boundary()
}

func reader(buf *bytes.Buffer, boundary string) *multipart.Reader {
	return multipart.NewReader(buf, boundary)
}

func TestImport(t *testing.T) {
	packs := filepath.Join(t.TempDir(), "packs")
	buf, boundary := body(t,
		upload{"Album/01 - Queen - Bohemian Rhapsody.mp3", []byte("no tags")},
		upload{"tagged.mp3", id3("Take On Me (Remastered 2015)", "a-ha feat. Nobody")},
		upload{"wh:at?.ogg", []byte("x")},
		upload{"same.flac", []byte("1")},
		upload{"same.flac", []byte("2")},
		upload{"notes.txt", []byte("not audio")},
		upload{"", nil},
	)
	res, err := Import(packs, reader(buf, boundary), Options{Start: 20, Duration: 15, Via: "device"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Pack, "import-") || res.Tracks != 5 || !reflect.DeepEqual(res.Skipped, []string{"notes.txt"}) {
		t.Fatalf("result = %+v", res)
	}

	p, err := pack.Open(filepath.Join(packs, res.Pack))
	if err != nil {
		t.Fatalf("the generated pack must load: %v", err)
	}
	defer p.Close()
	m := p.Manifest
	if *m.Rules.Duration != 15 || *m.Rules.Answer.Via != "device" {
		t.Fatalf("rules = %+v", m.Rules)
	}
	byMedia := map[string]pack.Track{}
	for _, tr := range m.Tracks {
		byMedia[tr.Media] = tr
	}
	queen := byMedia["01 - Queen - Bohemian Rhapsody.mp3"]
	if queen.Start != 20 || queen.Guesses[0].Answers[0] != "Queen" || queen.Guesses[1].Answers[0] != "Bohemian Rhapsody" {
		t.Fatalf("file name fallback = %+v", queen)
	}
	tagged := byMedia["tagged.mp3"]
	if !reflect.DeepEqual(tagged.Guesses[0].Answers, []string{"a-ha feat. Nobody", "a-ha"}) ||
		!reflect.DeepEqual(tagged.Guesses[1].Answers, []string{"Take On Me (Remastered 2015)", "Take On Me"}) {
		t.Fatalf("tags = %+v", tagged.Guesses)
	}
	if _, ok := byMedia["wh_at_.ogg"]; !ok {
		t.Fatalf("unsafe characters must be replaced: %v", byMedia)
	}
	if len(byMedia["same-2.flac"].Guesses) != 1 {
		t.Fatalf("duplicate names must not overwrite: %v", byMedia)
	}
}

func TestImportDefaults(t *testing.T) {
	packs := t.TempDir()
	buf, boundary := body(t, upload{"song.wav", []byte("x")})
	res, err := Import(packs, reader(buf, boundary), Options{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := pack.Open(filepath.Join(packs, res.Pack))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if r := p.Manifest.Rules; r.Duration != nil || r.Answer != nil {
		t.Fatalf("rules = %+v, want the defaults", r)
	}
}

func TestImportErrors(t *testing.T) {
	packs := t.TempDir()
	buf, boundary := body(t, upload{"notes.txt", []byte("x")})
	if _, err := Import(packs, reader(buf, boundary), Options{}); err == nil || !strings.Contains(err.Error(), "no audio file") {
		t.Fatalf("no audio: %v", err)
	}
	if entries, _ := os.ReadDir(packs); len(entries) != 0 {
		t.Fatalf("a failed import must leave nothing behind, found %v", entries)
	}

	buf, boundary = body(t, upload{"song.mp3", bytes.Repeat([]byte("x"), 1000)})
	truncated := bytes.NewBuffer(buf.Bytes()[:buf.Len()-600])
	if _, err := Import(packs, reader(truncated, boundary), Options{}); err == nil {
		t.Fatal("a truncated upload must fail")
	}
	if _, err := Import(packs, multipart.NewReader(strings.NewReader("--b\r\nbroken"), "b"), Options{}); err == nil {
		t.Fatal("a malformed upload must fail")
	}

	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o644)
	if _, err := Import(file, reader(body(t, upload{"a.mp3", nil})), Options{}); err == nil {
		t.Fatal("packs folder that is a file must fail")
	}
}

func TestNewPackDirAvoidsCollisions(t *testing.T) {
	packs := t.TempDir()
	_, first, _ := newPackDir(packs, "import")
	_, second, err := newPackDir(packs, "import")
	if first != "import" || second != "import-2" || err != nil {
		t.Fatalf("names = %q, %q (%v)", first, second, err)
	}
}

func TestRead(t *testing.T) {
	if got := read(filepath.Join(t.TempDir(), "Artist - Song.mp3")); got != (info{title: "Song", artist: "Artist"}) {
		t.Fatalf("missing file = %+v, want the file name", got)
	}
	cases := map[string]info{
		"07. Title.mp3":        {title: "Title"},
		"01.mp3":               {title: "01"},
		"12_Artist - Song.ogg": {title: "Song", artist: "Artist"},
	}
	for name, want := range cases {
		if got := fromName(name); got != want {
			t.Errorf("fromName(%q) = %+v, want %+v", name, got, want)
		}
	}
}
