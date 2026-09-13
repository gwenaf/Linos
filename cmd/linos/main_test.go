package main

import (
	"context"
	"net"
	"net/http"
	"testing"
)

func TestRunShutsDownOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, ln) }()

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

func TestRunReturnsServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if err := run(context.Background(), ln); err == nil {
		t.Fatal("run on a closed listener must fail")
	}
}
