package server

import (
	"net/http"

	"github.com/gwenaf/linos/internal/ws"
)

func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /ws", ws.Handler)
	return mux
}
