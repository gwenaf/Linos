package server

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gwenaf/linos/internal/ws"
)

// statusRecorder captures the response status for the request log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Hijack lets the websocket upgrade go through the recorder.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(s.ResponseWriter).Hijack()
}

// logRequests logs every request: failures as warnings, other devices (phones) at info so a phone reaching
// the PC always shows up, the host machine's own requests at debug level.
func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		level := slog.LevelInfo
		switch {
		case rec.status >= 400:
			level = slog.LevelWarn
		case ws.IsLocal(r):
			level = slog.LevelDebug
		}
		slog.Log(r.Context(), level, "http request",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration", time.Since(start), "remote", r.RemoteAddr)
	})
}

// maxFrontReport bounds what a page may write to the journal.
const maxFrontReport = 8 << 10

// frontError journals a JavaScript error reported by a page, so phone and host crashes show up in linos.log.
func frontError(w http.ResponseWriter, r *http.Request) {
	var report struct {
		Page    string `json:"page"`
		Message string `json:"message"`
		Stack   string `json:"stack"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFrontReport)).Decode(&report); err != nil {
		http.Error(w, "invalid report", http.StatusBadRequest)
		return
	}
	// ponytail: no rate limit; a misbehaving page can fill the journal 8 KB at a time.
	slog.Warn("front error", "page", report.Page, "message", report.Message, "stack", report.Stack,
		"remote", r.RemoteAddr, "userAgent", r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}
