package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gwenaf/linos/internal/game"
)

func TestEditorOff(t *testing.T) {
	check(t, New(game.NewRoom(t.TempDir()), web, false), []request{
		{"page", "/edit", local, http.StatusNotFound, ""},
		{"api", "/api/edit", local, http.StatusNotFound, ""},
	})
}

func TestEditor(t *testing.T) {
	packs := filepath.Join(t.TempDir(), "packs")
	h := New(game.NewRoom(packs), web, true)
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
		{"no packs folder yet", "/api/edit", local, http.StatusOK, "[]"},
		{"unknown pack", "/api/edit/nope", local, http.StatusNotFound, "no folder pack"},
		{"invalid pack name", "/api/edit/a:b", local, http.StatusNotFound, "no folder pack"},
	})

	want(do("POST", "/api/edit", "application/json", `{"name":"../evil"}`), http.StatusBadRequest, "invalid pack name")
	want(do("POST", "/api/edit", "application/json", `{"name":"quiz"}`), http.StatusCreated, "")
	want(do("POST", "/api/edit", "application/json", `{"name":"quiz"}`), http.StatusConflict, "cannot create pack")
	// Only folders holding a manifest are listed: not archives nor stray folders.
	os.WriteFile(filepath.Join(packs, "old.linospack"), []byte("zip"), 0o644)
	os.Mkdir(filepath.Join(packs, "stray"), 0o755)
	want(do("GET", "/api/edit", "", ""), http.StatusOK, `["quiz"]`)

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
	want(do("POST", "/api/edit/quiz/media", "text/plain", "x"), http.StatusBadRequest, "multipart")
	want(do("POST", "/api/edit/quiz/media", "multipart/form-data; boundary=b", "--b\r\nbroken"), http.StatusBadRequest, "")
	want(do("GET", "/api/edit/quiz/media/Artist%20-%20Song.mp3", "", ""), http.StatusOK, "audio")
	want(do("GET", "/api/edit/quiz", "", ""), http.StatusOK, `"media":["Artist - Song.mp3"]`)

	manifest := `{"version":1,"title":"Quiz","tracks":[{"id":"t1","media":"Artist - Song.mp3",` +
		`"guesses":[{"label":"Titre","type":"text","answers":["Song"]}]}]}`
	want(do("PUT", "/api/edit/quiz", "application/json", manifest), http.StatusOK, `"problems":[]`)
	saved, _ := os.ReadFile(filepath.Join(packs, "quiz", "manifest.json"))
	if !strings.Contains(string(saved), "\n  \"title\": \"Quiz\"") {
		t.Fatalf("manifest not saved indented: %s", saved)
	}
	want(do("PUT", "/api/edit/quiz", "application/json", `{"titel":"typo"}`), http.StatusBadRequest, "unknown field")
	want(do("PUT", "/api/edit/quiz", "application/json", `{} trailing`), http.StatusBadRequest, "invalid character")

	// A folder whose manifest.json cannot be read or written.
	os.MkdirAll(filepath.Join(packs, "broken", "manifest.json"), 0o755)
	want(do("GET", "/api/edit/broken", "", ""), http.StatusInternalServerError, "")
	want(do("PUT", "/api/edit/broken", "application/json", `{}`), http.StatusInternalServerError, "")
}
