package api

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"grabarr/internal/config"
	"grabarr/internal/models"

	"github.com/gorilla/mux"
)

// RemoteFileRepo is the repository interface for remote files.
type RemoteFileRepo interface {
	GetRemoteFiles(filter models.RemoteFileFilter) ([]*models.RemoteFile, error)
	CountRemoteFiles(filter models.RemoteFileFilter) (int, error)
	GetRemoteFile(id int64) (*models.RemoteFile, error)
	UpdateRemoteFileStatus(id int64, status models.FileStatus) error
	LinkRemoteFileToJob(remoteFileID, jobID int64, status models.FileStatus) error
}

// ListRemoteFiles returns all remote files with optional status/extension filters.
func (h *Handlers) ListRemoteFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := models.RemoteFileFilter{}
	if s := q.Get("status"); s != "" {
		filter.Status = models.FileStatus(s)
	}
	if wp := q.Get("watched_path"); wp != "" {
		filter.WatchedPath = wp
	}
	if ext := q.Get("extension"); ext != "" {
		filter.Extension = ext
	}
	if l := q.Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if o := q.Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 100
	}

	files, err := h.remoteFileRepo.GetRemoteFiles(filter)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list remote files", err)
		return
	}

	total, err := h.remoteFileRepo.CountRemoteFiles(filter)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to count remote files", err)
		return
	}

	totalPages := (total + filter.Limit - 1) / filter.Limit
	if totalPages == 0 {
		totalPages = 1
	}
	page := filter.Offset/filter.Limit + 1

	pagination := &PaginationMeta{
		Total:      total,
		Limit:      filter.Limit,
		Offset:     filter.Offset,
		TotalPages: totalPages,
		Page:       page,
	}

	h.writeSuccessWithPagination(w, http.StatusOK, files, pagination, "")
}

// QueueRemoteFile creates a download job for the given remote file.
func (h *Handlers) QueueRemoteFile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(mux.Vars(r)["id"])
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid file ID", err)
		return
	}

	rf, err := h.remoteFileRepo.GetRemoteFile(id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "remote file not found", err)
		return
	}

	if rf.Status == models.FileStatusQueued || rf.Status == models.FileStatusDownloading {
		h.writeError(w, http.StatusConflict, "file is already queued or downloading", nil)
		return
	}

	// Find the WatchedPath config for this file.
	wp := h.findWatchedPath(rf.WatchedPath)

	baseLocalPath := h.config.GetDownloads().LocalPath
	if wp != nil && wp.LocalPath != "" {
		baseLocalPath = wp.LocalPath
	}
	localPath := localPathForRemoteFile(rf.RemotePath, rf.WatchedPath, baseLocalPath)

	job := &models.Job{
		Name:       rf.Name,
		RemotePath: rf.RemotePath,
		LocalPath:  localPath,
		Status:     models.JobStatusQueued,
		Priority:   0,
		MaxRetries: h.config.GetJobs().MaxRetries,
	}
	job.FileSize = rf.Size

	if err := h.queue.Enqueue(job); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to enqueue job", err)
		return
	}

	if err := h.remoteFileRepo.LinkRemoteFileToJob(rf.ID, job.ID, models.FileStatusQueued); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to link file to job", err)
		return
	}

	h.writeSuccess(w, http.StatusCreated, job, "download queued")
}

// IgnoreRemoteFile marks a remote file as ignored.
func (h *Handlers) IgnoreRemoteFile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(mux.Vars(r)["id"])
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid file ID", err)
		return
	}

	if err := h.remoteFileRepo.UpdateRemoteFileStatus(id, models.FileStatusIgnored); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to ignore file", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, nil, "file marked as ignored")
}

// RestoreRemoteFile restores an ignored file back to on_seedbox.
func (h *Handlers) RestoreRemoteFile(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(mux.Vars(r)["id"])
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid file ID", err)
		return
	}

	if err := h.remoteFileRepo.UpdateRemoteFileStatus(id, models.FileStatusOnSeedbox); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to restore file", err)
		return
	}

	h.writeSuccess(w, http.StatusOK, nil, "file restored")
}

// TriggerScan triggers an immediate scan asynchronously.
func (h *Handlers) TriggerScan(w http.ResponseWriter, r *http.Request) {
	if h.scanner == nil {
		h.writeError(w, http.StatusServiceUnavailable, "scanner not configured", nil)
		return
	}

	go func() {
		if err := h.scanner.ScanNow(r.Context()); err != nil {
			// Errors are logged inside ScanNow; nothing more to do here.
			_ = err
		}
	}()

	h.writeSuccess(w, http.StatusAccepted, nil, "scan started")
}

// GetSyncStatus returns the current sync scanner status.
func (h *Handlers) GetSyncStatus(w http.ResponseWriter, r *http.Request) {
	type watchedPathResponse struct {
		RemotePath      string   `json:"remote_path"`
		Extensions      []string `json:"extensions"`
		ExcludePatterns []string `json:"exclude_patterns"`
		AutoDownload    bool     `json:"auto_download"`
		Recursive       bool     `json:"recursive"`
	}

	type syncStatusResponse struct {
		Enabled      bool                  `json:"enabled"`
		ScanInterval string                `json:"scan_interval"`
		LastScanAt   interface{}           `json:"last_scan_at"`
		FilesFound   int                   `json:"files_found"`
		ScanInFlight bool                  `json:"scan_in_flight"`
		Error        string                `json:"error,omitempty"`
		WatchedPaths []watchedPathResponse `json:"watched_paths"`
	}

	syncCfg := h.config.GetSync()

	resp := syncStatusResponse{
		Enabled:      syncCfg.Enabled,
		ScanInterval: syncCfg.ScanInterval.String(),
	}

	if h.scanner != nil {
		st := h.scanner.GetStatus()
		resp.LastScanAt = st.LastScanAt
		resp.FilesFound = st.FilesFound
		resp.ScanInFlight = st.ScanInFlight
		resp.Error = st.Error
	}

	for _, wp := range syncCfg.WatchedPaths {
		resp.WatchedPaths = append(resp.WatchedPaths, watchedPathResponse{
			RemotePath:      wp.RemotePath,
			Extensions:      wp.Extensions,
			ExcludePatterns: wp.ExcludePatterns,
			AutoDownload:    wp.AutoDownload,
			Recursive:       wp.Recursive,
		})
	}

	h.writeSuccess(w, http.StatusOK, resp, "")
}

// localPathForRemoteFile computes the local destination path for a remote file,
// preserving the directory structure relative to the watched path.
//
// Example:
//
//	remotePath  = "/seedbox/dp/Show.S01/episode.mkv"
//	watchedPath = "/seedbox/dp/"
//	base        = "/downloads/"
//	→ "/downloads/Show.S01/"
//
// Files sitting directly in the watched dir (no subdir) use base as-is.
func localPathForRemoteFile(remotePath, watchedPath, base string) string {
	rel := remotePath
	if strings.HasPrefix(remotePath, watchedPath) {
		rel = remotePath[len(watchedPath):]
	}
	dir := filepath.Dir(rel)
	if dir == "." || dir == "" {
		return base
	}
	return filepath.Join(base, dir) + "/"
}

// findWatchedPath finds the WatchedPath config matching a given remote_path prefix.
func (h *Handlers) findWatchedPath(watchedPath string) *config.WatchedPath {
	for i, wp := range h.config.GetSync().WatchedPaths {
		if wp.RemotePath == watchedPath {
			return &h.config.GetSync().WatchedPaths[i]
		}
	}
	return nil
}

// parseID parses a string path variable into an int64.
func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
