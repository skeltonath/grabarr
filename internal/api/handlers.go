package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"grabarr/internal/models"
	"grabarr/internal/queue"

	"github.com/gorilla/mux"
)

type Handlers struct {
	queue    queue.JobQueue
	monitor  ResourceMonitor
}

type ResourceMonitor interface {
	GetResourceStatus() queue.ResourceStatus
	GetMetrics() map[string]interface{}
}

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

type CreateJobRequest struct {
	Name          string            `json:"name"`
	RemotePath    string            `json:"remote_path"`
	LocalPath     string            `json:"local_path,omitempty"`
	Priority      int               `json:"priority,omitempty"`
	MaxRetries    int               `json:"max_retries,omitempty"`
	EstimatedSize int64             `json:"estimated_size,omitempty"`
	Metadata      models.JobMetadata `json:"metadata,omitempty"`
}

func NewHandlers(jobQueue queue.JobQueue, monitor ResourceMonitor) *Handlers {
	return &Handlers{
		queue:   jobQueue,
		monitor: monitor,
	}
}

func (h *Handlers) RegisterRoutes(r *mux.Router) {
	// Web UI routes (serve before API to avoid conflicts)
	h.registerWebRoutes(r)

	api := r.PathPrefix("/api/v1").Subrouter()

	// Job management endpoints
	api.HandleFunc("/jobs", h.CreateJob).Methods("POST")
	api.HandleFunc("/jobs", h.GetJobs).Methods("GET")
	api.HandleFunc("/jobs/{id:[0-9]+}", h.GetJob).Methods("GET")
	api.HandleFunc("/jobs/{id:[0-9]+}", h.DeleteJob).Methods("DELETE")
	api.HandleFunc("/jobs/{id:[0-9]+}/cancel", h.CancelJob).Methods("POST")
	api.HandleFunc("/jobs/summary", h.GetJobSummary).Methods("GET")

	// System endpoints
	api.HandleFunc("/health", h.HealthCheck).Methods("GET")
	api.HandleFunc("/metrics", h.GetMetrics).Methods("GET")
	api.HandleFunc("/status", h.GetStatus).Methods("GET")

	// Add CORS middleware
	api.Use(corsMiddleware)
	api.Use(loggingMiddleware)
	api.Use(jsonContentTypeMiddleware)
}

func (h *Handlers) CreateJob(w http.ResponseWriter, r *http.Request) {
	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload", err)
		return
	}

	// Validate required fields
	if req.Name == "" {
		h.writeError(w, http.StatusBadRequest, "job name is required", nil)
		return
	}
	if req.RemotePath == "" {
		h.writeError(w, http.StatusBadRequest, "remote_path is required", nil)
		return
	}

	// Create job model
	job := &models.Job{
		Name:          req.Name,
		RemotePath:    req.RemotePath,
		LocalPath:     req.LocalPath,
		Priority:      req.Priority,
		MaxRetries:    req.MaxRetries,
		EstimatedSize: req.EstimatedSize,
		Metadata:      req.Metadata,
		Status:        models.JobStatusQueued,
		Progress: models.JobProgress{
			LastUpdateTime: time.Now(),
		},
	}

	// Enqueue the job
	if err := h.queue.Enqueue(job); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to enqueue job", err)
		return
	}

	h.writeSuccess(w, http.StatusCreated, job, "Job created successfully")
}

func (h *Handlers) GetJobs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	filter := models.JobFilter{}

	// Parse status filter
	if statusStr := query.Get("status"); statusStr != "" {
		filter.Status = []models.JobStatus{models.JobStatus(statusStr)}
	}

	// Parse category filter
	if category := query.Get("category"); category != "" {
		filter.Category = category
	}

	// Parse priority filters
	if minPriorityStr := query.Get("min_priority"); minPriorityStr != "" {
		if minPriority, err := strconv.Atoi(minPriorityStr); err == nil {
			filter.MinPriority = &minPriority
		}
	}
	if maxPriorityStr := query.Get("max_priority"); maxPriorityStr != "" {
		if maxPriority, err := strconv.Atoi(maxPriorityStr); err == nil {
			filter.MaxPriority = &maxPriority
		}
	}

	// Parse pagination
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 && limit <= 1000 {
			filter.Limit = limit
		} else {
			filter.Limit = 50 // Default limit
		}
	} else {
		filter.Limit = 50
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			filter.Offset = offset
		}
	}

	// Parse sorting
	if sortBy := query.Get("sort_by"); sortBy != "" {
		filter.SortBy = sortBy
	}
	if sortOrder := query.Get("sort_order"); sortOrder != "" {
		filter.SortOrder = sortOrder
	}

	jobs, err := h.queue.GetJobs(filter)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get jobs", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, jobs, "")
}

func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid job ID", err)
		return
	}

	job, err := h.queue.GetJob(id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "Job not found", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, job, "")
}

func (h *Handlers) DeleteJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid job ID", err)
		return
	}

	if err := h.queue.CancelJob(id); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to delete job", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, nil, "Job deleted successfully")
}

func (h *Handlers) CancelJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid job ID", err)
		return
	}

	if err := h.queue.CancelJob(id); err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to cancel job", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, nil, "Job cancelled successfully")
}

func (h *Handlers) GetJobSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.queue.GetSummary()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get job summary", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, summary, "")
}

func (h *Handlers) HealthCheck(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(startTime).String(),
		"version":   "1.0.0", // TODO: Get from build info
	}

	// Check resource status
	if h.monitor != nil {
		resourceStatus := h.monitor.GetResourceStatus()
		health["resources"] = resourceStatus
	}

	h.writeSuccess(w, http.StatusOK, health, "Service is healthy")
}

func (h *Handlers) GetMetrics(w http.ResponseWriter, r *http.Request) {
	if h.monitor == nil {
		h.writeError(w, http.StatusServiceUnavailable, "Monitoring not available", nil)
		return
	}

	metrics := h.monitor.GetMetrics()

	// Add job queue metrics
	summary, err := h.queue.GetSummary()
	if err == nil {
		metrics["jobs"] = summary
	}

	h.writeSuccess(w, http.StatusOK, metrics, "")
}

func (h *Handlers) GetStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"service":   "grabarr",
		"version":   "1.0.0",
		"timestamp": time.Now().UTC(),
		"uptime":    time.Since(startTime).String(),
	}

	// Get job summary
	if summary, err := h.queue.GetSummary(); err == nil {
		status["jobs"] = summary
	}

	// Get resource status
	if h.monitor != nil {
		status["resources"] = h.monitor.GetResourceStatus()
	}

	h.writeSuccess(w, http.StatusOK, status, "")
}

func (h *Handlers) writeSuccess(w http.ResponseWriter, statusCode int, data interface{}, message string) {
	w.WriteHeader(statusCode)
	response := APIResponse{
		Success: true,
		Data:    data,
		Message: message,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func (h *Handlers) writeError(w http.ResponseWriter, statusCode int, message string, err error) {
	w.WriteHeader(statusCode)
	response := APIResponse{
		Success: false,
		Error:   message,
	}

	if err != nil {
		slog.Error("API error", "message", message, "error", err)
	} else {
		slog.Warn("API error", "message", message)
	}

	if jsonErr := json.NewEncoder(w).Encode(response); jsonErr != nil {
		slog.Error("failed to encode error response", "error", jsonErr)
	}
}

var startTime = time.Now()

// Middleware functions
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a custom ResponseWriter to capture the status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		slog.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", duration.String(),
			"user_agent", r.UserAgent(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

func jsonContentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

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

	// Serve main dashboard at root
	r.HandleFunc("/", h.serveDashboard).Methods("GET")
}

// serveDashboard serves the main dashboard HTML page
func (h *Handlers) serveDashboard(w http.ResponseWriter, r *http.Request) {
	webDir := "web/static"
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		// Try relative to binary location
		if execPath, err := os.Executable(); err == nil {
			webDir = filepath.Join(filepath.Dir(execPath), "web", "static")
		}
	}

	indexPath := filepath.Join(webDir, "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		// If index.html doesn't exist, show a simple message
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Grabarr</title>
</head>
<body>
    <h1>Grabarr Web UI</h1>
    <p>Web interface is being developed. Use the API at <a href="/api/v1/health">/api/v1/</a> for now.</p>
</body>
</html>
		`))
		return
	}

	http.ServeFile(w, r, indexPath)
}