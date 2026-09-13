package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/gwenaf/linos/internal/game"
	"github.com/gwenaf/linos/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", ":7777")
	if err != nil {
		log.Fatal(err)
	}
	// Packs live next to the executable so Linos stays portable on a USB stick.
	if err := run(ctx, ln, filepath.Join(filepath.Dir(exe), "packs")); err != nil {
		log.Fatal(err)
	}
}

// run serves until ctx is cancelled, then shuts down gracefully.
func run(ctx context.Context, ln net.Listener, packsDir string) error {
	srv := &http.Server{Handler: server.New(game.NewRoom(packsDir))}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Println("listening on", ln.Addr())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
