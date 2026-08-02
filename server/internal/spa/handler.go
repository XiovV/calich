// Package spa serves an embedded single-page-app build: existing files are
// served directly, and any other path falls back to index.html so
// client-side routing survives a refresh or deep link.
package spa

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

func New(distFS fs.FS) (http.Handler, error) {
	index, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}

	fileServer := http.FileServerFS(distFS)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if f, err := distFS.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	}), nil
}
