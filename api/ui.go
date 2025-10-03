package api

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
)

//go:embed ui/dist
var uiFS embed.FS

var uiContent fs.FS

func init() {
	var err error
	uiContent, err = fs.Sub(uiFS, "ui/dist")
	if err != nil {
		panic(fmt.Errorf("prepare ui filesystem: %w", err))
	}
}

func (s *Server) staticHandler() http.Handler {
	return http.FileServer(http.FS(uiContent))
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, http.MethodGet)
		return
	}

	data, err := fs.ReadFile(uiContent, "index.html")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("load ui index: %w", err))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(data); err != nil {
		s.logger.Printf("write ui index: %v", err)
	}
}
