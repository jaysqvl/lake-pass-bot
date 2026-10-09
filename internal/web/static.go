package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// The Vite build is embedded in native binaries and container builds alike.
//
//go:embed all:dist
var frontendFiles embed.FS

func frontendHandler() http.Handler {
	files, _ := fs.Sub(frontendFiles, "dist")
	return newFrontendHandler(files)
}

func newFrontendHandler(files fs.FS) http.Handler {
	assets := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if name != "" && !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		if name != "" {
			if info, err := fs.Stat(files, name); err == nil && !info.IsDir() && !strings.HasPrefix(name, ".") {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				assets.ServeHTTP(w, r)
				return
			}
		}
		if name == "api" || strings.HasPrefix(name, "api/") || name == "assets" || strings.HasPrefix(name, "assets/") || name == "static" || strings.HasPrefix(name, "static/") || path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		index, err := fs.ReadFile(files, "index.html")
		if err != nil {
			http.Error(w, "frontend build missing; run npm ci and npm run build in web/frontend", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
