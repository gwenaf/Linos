package server

import (
	"net/http"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/ws"
)

func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("GET /ws", ws.Handler(game.NewRoom()))
	return mux
}
