package server

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
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

// maxFrontReports per minute, for all pages together: on a public server anyone can post reports,
// and a real crash sends a handful at most. Past it, reports are dropped until the next minute.
const maxFrontReports = 30

// frontErrors journals the JavaScript errors reported by the pages, so phone and host crashes show
// up in linos.log.
func frontErrors() http.HandlerFunc {
	var mu sync.Mutex
	var window time.Time
	count := 0
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if now := time.Now(); now.Sub(window) >= time.Minute {
			window, count = now, 0
		}
		count++
		over := count > maxFrontReports
		mu.Unlock()
		if over {
			http.Error(w, "too many reports", http.StatusTooManyRequests)
			return
		}
		var report struct {
			Page    string `json:"page"`
			Message string `json:"message"`
			Stack   string `json:"stack"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFrontReport)).Decode(&report); err != nil {
			http.Error(w, "invalid report", http.StatusBadRequest)
			return
		}
		slog.Warn("front error", "page", report.Page, "message", report.Message, "stack", report.Stack,
			"remote", r.RemoteAddr, "userAgent", r.UserAgent())
		w.WriteHeader(http.StatusNoContent)
	}
}

// LogFile is the journal file, kept under a size limit whatever is logged: past it, the file
// becomes <path>.1, replacing the previous one, and a new file starts. At most twice the limit
// stays on disk, even on a public server flooded with requests.
type LogFile struct {
	mu    sync.Mutex
	path  string
	limit int64
	f     *os.File
	size  int64
}

// OpenLog opens the journal at path for appending.
func OpenLog(path string, limit int64) (*LogFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	info, _ := f.Stat() // just opened
	return &LogFile{path: path, limit: limit, f: f, size: info.Size()}, nil
}

func (l *LogFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > l.limit {
		l.f.Close()
		os.Rename(l.path, l.path+".1") // on failure the file is truncated below: the limit wins over old lines
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return 0, err
		}
		l.f, l.size = f, 0
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *LogFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}
