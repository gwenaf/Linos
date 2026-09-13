package web

import (
	"embed"
	"io/fs"
)

// dist holds the built front (npm run build). The committed dist/.gitkeep keeps go build working before any build.
//
//go:embed all:dist
var dist embed.FS

func Dist() fs.FS {
	sub, _ := fs.Sub(dist, "dist") // "dist" is a valid, embedded path
	return sub
}
