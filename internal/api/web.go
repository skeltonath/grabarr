package api

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
)

// spaRoutes are the client-side routes the web UI pushes onto the history
// stack. Each one serves the same shell so a deep link or a refresh lands on
// the right page instead of a 404.
var spaRoutes = []string{"/", "/jobs", "/settings"}

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
	for _, route := range spaRoutes {
		r.HandleFunc(route, h.serveDashboard).Methods("GET")
		if route != "/" {
			// "/jobs/" is the same page as "/jobs". Redirecting rather than
			// 404ing keeps a hand-typed or shared URL working, and keeps one
			// canonical address for the route.
			r.HandleFunc(route+"/", redirectTo(route)).Methods("GET")
		}
	}
}

func redirectTo(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, path, http.StatusMovedPermanently)
	}
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
