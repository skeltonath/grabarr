package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"grabarr/internal/models"

	"github.com/gorilla/mux"
)

type CreateSyncRequest struct {
	RemotePath string `json:"remote_path"`
}

func (h *Handlers) CreateSync(w http.ResponseWriter, r *http.Request) {
	var req CreateSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload", err)
		return
	}

	// Validate required fields
	if req.RemotePath == "" {
		h.writeError(w, http.StatusBadRequest, "remote_path is required", nil)
		return
	}

	// Start the sync
	syncJob, err := h.syncService.StartSync(r.Context(), req.RemotePath)
	if err != nil {
		if err.Error() == "maximum concurrent syncs (1) reached, please wait for existing sync to complete" {
			h.writeError(w, http.StatusConflict, err.Error(), nil)
		} else {
			h.writeError(w, http.StatusInternalServerError, "Failed to start sync", err)
		}
		return
	}

	h.writeSuccess(w, http.StatusCreated, syncJob, "Sync started successfully")
}

func (h *Handlers) GetSyncs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	filter := models.SyncFilter{}

	// Parse status filter
	if statusStr := query.Get("status"); statusStr != "" {
		filter.Status = []models.SyncStatus{models.SyncStatus(statusStr)}
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

	syncs, err := h.syncService.GetSyncJobs(filter)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get syncs", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, syncs, "")
}

func (h *Handlers) GetSync(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid sync ID", err)
		return
	}

	syncJob, err := h.syncService.GetSyncJob(id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "Sync not found", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, syncJob, "")
}

func (h *Handlers) CancelSync(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid sync ID", err)
		return
	}

	if err := h.syncService.CancelSync(r.Context(), id); err != nil {
		if err.Error() == "sync job is not active" {
			h.writeError(w, http.StatusBadRequest, err.Error(), nil)
		} else if err.Error() == "sync job not found" {
			h.writeError(w, http.StatusNotFound, err.Error(), nil)
		} else {
			h.writeError(w, http.StatusInternalServerError, "Failed to cancel sync", err)
		}
		return
	}

	h.writeSuccess(w, http.StatusOK, nil, "Sync cancelled successfully")
}

func (h *Handlers) GetSyncSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.syncService.GetSyncSummary()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to get sync summary", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, summary, "")
}
