package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
)

// registerWebRoutes sets up static file serving for the web UI
func (h *Handlers) registerWebRoutes(r *mux.Router) {
	// Determine web directory path
	webDir := "web/static"
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		// Try relative to binary location
		if execPath, err := os.Executable(); err == nil {
			webDir = filepath.Join(filepath.Dir(execPath), "web", "static")
		}
	}

	// Serve static assets (images, etc.)
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(webDir)))
	r.PathPrefix("/static/").Handler(staticHandler)

	// Serve dashboard
	r.HandleFunc("/", h.serveDashboard).Methods("GET")
}

// serveDashboard serves the main dashboard HTML page
func (h *Handlers) serveDashboard(w http.ResponseWriter, r *http.Request) {
	webDir := "web/static"
	possiblePaths := []string{
		webDir,
		filepath.Join(".", webDir),
		filepath.Join("/app", webDir),
	}
	for _, dir := range possiblePaths {
		p := filepath.Join(dir, "v2.html")
		if _, err := os.Stat(p); err == nil {
			http.ServeFile(w, r, p)
			return
		}
	}
	http.NotFound(w, r)
}
