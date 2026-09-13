package web

import (
	"io/fs"
	"testing"
)

func TestDist(t *testing.T) {
	if _, err := fs.Stat(Dist(), ".gitkeep"); err != nil {
		t.Fatalf("dist is not embedded: %v", err)
	}
}
