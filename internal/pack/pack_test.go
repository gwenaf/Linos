package pack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// baseManifest is the smallest valid manifest; tests mutate a fresh copy.
func baseManifest() map[string]any {
	return map[string]any{
		"version": 1,
		"title":   "Test",
		"themes":  []any{map[string]any{"id": "fr", "name": "FR"}},
		"tracks": []any{map[string]any{
			"id":     "t1",
			"themes": []any{"fr"},
			"media":  "a.mp3",
			"guesses": []any{map[string]any{
				"label": "Titre", "type": "text", "answers": []any{"x"},
			}},
		}},
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type zipEntry struct {
	name, body string
	method     uint16
}

func zipBytes(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: e.method})
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, e.body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func expectErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %q", err, want)
	}
}

func readAll(t *testing.T, r io.ReadSeekCloser) string {
	t.Helper()
	defer r.Close()
	if _, err := r.Seek(1, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExampleManifestIsValid(t *testing.T) {
	example, err := os.ReadFile("../../docs/manifest.example.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := writeDir(t, map[string]string{
		"manifest.json": string(example),
		"media/01.mp4":  "", "media/02.mp4": "", "media/03.mp3": "", "media/04.jpg": "",
	})
	p, err := Open(dir)
	if err != nil {
		t.Fatalf("docs/manifest.example.json must stay valid: %v", err)
	}
	if p.Manifest.Title != "Années 80" || len(p.Manifest.Tracks) != 4 || p.Close() != nil {
		t.Fatalf("manifest = %+v", p.Manifest)
	}
}

func TestFolderPackMedia(t *testing.T) {
	dir := writeDir(t, map[string]string{"manifest.json": mustJSON(t, baseManifest()), "a.mp3": "audio"})
	p, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	r, err := p.Media("a.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r); got != "udio" {
		t.Fatalf("media after seek = %q, want udio", got)
	}
	if _, err := p.Media("missing.mp3"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing media error = %v, want not exist", err)
	}
	if _, err := p.Media("../a.mp3"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("escaping path error = %v, want invalid", err)
	}
}

func TestZipPackMedia(t *testing.T) {
	path := writeFile(t, "test.linospack", zipBytes(t,
		zipEntry{"manifest.json", mustJSON(t, baseManifest()), zip.Deflate},
		zipEntry{"a.mp3", "audio", zip.Store},
	))
	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	r, err := p.Media("a.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, r); got != "udio" {
		t.Fatalf("media after seek = %q, want udio", got)
	}
	if _, err := p.Media("missing.mp3"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing media error = %v, want not exist", err)
	}
	if _, err := p.Media("manifest.json"); err == nil {
		t.Fatal("compressed entries cannot be served in place")
	}
	if _, err := p.Media("C:/a.mp3"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("absolute path error = %v, want invalid", err)
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing path error = %v", err)
	}
	_, err := Open(writeFile(t, "notes.txt", []byte("hello")))
	expectErr(t, err, "neither a folder nor a zip archive")

	_, err = Open(t.TempDir())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("folder without manifest error = %v", err)
	}
	_, err = Open(writeDir(t, map[string]string{"manifest.json": "{"}))
	expectErr(t, err, "manifest.json")
	_, err = Open(writeDir(t, map[string]string{"manifest.json": `{"surprise": true}`}))
	expectErr(t, err, `unknown field "surprise"`)
}

func TestZipMediaChecks(t *testing.T) {
	manifest := zipEntry{"manifest.json", mustJSON(t, baseManifest()), zip.Deflate}

	_, err := Open(writeFile(t, "compressed.linospack", zipBytes(t, manifest, zipEntry{"a.mp3", "audio", zip.Deflate})))
	expectErr(t, err, "must be stored uncompressed")

	_, err = Open(writeFile(t, "missing.linospack", zipBytes(t, manifest)))
	expectErr(t, err, `"a.mp3" not found`)

	// Break the local header of the media entry; the central directory still lists it.
	data := zipBytes(t, manifest, zipEntry{"a.mp3", "audio", zip.Store})
	sig := []byte("PK\x03\x04")
	second := bytes.Index(data[1:], sig) + 1
	second += bytes.Index(data[second+1:], sig) + 1
	copy(data[second:], "XXXX")
	_, err = Open(writeFile(t, "corrupt.linospack", data))
	expectErr(t, err, "is corrupted")
}
