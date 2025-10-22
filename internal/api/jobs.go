package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"grabarr/internal/models"

	"github.com/gorilla/mux"
)

type CreateJobRequest struct {
	Name           string                 `json:"name"`
	RemotePath     string                 `json:"remote_path"`
	Priority       int                    `json:"priority,omitempty"`
	MaxRetries     int                    `json:"max_retries,omitempty"`
	EstimatedSize  int64                  `json:"estimated_size,omitempty"`
	FileSize       int64                  `json:"file_size,omitempty"`
	Metadata       models.JobMetadata     `json:"metadata,omitempty"`
	DownloadConfig *models.DownloadConfig `json:"download_config,omitempty"`
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

	// Check category filtering
	downloadsConfig := h.config.GetDownloads()
	if len(downloadsConfig.AllowedCategories) > 0 {
		category := req.Metadata.Category
		if category == "" || !contains(downloadsConfig.AllowedCategories, category) {
			h.writeError(w, http.StatusBadRequest,
				fmt.Sprintf("category '%s' not allowed. Allowed categories: %v",
					category, downloadsConfig.AllowedCategories), nil)
			return
		}
	}

	// Create job model
	job := &models.Job{
		Name:           req.Name,
		RemotePath:     req.RemotePath,
		LocalPath:      downloadsConfig.LocalPath,
		Priority:       req.Priority,
		MaxRetries:     req.MaxRetries,
		EstimatedSize:  req.EstimatedSize,
		Metadata:       req.Metadata,
		DownloadConfig: req.DownloadConfig,
		Status:         models.JobStatusQueued,
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

	if err := h.queue.DeleteJob(id); err != nil {
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

func (h *Handlers) RetryJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid job ID", err)
		return
	}

	if err := h.queue.RetryJob(id); err != nil {
		h.writeError(w, http.StatusBadRequest, "Failed to retry job", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, nil, "Job retried successfully")
}

func (h *Handlers) GetJobSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.queue.GetSummary()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get job summary", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, summary, "")
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
