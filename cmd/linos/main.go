package main

import (
	"context"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/network"
	"github.com/gwenaf/linos/internal/server"
	"github.com/gwenaf/linos/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// LINOS_HOME holds packs/ and linos.log; by default next to the executable so Linos stays portable.
	// With go run the executable lives in a temp folder: set LINOS_HOME=bin.
	home := os.Getenv("LINOS_HOME")
	if home == "" {
		exe, err := os.Executable()
		if err != nil {
			log.Fatal(err)
		}
		home = filepath.Dir(exe)
	}
	os.MkdirAll(home, 0o755) // a package manager may point it at a folder to create; OpenFile reports a failure
	logFile, err := server.OpenLog(filepath.Join(home, "linos.log"), 10<<20)
	if err != nil {
		log.Fatal(err)
	}
	defer logFile.Close()
	slog.SetDefault(newLogger(io.MultiWriter(os.Stderr, logFile), os.Getenv("LINOS_LOG_LEVEL")))

	addr := ":7777"
	if a := os.Getenv("LINOS_ADDR"); a != "" {
		addr = a
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("cannot listen: is Linos already running?", "addr", addr, "error", err)
		os.Exit(1)
	}
	// Front development: LINOS_WEB_DIR=web/dist serves the front from disk, rebuilt by npm run dev.
	var front fs.FS = web.Dist()
	if dir := os.Getenv("LINOS_WEB_DIR"); dir != "" {
		front = os.DirFS(dir)
	}
	packs := filepath.Join(home, "packs")
	slog.Info("starting", "home", home, "packs", packs, "webDir", os.Getenv("LINOS_WEB_DIR"), "joinAddresses", network.LocalIPs())
	// LINOS_ADMIN_TOKEN lets a browser sign in at /login to run a self-hosted server. It is the only
	// barrier to the host's rights there, hence a length that rules out guessing.
	token := os.Getenv("LINOS_ADMIN_TOKEN")
	if token != "" && len(token) < 32 {
		slog.Error("LINOS_ADMIN_TOKEN must be at least 32 characters, e.g. openssl rand -base64 32")
		os.Exit(1)
	}
	// LINOS_EDIT=off removes the pack editor.
	opts := server.Options{Edit: os.Getenv("LINOS_EDIT") != "off", AdminToken: token}
	// A double-click on the executable lands on the control page. Not on a server, nor with
	// LINOS_BROWSER=off (tests, development); a machine without a browser just logs it.
	if token == "" && os.Getenv("LINOS_BROWSER") != "off" {
		if err := server.OpenBrowser("http://localhost:" + portOf(ln) + "/control"); err != nil {
			slog.Info("browser not opened", "error", err)
		}
	}
	if err := run(ctx, ln, server.New(game.NewRoom(packs), front, opts)); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes JSON events; level is "debug", "warn" or "error", info otherwise.
func newLogger(w io.Writer, level string) *slog.Logger {
	var lvl slog.Level
	lvl.UnmarshalText([]byte(strings.ToUpper(level))) // unknown or empty level: info
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl}))
}

// run serves until ctx is cancelled, then shuts down gracefully.
func run(ctx context.Context, ln net.Listener, h http.Handler) error {
	// ReadHeaderTimeout closes connections that never finish their headers (slowloris). No overall
	// timeout: media streams and websockets stay open for the whole game.
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	slog.Info("listening", "control", "http://localhost:"+portOf(ln)+"/control")

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func portOf(ln net.Listener) string {
	_, port, _ := net.SplitHostPort(ln.Addr().String()) // a TCP listener address always has a port
	return port
}
