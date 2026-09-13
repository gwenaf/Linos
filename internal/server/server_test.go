package server

import (
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Body.String() != "ok" {
		t.Fatalf("health = %q, want ok", rec.Body.String())
	}
}
