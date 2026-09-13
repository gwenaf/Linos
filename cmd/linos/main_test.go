package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestRunShutsDownOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	}()

	res, err := http.Get("http://" + ln.Addr().String() + "/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run = %v, want clean shutdown", err)
	}
}

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "debug").Debug("seen", "k", 1)
	newLogger(&buf, "").Debug("hidden")
	newLogger(&buf, "nonsense").Info("info by default")
	out := buf.String()
	if !strings.Contains(out, `"msg":"seen","k":1`) || strings.Contains(out, "hidden") || !strings.Contains(out, "info by default") {
		t.Fatalf("log output = %s", out)
	}
}

func TestRunReturnsServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if err := run(context.Background(), ln, http.NotFoundHandler()); err == nil {
		t.Fatal("run on a closed listener must fail")
	}
}
