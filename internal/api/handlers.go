package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/skeltonath/grabarr/internal/config"
	"github.com/skeltonath/grabarr/internal/interfaces"
	"github.com/skeltonath/grabarr/internal/sync"

	"github.com/go-chi/chi/v5"
)

type Handlers struct {
	queue          interfaces.JobQueue
	gatekeeper     interfaces.Gatekeeper
	config         *config.Config
	remoteFileRepo RemoteFileRepo
	scanner        *sync.Scanner
	ui             fs.FS // the web UI's files; nil means the embedded build
}

type APIResponse struct {
	Success    bool            `json:"success"`
	Data       interface{}     `json:"data,omitempty"`
	Error      string          `json:"error,omitempty"`
	Message    string          `json:"message,omitempty"`
	Pagination *PaginationMeta `json:"pagination,omitempty"`
}

type PaginationMeta struct {
	Total      int `json:"total"`
	Limit      int `json:"limit"`
	Offset     int `json:"offset"`
	TotalPages int `json:"total_pages"`
	Page       int `json:"page"`
}

func NewHandlers(jobQueue interfaces.JobQueue, gatekeeper interfaces.Gatekeeper, cfg *config.Config, remoteFileRepo RemoteFileRepo, scanner *sync.Scanner) *Handlers {
	return &Handlers{
		queue:          jobQueue,
		gatekeeper:     gatekeeper,
		config:         cfg,
		remoteFileRepo: remoteFileRepo,
		scanner:        scanner,
	}
}

func (h *Handlers) RegisterRoutes(r chi.Router) {
	h.registerWebRoutes(r)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(corsMiddleware)
		api.Use(loggingMiddleware)
		api.Use(jsonContentTypeMiddleware)

		// Job management endpoints
		api.Post("/jobs", h.CreateJob)
		api.Get("/jobs", h.GetJobs)
		api.Get("/jobs/summary", h.GetJobSummary)
		api.Get("/jobs/{id:[0-9]+}", h.GetJob)
		api.Delete("/jobs/{id:[0-9]+}", h.DeleteJob)
		api.Post("/jobs/{id:[0-9]+}/cancel", h.CancelJob)
		api.Post("/jobs/{id:[0-9]+}/retry", h.RetryJob)
		api.Post("/jobs/{id:[0-9]+}/start-now", h.StartJobNow)

		// Remote files (seedbox scanner) endpoints
		api.Get("/remote-files", h.ListRemoteFiles)
		api.Get("/remote-files/tree", h.GetRemoteFileTree)
		api.Post("/remote-files/queue-folder", h.QueueFolder)
		api.Post("/remote-files/{id:[0-9]+}/queue", h.QueueRemoteFile)
		api.Post("/remote-files/{id:[0-9]+}/ignore", h.IgnoreRemoteFile)
		api.Post("/remote-files/{id:[0-9]+}/restore", h.RestoreRemoteFile)
		api.Post("/sync/scan", h.TriggerScan)
		api.Get("/sync/status", h.GetSyncStatus)

		// Runtime settings endpoints
		api.Get("/settings", h.GetSettings)
		api.Patch("/settings", h.UpdateSettings)

		// System endpoints
		api.Get("/health", h.HealthCheck)
		api.Get("/metrics", h.GetMetrics)
		api.Get("/status", h.GetStatus)
	})
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

func (h *Handlers) writeSuccessWithPagination(w http.ResponseWriter, statusCode int, data interface{}, pagination *PaginationMeta, message string) {
	w.WriteHeader(statusCode)
	response := APIResponse{
		Success:    true,
		Data:       data,
		Message:    message,
		Pagination: pagination,
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
