package apihost

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:web
var webFS embed.FS

// registerUI adds the /ui routes serving the embedded web frontend.
func (s *Server) registerUI() {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		// Should never happen — the embed path is compiled in.
		panic("apihost: failed to sub web FS: " + err.Error())
	}

	fileServer := http.FileServerFS(sub)
	// Disable caching so rebuilds (esp. app.js progress/countdown fixes)
	// show up without a hard refresh — browsers otherwise keep stale UI.
	noCache := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
	s.mux.Handle("GET /ui/", http.StripPrefix("/ui/", noCache))
	s.mux.HandleFunc("GET /ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})
}
