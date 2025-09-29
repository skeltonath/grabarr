package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"grabarr/internal/config"
	"grabarr/internal/queue"

	"github.com/gorilla/mux"
)

type Handlers struct {
	queue       queue.JobQueue
	monitor     ResourceMonitor
	config      *config.Config
	syncService SyncService
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


func NewHandlers(jobQueue queue.JobQueue, monitor ResourceMonitor, cfg *config.Config, syncService SyncService) *Handlers {
	return &Handlers{
		queue:       jobQueue,
		monitor:     monitor,
		config:      cfg,
		syncService: syncService,
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

	// Sync endpoints
	api.HandleFunc("/sync", h.CreateSync).Methods("POST")
	api.HandleFunc("/sync", h.GetSyncs).Methods("GET")
	api.HandleFunc("/sync/{id:[0-9]+}", h.GetSync).Methods("GET")
	api.HandleFunc("/sync/{id:[0-9]+}/cancel", h.CancelSync).Methods("POST")
	api.HandleFunc("/sync/summary", h.GetSyncSummary).Methods("GET")

	// System endpoints
	api.HandleFunc("/health", h.HealthCheck).Methods("GET")
	api.HandleFunc("/metrics", h.GetMetrics).Methods("GET")
	api.HandleFunc("/status", h.GetStatus).Methods("GET")

	// Add CORS middleware
	api.Use(corsMiddleware)
	api.Use(loggingMiddleware)
	api.Use(jsonContentTypeMiddleware)
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

