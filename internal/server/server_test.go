package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gwenaf/linos/internal/game"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	New(game.NewRoom(t.TempDir())).ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Body.String() != "ok" {
		t.Fatalf("health = %q, want ok", rec.Body.String())
	}
}

func TestMedia(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "basic"), 0o755)
	os.WriteFile(filepath.Join(dir, "basic", "manifest.json"), []byte(`{"version":1,"title":"B","tracks":[
		{"id":"t1","media":"a.mp3","guesses":[{"label":"Titre","type":"text","answers":["x"]}]}]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "basic", "a.mp3"), []byte("audio"), 0o644)

	room := game.NewRoom(dir)
	ctrl := room.Connect(game.RoleControl)
	room.Receive(ctrl, game.NewMessage("join", nil))
	room.Receive(ctrl, game.NewMessage("configure", map[string]string{"pack": "basic"}))
	for m := range ctrl.Messages() {
		if m.Type == "configured" {
			break
		}
	}
	h := New(room)

	cases := []struct {
		name, path, remote string
		want               int
	}{
		{"host machine", "/media/a.mp3", "127.0.0.1:5000", http.StatusOK},
		{"phone", "/media/a.mp3", "192.168.1.20:5000", http.StatusForbidden},
		{"not in manifest", "/media/manifest.json", "127.0.0.1:5000", http.StatusNotFound},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.RemoteAddr, req.Host = tc.remote, "localhost:7777"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}
