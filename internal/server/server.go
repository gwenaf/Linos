package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/network"
	"github.com/gwenaf/linos/internal/tags"
	"github.com/gwenaf/linos/internal/ws"
)

// New serves the game: the pages from web (the built front), the websocket and the host-only helpers.
// edit enables the pack editor.
func New(room *game.Room, web fs.FS, edit bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /ws", ws.Handler(room))

	page := func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, web, "index.html") }
	mux.HandleFunc("GET /control", page)
	mux.HandleFunc("GET /host", page)
	mux.HandleFunc("GET /play", page)
	if edit {
		editor(mux, room.PacksDir(), page)
	}
	mux.Handle("GET /assets/", http.FileServerFS(web))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		target := "/play"
		if ws.IsLocal(r) {
			target = "/control"
		}
		http.Redirect(w, r, target, http.StatusFound)
	})

	// Media stay on the host machine: a phone could otherwise fetch the clip and identify it.
	mux.HandleFunc("GET /media/{name...}", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		f, err := room.Media(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, name, time.Time{}, f)
	}))

	mux.HandleFunc("GET /api/join", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		_, port, err := net.SplitHostPort(r.Host)
		if err != nil {
			port = "80"
		}
		urls := []string{}
		for _, ip := range network.LocalIPs() {
			urls = append(urls, "http://"+net.JoinHostPort(ip, port)+"/play")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{"urls": urls})
	}))

	mux.HandleFunc("GET /api/network", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"categories": network.Categories(),
			"addresses":  network.LocalIPs(),
			"hotspot":    network.HotspotPrefix,
		})
	}))

	mux.HandleFunc("POST /api/firewall", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		exe, _ := os.Executable() // cannot fail on Windows, the only platform AllowFirewall acts on
		if err := network.AllowFirewall(exe); err != nil {
			slog.Warn("firewall rule not added", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("firewall rule added", "program", exe)
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("POST /api/import", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		files, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "expected a multipart upload of audio files", http.StatusBadRequest)
			return
		}
		q := r.URL.Query()
		start, _ := strconv.ParseFloat(q.Get("start"), 64)       // missing or invalid: 0
		duration, _ := strconv.ParseFloat(q.Get("duration"), 64) // missing or invalid: pack default
		res, err := tags.Import(room.PacksDir(), files, tags.Options{Start: max(start, 0), Duration: duration, Via: q.Get("via")})
		if err != nil {
			slog.Warn("import failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Info("music imported", "pack", res.Pack, "tracks", res.Tracks, "skipped", len(res.Skipped))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	}))

	mux.HandleFunc("POST /api/log", frontError)

	mux.HandleFunc("GET /qr.png", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		png, err := qrcode.Encode(r.URL.Query().Get("data"), qrcode.Medium, 512)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	}))
	return logRequests(mux)
}

// hostOnly restricts a route to the host machine; posts from another web origin are refused too,
// so a page open in the PC's browser cannot trigger them.
func hostOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ws.IsLocal(r) {
			http.Error(w, "only available on the host machine", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}
