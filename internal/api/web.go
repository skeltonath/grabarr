package api

import (
	"fmt"
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

	// Serve static files (CSS, JS, images)
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(webDir)))
	r.PathPrefix("/static/").Handler(staticHandler)

	// Serve main dashboard at /dashboard
	r.HandleFunc("/dashboard", h.serveDashboard).Methods("GET")
	r.HandleFunc("/ui", h.serveDashboard).Methods("GET")
}

// serveDashboard serves the main dashboard HTML page
func (h *Handlers) serveDashboard(w http.ResponseWriter, r *http.Request) {
	webDir := "web/static"

	// Try different possible locations
	possiblePaths := []string{
		webDir,
		filepath.Join(".", webDir),
		filepath.Join("/app", webDir),
	}

	var indexPath string
	for _, dir := range possiblePaths {
		testPath := filepath.Join(dir, "index.html")
		if _, err := os.Stat(testPath); err == nil {
			indexPath = testPath
			break
		}
	}

	if indexPath == "" {
		// If index.html doesn't exist, show a simple message with navigation
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Grabarr Web UI</title>
    <style>
        body { font-family: sans-serif; max-width: 800px; margin: 50px auto; padding: 20px; }
        .links { margin-top: 30px; }
        .links a { display: inline-block; margin-right: 20px; padding: 10px 15px; background: #007bff; color: white; text-decoration: none; border-radius: 5px; }
    </style>
</head>
<body>
    <h1>Grabarr Web UI</h1>
    <p>Web UI files not found, but the API is working!</p>
    <div class="links">
        <a href="/api/v1/health">Health Check</a>
        <a href="/api/v1/status">System Status</a>
        <a href="/api/v1/jobs">View Jobs (JSON)</a>
        <a href="/api/v1/jobs/summary">Job Summary</a>
    </div>
    <p><strong>Debug info:</strong> Checked paths: ` + fmt.Sprintf("%v", possiblePaths) + `</p>
</body>
</html>
		`))
		return
	}

	http.ServeFile(w, r, indexPath)
}
