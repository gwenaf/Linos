package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/network"
	"github.com/gwenaf/linos/internal/ws"
)

// allowFirewall opens the firewall to Linos. Tests replace it: the real one asks Windows for elevation.
var allowFirewall = network.AllowFirewall

// Options configure what the server exposes.
type Options struct {
	// Edit enables the pack editor.
	Edit bool
	// AdminToken, when set, gives a browser that signs in at /login the host machine's rights:
	// the way to run a game on a self-hosted server.
	AdminToken string
	// PublicURL, when set, is the address the phones join (QR code): Linos cannot see it from a
	// container or behind a reverse proxy.
	PublicURL string
}

// New serves the game: the pages from web (the built front), the websocket and the host-only helpers.
func New(room *game.Room, web fs.FS, opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	isHost := hostRights(opts.AdminToken)
	hostOnly := restrict(isHost)
	mux.Handle("GET /ws", ws.Handler(room, isHost))
	if opts.AdminToken != "" {
		adminLogin(mux, opts.AdminToken)
	}

	page := func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, web, "index.html") }
	mux.HandleFunc("GET /control", page)
	mux.HandleFunc("GET /host", page)
	mux.HandleFunc("GET /play", page)
	if opts.Edit {
		editor(mux, room.PacksDir(), page, hostOnly)
	}
	mux.Handle("GET /assets/", http.FileServerFS(web))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		target := "/play"
		if isHost(r) {
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
		inert(w)
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
		if opts.PublicURL != "" {
			urls = []string{strings.TrimSuffix(opts.PublicURL, "/") + "/play"}
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
			"firewall":   network.CanAllowFirewall(),
		})
	}))

	mux.HandleFunc("POST /api/firewall", hostOnly(func(w http.ResponseWriter, r *http.Request) {
		exe, _ := os.Executable() // cannot fail on Windows, the only platform AllowFirewall acts on
		if err := allowFirewall(exe); err != nil {
			slog.Warn("firewall rule not added", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("firewall rule added", "program", exe)
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("POST /api/log", frontErrors())

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

// inert keeps a pack file from ever running as a page, whatever its content: no type sniffing, and
// sandboxed if a browser opens it directly.
func inert(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
}

// restrict returns hostOnly, which limits a route to requests with the host machine's rights; posts
// from another web origin are refused too, so a page open in the same browser cannot trigger them.
func restrict(isHost func(*http.Request) bool) func(http.HandlerFunc) http.HandlerFunc {
	return func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !isHost(r) {
				http.Error(w, "only available on the host machine", http.StatusForbidden)
				return
			}
			// Scheme left out: behind an HTTPS reverse proxy the server itself is reached in HTTP.
			if origin := r.Header.Get("Origin"); origin != "" {
				if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request refused", http.StatusForbidden)
					return
				}
			}
			h(w, r)
		}
	}
}
