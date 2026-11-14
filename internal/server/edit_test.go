package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/pack"
)

func TestEditorOff(t *testing.T) {
	check(t, New(game.NewRoom(t.TempDir()), web, Options{}), []request{
		{"page", "/edit", local, http.StatusNotFound, ""},
		{"api", "/api/edit", local, http.StatusNotFound, ""},
		{"import", "/api/import", local, http.StatusNotFound, ""},
	})
}

func TestEditor(t *testing.T) {
	packs := filepath.Join(t.TempDir(), "packs")
	h := New(game.NewRoom(packs), web, Options{Edit: true})
	do := func(method, path, contentType, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr, req.Host = local, "localhost:7777"
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	want := func(rec *httptest.ResponseRecorder, code int, body string) {
		t.Helper()
		if rec.Code != code || !strings.Contains(rec.Body.String(), body) {
			t.Fatalf("got %d %q, want %d containing %q", rec.Code, rec.Body, code, body)
		}
	}

	check(t, h, []request{
		{"page", "/edit", local, http.StatusOK, "id=app"},
		{"page from a phone", "/edit", phone, http.StatusForbidden, ""},
		{"no packs folder yet", "/api/edit", local, http.StatusOK, `{"archives":[],"folders":[]}`},
		{"unknown pack", "/api/edit/nope", local, http.StatusNotFound, "no folder pack"},
		{"invalid pack name", "/api/edit/a:b", local, http.StatusNotFound, "no folder pack"},
	})

	want(do("POST", "/api/edit", "application/json", `{"name":"../evil"}`), http.StatusBadRequest, "invalid pack name")
	want(do("POST", "/api/edit", "application/json", `{"name":"quiz"}`), http.StatusCreated, "")
	want(do("POST", "/api/edit", "application/json", `{"name":"quiz"}`), http.StatusConflict, "cannot create pack")
	// Only folders holding a manifest are listed: not archives nor stray folders.
	os.WriteFile(filepath.Join(packs, "old.linospack"), []byte("zip"), 0o644)
	os.Mkdir(filepath.Join(packs, "stray"), 0o755)
	want(do("GET", "/api/edit", "", ""), http.StatusOK, `{"archives":["old.linospack"],"folders":["quiz"]}`)

	// A new pack is saved but not playable yet.
	want(do("GET", "/api/edit/quiz", "", ""), http.StatusOK, "at least one track is required")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, name := range []string{"Artist - Song.mp3", "notes.txt"} {
		part, _ := mw.CreateFormFile("files", name)
		part.Write([]byte("audio"))
	}
	mw.Close()
	rec := do("POST", "/api/edit/quiz/media", mw.FormDataContentType(), buf.String())
	want(rec, http.StatusOK, `"skipped":["notes.txt"]`)
	var up struct {
		Uploads []struct {
			Media   string
			Guesses []struct{ Label string }
		}
	}
	json.Unmarshal(rec.Body.Bytes(), &up)
	if len(up.Uploads) != 1 || up.Uploads[0].Media != "Artist - Song.mp3" || len(up.Uploads[0].Guesses) != 2 {
		t.Fatalf("uploads = %+v, want the mp3 with artist and title guesses", up)
	}
	// The same song again is not copied: the existing file comes back, flagged.
	buf.Reset()
	mw = multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("files", "copy.mp3")
	part.Write([]byte("audio"))
	mw.Close()
	want(do("POST", "/api/edit/quiz/media", mw.FormDataContentType(), buf.String()), http.StatusOK, `"duplicate":true`)
	if _, err := os.Stat(filepath.Join(packs, "quiz", "copy.mp3")); !os.IsNotExist(err) {
		t.Fatalf("a duplicate upload must not be kept: %v", err)
	}
	want(do("POST", "/api/edit/quiz/media", "text/plain", "x"), http.StatusBadRequest, "multipart")
	want(do("POST", "/api/edit/quiz/media", "multipart/form-data; boundary=b", "--b\r\nbroken"), http.StatusBadRequest, "")
	rec = do("GET", "/api/edit/quiz/media/Artist%20-%20Song.mp3", "", "")
	want(rec, http.StatusOK, "audio")
	if rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatal("pack media must be served sandboxed")
	}
	// Only media: an unpacked page or the manifest is never served.
	os.WriteFile(filepath.Join(packs, "quiz", "page.html"), []byte("<script>"), 0o644)
	want(do("GET", "/api/edit/quiz/media/page.html", "", ""), http.StatusNotFound, "")
	want(do("GET", "/api/edit/quiz", "", ""), http.StatusOK, `"media":["Artist - Song.mp3"]`)

	manifest := `{"version":1,"title":"Quiz","tracks":[{"id":"t1","media":"Artist - Song.mp3",` +
		`"guesses":[{"label":"Titre","type":"text","answers":["Song"]}]}]}`
	want(do("PUT", "/api/edit/quiz", "application/json", manifest), http.StatusOK, `"problems":[]`)
	saved, _ := os.ReadFile(filepath.Join(packs, "quiz", "manifest.json"))
	if !strings.Contains(string(saved), "\n  \"title\": \"Quiz\"") {
		t.Fatalf("manifest not saved indented: %s", saved)
	}
	// Two editors: a save from the current version passes, one from an older version is refused with
	// the current version; an unchanged pack polls as 304.
	cond := func(method, header, tag, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/edit/quiz", strings.NewReader(body))
		req.RemoteAddr, req.Host = local, "localhost:7777"
		req.Header.Set(header, tag)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	v1 := do("GET", "/api/edit/quiz", "", "").Header().Get("ETag")
	want(cond("GET", "If-None-Match", v1, ""), http.StatusNotModified, "")
	saved2 := cond("PUT", "If-Match", v1, manifest)
	want(saved2, http.StatusOK, `"problems":[]`)
	v2 := saved2.Header().Get("ETag")
	if v2 != v1 { // same manifest written again: same version
		t.Fatalf("version changed from %s to %s for the same manifest", v1, v2)
	}
	other := strings.Replace(manifest, `"Quiz"`, `"Quiz 2"`, 1)
	v3 := cond("PUT", "If-Match", v2, other).Header().Get("ETag")
	stale := cond("PUT", "If-Match", v2, manifest)
	want(stale, http.StatusPreconditionFailed, "another editor")
	if stale.Header().Get("ETag") != v3 {
		t.Fatalf("a refused save must give the current version %s, got %s", v3, stale.Header().Get("ETag"))
	}
	want(cond("GET", "If-None-Match", v1, ""), http.StatusOK, "Quiz 2")
	want(do("PUT", "/api/edit/quiz", "application/json", manifest), http.StatusOK, `"problems":[]`)
	want(do("PUT", "/api/edit/quiz", "application/json", `{"titel":"typo"}`), http.StatusBadRequest, "unknown field")
	want(do("PUT", "/api/edit/quiz", "application/json", `{} trailing`), http.StatusBadRequest, "invalid character")

	// Export without ffmpeg: media copied whole, archive next to the folder.
	old := pack.FFmpeg
	t.Cleanup(func() { pack.FFmpeg = old })
	pack.FFmpeg = func(...string) ([]byte, error) { return nil, errors.New("not found") }
	want(do("POST", "/api/edit/quiz/export", "", ""), http.StatusOK, `"cut":false,"file":"quiz.linospack"`)
	if _, err := os.Stat(filepath.Join(packs, "quiz.linospack")); err != nil {
		t.Fatalf("archive not written: %v", err)
	}
	// The archive unpacks into a new folder, numbered since "quiz" exists.
	want(do("POST", "/api/edit/unpack", "application/json", `{"name":"quiz.linospack"}`), http.StatusOK, `"name":"quiz-2"`)
	want(do("GET", "/api/edit/quiz-2", "", ""), http.StatusOK, `"problems":[]`)
	want(do("POST", "/api/edit/unpack", "application/json", `{"name":"quiz"}`), http.StatusBadRequest, "invalid archive name")
	want(do("POST", "/api/edit/unpack", "application/json", `{"name":"../x.linospack"}`), http.StatusBadRequest, "invalid archive name")
	want(do("POST", "/api/edit/unpack", "application/json", `{"name":"old.linospack"}`), http.StatusBadRequest, "zip")

	// Download the archive, then send it back as someone receiving it would: a valid one is added,
	// an invalid one is removed, other files are skipped, and a file already there is not copied again.
	rec = do("GET", "/api/edit/archives/quiz.linospack", "", "")
	want(rec, http.StatusOK, "")
	if rec.Header().Get("Content-Disposition") != `attachment; filename="quiz.linospack"` {
		t.Fatalf("download headers = %v", rec.Header())
	}
	archive := rec.Body.Bytes()
	want(do("GET", "/api/edit/archives/quiz", "", ""), http.StatusNotFound, "")
	want(do("GET", "/api/edit/archives/a:b.linospack", "", ""), http.StatusNotFound, "")
	os.Remove(filepath.Join(packs, "quiz.linospack"))
	sendArchives := func(files map[string][]byte) *httptest.ResponseRecorder {
		buf.Reset()
		mw = multipart.NewWriter(&buf)
		for name, body := range files {
			part, _ := mw.CreateFormFile("files", name)
			part.Write(body)
		}
		mw.Close()
		return do("POST", "/api/edit/archives", mw.FormDataContentType(), buf.String())
	}
	rec = sendArchives(map[string][]byte{"received.linospack": archive, "fake.linospack": []byte("not a pack"), "notes.txt": nil})
	for _, part := range []string{`"received.linospack"`, `"fake.linospack":"`, `"skipped":["notes.txt"]`} {
		want(rec, http.StatusOK, part)
	}
	if _, err := os.Stat(filepath.Join(packs, "fake.linospack")); !os.IsNotExist(err) {
		t.Fatalf("invalid archive kept: %v", err)
	}
	want(sendArchives(map[string][]byte{"again.linospack": archive}), http.StatusOK, `"existing":["received.linospack"],"invalid":{},"saved":[]`)
	os.Rename(filepath.Join(packs, "received.linospack"), filepath.Join(packs, "quiz.linospack")) // the export, as before
	want(do("POST", "/api/edit/archives", "text/plain", "x"), http.StatusBadRequest, "multipart")
	want(do("POST", "/api/edit/archives", "multipart/form-data; boundary=b", "--b\r\nbroken"), http.StatusBadRequest, "")

	want(do("PUT", "/api/edit/quiz", "application/json", `{"version":1,"title":""}`), http.StatusOK, "title is required")
	want(do("POST", "/api/edit/quiz/export", "", ""), http.StatusBadRequest, "title is required")

	// Reveal in the file manager, per platform; the archive only once exported.
	var started []string
	oldStart, oldOS := Start, goos
	t.Cleanup(func() { Start, goos = oldStart, oldOS })
	Start = func(name string, args ...string) error {
		started = append(started, name+" "+strings.Join(args, " "))
		return nil
	}
	for _, os := range []string{"windows", "darwin", "linux"} {
		goos = os
		want(do("POST", "/api/edit/quiz/reveal", "", ""), http.StatusNoContent, "")
	}
	quiz := filepath.Join(packs, "quiz")
	if want := []string{"explorer /select," + quiz, "open -R " + quiz, "xdg-open " + packs}; strings.Join(started, "|") != strings.Join(want, "|") {
		t.Fatalf("started %q, want %q", started, want)
	}
	goos = "windows"
	want(do("POST", "/api/edit/quiz/reveal?archive=1", "", ""), http.StatusNoContent, "")
	if started[3] != "explorer /select,"+quiz+".linospack" {
		t.Fatalf("archive reveal = %q", started[3])
	}
	want(do("POST", "/api/edit/stray/reveal?archive=1", "", ""), http.StatusInternalServerError, "")

	// A folder whose manifest.json cannot be read or written.
	os.MkdirAll(filepath.Join(packs, "broken", "manifest.json"), 0o755)
	want(do("GET", "/api/edit/broken", "", ""), http.StatusInternalServerError, "")
	want(do("PUT", "/api/edit/broken", "application/json", `{}`), http.StatusInternalServerError, "")
}
