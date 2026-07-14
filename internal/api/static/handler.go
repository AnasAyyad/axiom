package static

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Handler returns a handler for the frontend assets embedded at build time.
func Handler() http.Handler {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	return New(assets)
}

// New constructs the SPA handler over a supplied filesystem for focused tests.
func New(assets fs.FS) http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(writer, "method_not_allowed", http.StatusMethodNotAllowed)
			return
		}
		requested := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if requested != "." {
			if info, err := fs.Stat(assets, requested); err == nil && !info.IsDir() {
				files.ServeHTTP(writer, request)
				return
			}
		}
		if _, err := fs.Stat(assets, "index.html"); err != nil {
			http.NotFound(writer, request)
			return
		}
		request.URL.Path = "/"
		files.ServeHTTP(writer, request)
	})
}
