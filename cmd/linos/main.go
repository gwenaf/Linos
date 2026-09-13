package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gwenaf/linos/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	ln, err := net.Listen("tcp", ":7777")
	if err != nil {
		log.Fatal(err)
	}
	if err := run(ctx, ln); err != nil {
		log.Fatal(err)
	}
}

// run serves until ctx is cancelled, then shuts down gracefully.
func run(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: server.New()}
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
