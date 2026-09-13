package server

import (
	"net/http"
	"time"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/ws"
)

func New(room *game.Room) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /ws", ws.Handler(room))
	mux.HandleFunc("GET /media/{name...}", func(w http.ResponseWriter, r *http.Request) {
		// Media stay on the host machine: a phone could otherwise fetch the clip and identify it.
		if !ws.IsLocal(r) {
			http.Error(w, "media are only served to the host machine", http.StatusForbidden)
			return
		}
		name := r.PathValue("name")
		f, err := room.Media(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, name, time.Time{}, f)
	})
	return mux
}
