package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/network"
)

var web = fstest.MapFS{
	"index.html":    {Data: []byte("<div id=app></div>")},
	"assets/app.js": {Data: []byte("console.log('linos')")},
}

type request struct {
	name, path, remote string
	want               int
	body               string
}

func check(t *testing.T, h http.Handler, cases []request) {
	t.Helper()
	for _, tc := range cases {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.RemoteAddr, req.Host = tc.remote, "localhost:7777"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.body) {
			t.Errorf("%s: %d %q, want %d containing %q", tc.name, rec.Code, rec.Body.String(), tc.want, tc.body)
		}
	}
}

const local, phone = "127.0.0.1:5000", "192.168.1.20:5000"

func TestPages(t *testing.T) {
	h := New(game.NewRoom(t.TempDir()), web)
	check(t, h, []request{
		{"health", "/health", phone, http.StatusOK, "ok"},
		{"control page", "/control", local, http.StatusOK, "id=app"},
		{"host page", "/host", local, http.StatusOK, "id=app"},
		{"play page", "/play", phone, http.StatusOK, "id=app"},
		{"asset", "/assets/app.js", phone, http.StatusOK, "linos"},
		{"root on host", "/", local, http.StatusFound, "/control"},
		{"root on phone", "/", phone, http.StatusFound, "/play"},
	})
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
	check(t, New(room, web), []request{
		{"host machine", "/media/a.mp3", local, http.StatusOK, "audio"},
		{"phone", "/media/a.mp3", phone, http.StatusForbidden, ""},
		{"not in manifest", "/media/manifest.json", local, http.StatusNotFound, ""},
	})
}

func TestJoinAndQR(t *testing.T) {
	h := New(game.NewRoom(t.TempDir()), web)
	check(t, h, []request{
		{"join from phone", "/api/join", phone, http.StatusForbidden, ""},
		{"qr", "/qr.png?data=http://192.168.1.10:7777/play", local, http.StatusOK, "PNG"},
		{"qr without data", "/qr.png", local, http.StatusBadRequest, ""},
		{"qr from phone", "/qr.png?data=x", phone, http.StatusForbidden, ""},
	})

	for host, port := range map[string]string{"localhost:7777": ":7777/play", "localhost": ":80/play"} {
		req := httptest.NewRequest("GET", "/api/join", nil)
		req.RemoteAddr, req.Host = local, host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var got struct{ URLs []string }
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("join: %v (%s)", err, rec.Body)
		}
		for _, u := range got.URLs {
			if !strings.HasPrefix(u, "http://") || !strings.HasSuffix(u, port) {
				t.Errorf("Host %s: url %q, want http://<ip>%s", host, u, port)
			}
		}
	}
}

func TestFrontErrorReport(t *testing.T) {
	h := New(game.NewRoom(t.TempDir()), web)
	post := func(body string) int {
		req := httptest.NewRequest("POST", "/api/log", strings.NewReader(body))
		req.RemoteAddr = phone
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(`{"page":"/play","message":"x is undefined","stack":"at play.tsx:1"}`); code != http.StatusNoContent {
		t.Errorf("valid report: %d, want 204", code)
	}
	if code := post("not json"); code != http.StatusBadRequest {
		t.Errorf("invalid report: %d, want 400", code)
	}
	if code := post(`{"message":"` + strings.Repeat("a", maxFrontReport) + `"}`); code != http.StatusBadRequest {
		t.Errorf("oversized report: %d, want 400", code)
	}
}

func TestWebSocketThroughRequestLog(t *testing.T) {
	srv := httptest.NewServer(New(game.NewRoom(t.TempDir()), web))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("upgrade through the request logger: %v", err)
	}
	defer c.CloseNow()
	if err := wsjson.Write(ctx, c, game.NewMessage("join", nil)); err != nil {
		t.Fatal(err)
	}
	var m game.Message
	if err := wsjson.Read(ctx, c, &m); err != nil || m.Type != "welcome" {
		t.Fatalf("read = %v %v, want welcome", m, err)
	}
}

func TestNetworkHelpers(t *testing.T) {
	prev := network.Command
	t.Cleanup(func() { network.Command = prev })
	var fail error
	network.Command = func(string, ...string) ([]byte, error) { return []byte("Public"), fail }
	h := New(game.NewRoom(t.TempDir()), web)

	check(t, h, []request{{"network info", "/api/network", local, http.StatusOK, `"hotspot":"192.168.137."`}})

	firewall := func(remote, origin string) int {
		req := httptest.NewRequest("POST", "/api/firewall", nil)
		req.RemoteAddr, req.Host = remote, "localhost:7777"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := firewall(phone, ""); code != http.StatusForbidden {
		t.Errorf("from phone: %d, want 403", code)
	}
	if code := firewall(local, "http://evil.example"); code != http.StatusForbidden {
		t.Errorf("cross-origin: %d, want 403", code)
	}
	if code := firewall(local, "http://localhost:7777"); code != http.StatusNoContent && runtime.GOOS == "windows" {
		t.Errorf("same origin: %d, want 204", code)
	}
	fail = errors.New("UAC refused")
	if code := firewall(local, ""); code != http.StatusInternalServerError {
		t.Errorf("refused prompt: %d, want 500", code)
	}
}

func TestImport(t *testing.T) {
	room := game.NewRoom(t.TempDir())
	h := New(room, web)
	post := func(remote, origin, contentType string, body *bytes.Buffer) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/import?start=10&duration=20&via=device", body)
		req.RemoteAddr, req.Host = remote, "localhost:7777"
		req.Header.Set("Content-Type", contentType)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	upload := func(name string) (*bytes.Buffer, string) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		part, _ := w.CreateFormFile("files", name)
		part.Write([]byte("audio"))
		w.Close()
		return &buf, w.FormDataContentType()
	}

	buf, ct := upload("Artist - Song.mp3")
	rec := post(local, "http://localhost:7777", ct, buf)
	var res struct {
		Pack   string
		Tracks int
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK || res.Tracks != 1 {
		t.Fatalf("import = %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(room.PacksDir(), res.Pack, "manifest.json")); err != nil {
		t.Fatalf("imported pack not written: %v", err)
	}

	buf, ct = upload("song.mp3")
	if rec := post(phone, "", ct, buf); rec.Code != http.StatusForbidden {
		t.Errorf("from a phone: %d, want 403", rec.Code)
	}
	buf, ct = upload("song.mp3")
	if rec := post(local, "http://evil.example", ct, buf); rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin: %d, want 403", rec.Code)
	}
	if rec := post(local, "", "text/plain", bytes.NewBufferString("x")); rec.Code != http.StatusBadRequest {
		t.Errorf("not multipart: %d, want 400", rec.Code)
	}
	buf, ct = upload("notes.txt")
	if rec := post(local, "", ct, buf); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no audio file") {
		t.Errorf("no audio: %d %s, want 400", rec.Code, rec.Body)
	}
}
