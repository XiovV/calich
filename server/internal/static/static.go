// Package static embeds the built frontend (see /Dockerfile, which copies
// the frontend build into dist/ before compiling the Go binary).
package static

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// Dist returns the embedded frontend build, rooted at dist/'s contents.
func Dist() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}
