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
	s.mux.Handle("GET /ui/", http.StripPrefix("/ui/", fileServer))
	s.mux.HandleFunc("GET /ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})
}
