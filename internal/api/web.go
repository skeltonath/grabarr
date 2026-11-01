package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	webui "github.com/skeltonath/grabarr/web"
)

// registerWebRoutes serves the embedded SvelteKit app (web/build) for every
// path the API doesn't claim. Client-side routes (/jobs, /settings) get
// index.html so a deep link or a refresh lands on the right page.
func (h *Handlers) registerWebRoutes(r chi.Router) {
	ui := h.ui
	if ui == nil {
		sub, err := fs.Sub(webui.Build, "build")
		if err != nil {
			slog.Error("web UI not embedded", "error", err)
			return
		}
		ui = sub
	}
	s := &uiServer{fs: ui}
	r.NotFound(s.ServeHTTP)
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
			return
		}
		s.ServeHTTP(w, r)
	})
}

type uiServer struct {
	fs    fs.FS
	etags sync.Map // file path → ETag
}

func (s *uiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead || strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p != "" {
		if st, err := fs.Stat(s.fs, p); err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				// Icons and the manifest keep their names across deploys.
				w.Header().Set("Cache-Control", "no-cache")
				if tag := s.etag(p); tag != "" {
					w.Header().Set("ETag", tag)
				}
			}
			http.ServeFileFS(w, r, s.fs, p)
			return
		}
	}
	// A missing asset is a 404, not the app's HTML.
	if path.Ext(p) != "" {
		http.NotFound(w, r)
		return
	}
	index, err := fs.ReadFile(s.fs, "index.html")
	if err != nil {
		http.Error(w, "web UI not built (run make web)", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}

func (s *uiServer) etag(p string) string {
	if v, ok := s.etags.Load(p); ok {
		return v.(string)
	}
	b, err := fs.ReadFile(s.fs, p)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	s.etags.Store(p, tag)
	return tag
}
