package server

import (
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/network"
	"github.com/gwenaf/linos/internal/ws"
)

// New serves the game: the three pages from web (the built front), the websocket and the host-only helpers.
func New(room *game.Room, web fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /ws", ws.Handler(room))

	page := func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, web, "index.html") }
	mux.HandleFunc("GET /control", page)
	mux.HandleFunc("GET /host", page)
	mux.HandleFunc("GET /play", page)
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

func hostOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ws.IsLocal(r) {
			http.Error(w, "only available on the host machine", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}
