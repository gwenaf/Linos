package pack

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestUnpack(t *testing.T) {
	archive := writeFile(t, "quiz.linospack", zipBytes(t,
		zipEntry{name: "manifest.json", body: mustJSON(t, baseManifest())},
		zipEntry{name: "media/"},
		zipEntry{name: "media/a.mp3", body: "song"},
		zipEntry{name: "a.mp3", body: "song"},
	))
	dir := filepath.Join(t.TempDir(), "quiz")
	if err := Unpack(archive, dir); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "media", "a.mp3")); err != nil || string(body) != "song" {
		t.Fatalf("media/a.mp3 = %q, %v", body, err)
	}
	p, err := Open(dir)
	if err != nil {
		t.Fatalf("unpacked folder is not a valid pack: %v", err)
	}
	p.Close()
}

func TestUnpackErrors(t *testing.T) {
	expectErr(t, Unpack(writeFile(t, "x.linospack", []byte("not a zip")), filepath.Join(t.TempDir(), "x")), "zip")

	archive := writeFile(t, "ok.linospack", zipBytes(t, zipEntry{name: "a.mp3", body: "a"}))
	expectErr(t, Unpack(archive, t.TempDir()), "")

	// A crafted archive cannot write outside the folder; the partial folder is removed.
	dir := filepath.Join(t.TempDir(), "evil")
	expectErr(t, Unpack(writeFile(t, "evil.linospack", zipBytes(t, zipEntry{name: "a.mp3"}, zipEntry{name: "../escape.mp3"})), dir), "unsafe path")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("partial folder kept: %v", err)
	}

	// A file then a folder of the same name.
	clash := writeFile(t, "clash.linospack", zipBytes(t, zipEntry{name: "a", body: "x"}, zipEntry{name: "a/b.mp3"}))
	expectErr(t, Unpack(clash, filepath.Join(t.TempDir(), "clash")), "")

	// An entry compressed with a method this build cannot read.
	zip.RegisterCompressor(99, func(w io.Writer) (io.WriteCloser, error) { return nopWriteCloser{w}, nil })
	odd := writeFile(t, "odd.linospack", zipBytes(t, zipEntry{name: "a.mp3", body: "x", method: 99}))
	expectErr(t, Unpack(odd, filepath.Join(t.TempDir(), "odd")), "algorithm")
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
