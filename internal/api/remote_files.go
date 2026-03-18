package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// GetRemoteFilesByPathPrefix returns all files whose remote_path starts with
	// watchedRoot+pathPrefix and whose status is on_seedbox.
	GetRemoteFilesByPathPrefix(watchedRoot, pathPrefix string) ([]*models.RemoteFile, error)
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

	for _, remote := range h.config.GetRemotes() {
		for _, wp := range remote.WatchedPaths {
			resp.WatchedPaths = append(resp.WatchedPaths, watchedPathResponse{
				RemotePath:      wp.RemotePath,
				Extensions:      wp.Extensions,
				ExcludePatterns: wp.ExcludePatterns,
				AutoDownload:    wp.AutoDownload,
				Recursive:       wp.Recursive,
			})
		}
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
	for _, remote := range h.config.GetRemotes() {
		for i, wp := range remote.WatchedPaths {
			if wp.RemotePath == watchedPath {
				return &remote.WatchedPaths[i]
			}
		}
	}
	return nil
}

// findWatchedPathRemoteName returns the remote name for the given watched path.
func (h *Handlers) findWatchedPathRemoteName(watchedPath string) string {
	for _, remote := range h.config.GetRemotes() {
		for _, wp := range remote.WatchedPaths {
			if wp.RemotePath == watchedPath {
				return remote.Name
			}
		}
	}
	return ""
}

// parseID parses a string path variable into an int64.
func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// ---- tree endpoint types ----

type treeFolderStats struct {
	FileCount   int   `json:"file_count"`
	TotalBytes  int64 `json:"total_bytes"`
	Downloaded  int   `json:"downloaded"`
	Queued      int   `json:"queued"`
	Downloading int   `json:"downloading"`
	Failed      int   `json:"failed"`
	OnSeedbox   int   `json:"on_seedbox"`
	Ignored     int   `json:"ignored"`
}

type treeFileProgress struct {
	JobID            int64            `json:"job_id"`
	JobStatus        models.JobStatus `json:"job_status"`
	Percentage       float64          `json:"percentage"`
	TransferSpeed    int64            `json:"transfer_speed"`
	TransferredBytes int64            `json:"transferred_bytes"`
	TotalBytes       int64            `json:"total_bytes"`
	ETA              *time.Time       `json:"eta,omitempty"`
	ErrorMessage     string           `json:"error_message,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	StartedAt        *time.Time       `json:"started_at,omitempty"`
	CompletedAt      *time.Time       `json:"completed_at,omitempty"`
}

type treeNode struct {
	Type string `json:"type"` // "folder" or "file"
	Name string `json:"name"`
	Path string `json:"path"`

	// folder-only fields
	Stats    *treeFolderStats `json:"stats,omitempty"`
	Children []*treeNode      `json:"children,omitempty"`

	// file-only fields
	ID        int64             `json:"id,omitempty"`
	SizeBytes int64             `json:"size_bytes,omitempty"`
	Status    models.FileStatus `json:"status,omitempty"`
	Progress  *treeFileProgress `json:"progress,omitempty"`
}

type treeRoot struct {
	WatchedPath string           `json:"watched_path"`
	Type        string           `json:"type"` // always "folder"
	Name        string           `json:"name"`
	Path        string           `json:"path"` // always "/"
	Stats       *treeFolderStats `json:"stats"`
	Children    []*treeNode      `json:"children"`
}

type treeResponse struct {
	ScannedAt time.Time   `json:"scanned_at"`
	Roots     []*treeRoot `json:"roots"`
}

// GetRemoteFileTree returns the full seedbox file hierarchy with per-folder stats.
func (h *Handlers) GetRemoteFileTree(w http.ResponseWriter, r *http.Request) {
	// Fetch all remote files (large limit; seedbox won't have 50k files).
	files, err := h.remoteFileRepo.GetRemoteFiles(models.RemoteFileFilter{Limit: 50000})
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list remote files", err)
		return
	}

	// Group files by watched path.
	byWatched := make(map[string][]*models.RemoteFile)
	for _, f := range files {
		byWatched[f.WatchedPath] = append(byWatched[f.WatchedPath], f)
	}

	// Determine scanned_at from scanner if available.
	scannedAt := time.Now()
	if h.scanner != nil {
		if st := h.scanner.GetStatus(); st.LastScanAt != nil {
			scannedAt = *st.LastScanAt
		}
	}

	// Build ordered list of watched paths.
	watchedPaths := make([]string, 0, len(byWatched))
	for wp := range byWatched {
		watchedPaths = append(watchedPaths, wp)
	}
	sort.Strings(watchedPaths)

	roots := make([]*treeRoot, 0, len(watchedPaths))
	for _, wp := range watchedPaths {
		wpFiles := byWatched[wp]

		// Collect job IDs to fetch.
		jobMap := make(map[int64]*models.Job)
		for _, f := range wpFiles {
			if f.JobID != nil {
				if job, err := h.queue.GetJob(*f.JobID); err == nil {
					jobMap[*f.JobID] = job
				} else {
					slog.Debug("tree: could not fetch job", "job_id", *f.JobID, "error", err)
				}
			}
		}

		// Use remote name as display name if available.
		displayName := h.findWatchedPathRemoteName(wp)
		root := buildFileTree(wp, displayName, wpFiles, jobMap)
		roots = append(roots, root)
	}

	h.writeSuccess(w, http.StatusOK, treeResponse{
		ScannedAt: scannedAt,
		Roots:     roots,
	}, "")
}

// buildFileTree constructs a treeRoot from a flat list of remote files belonging to one watched path.
func buildFileTree(watchedPath, displayName string, files []*models.RemoteFile, jobMap map[int64]*models.Job) *treeRoot {
	// folderMap maps relative folder paths (e.g. "/ShowName") to folder nodes.
	folderMap := make(map[string]*treeNode)

	rootName := displayName
	if rootName == "" {
		rootName = filepath.Base(strings.TrimSuffix(watchedPath, "/"))
	}
	if rootName == "" || rootName == "." {
		rootName = watchedPath
	}

	rootNode := &treeNode{
		Type:     "folder",
		Name:     rootName,
		Path:     "/",
		Stats:    &treeFolderStats{},
		Children: []*treeNode{},
	}
	folderMap["/"] = rootNode

	var ensureFolder func(path string)
	ensureFolder = func(path string) {
		if _, ok := folderMap[path]; ok {
			return
		}
		parent := filepath.Dir(path)
		if parent == "." || parent == "" {
			parent = "/"
		}
		ensureFolder(parent)

		node := &treeNode{
			Type:     "folder",
			Name:     filepath.Base(path),
			Path:     path,
			Stats:    &treeFolderStats{},
			Children: []*treeNode{},
		}
		folderMap[path] = node
		folderMap[parent].Children = append(folderMap[parent].Children, node)
	}

	for _, rf := range files {
		// Compute relative path.
		rel := rf.RemotePath
		if strings.HasPrefix(rel, watchedPath) {
			rel = rel[len(watchedPath):]
		}
		if !strings.HasPrefix(rel, "/") {
			rel = "/" + rel
		}

		dir := filepath.Dir(rel)
		if dir == "." || dir == "" {
			dir = "/"
		}
		ensureFolder(dir)

		// Build file node.
		fileNode := &treeNode{
			Type:      "file",
			Name:      rf.Name,
			Path:      rel,
			ID:        rf.ID,
			SizeBytes: rf.Size,
			Status:    rf.Status,
		}
		if rf.JobID != nil {
			if job, ok := jobMap[*rf.JobID]; ok {
				fileNode.Progress = &treeFileProgress{
					JobID:            job.ID,
					JobStatus:        job.Status,
					Percentage:       job.Progress.Percentage,
					TransferSpeed:    job.Progress.TransferSpeed,
					TransferredBytes: job.Progress.TransferredBytes,
					TotalBytes:       job.Progress.TotalBytes,
					ETA:              job.Progress.ETA,
					ErrorMessage:     job.ErrorMessage,
					CreatedAt:        job.CreatedAt,
					StartedAt:        job.StartedAt,
					CompletedAt:      job.CompletedAt,
				}
			}
		}

		folderMap[dir].Children = append(folderMap[dir].Children, fileNode)
	}

	// Roll up stats recursively.
	var rollup func(node *treeNode) *treeFolderStats
	rollup = func(node *treeNode) *treeFolderStats {
		stats := &treeFolderStats{}
		for _, child := range node.Children {
			if child.Type == "file" {
				stats.FileCount++
				stats.TotalBytes += child.SizeBytes
				effectiveStatus := child.Status
				if child.Progress != nil && child.Progress.JobStatus == models.JobStatusFailed {
					effectiveStatus = "failed"
				}
				switch effectiveStatus {
				case models.FileStatusDownloaded:
					stats.Downloaded++
				case models.FileStatusQueued:
					stats.Queued++
				case models.FileStatusDownloading:
					stats.Downloading++
				case models.FileStatusOnSeedbox:
					stats.OnSeedbox++
				case models.FileStatusIgnored:
					stats.Ignored++
				case "failed":
					stats.Failed++
				}
			} else {
				cs := rollup(child)
				stats.FileCount += cs.FileCount
				stats.TotalBytes += cs.TotalBytes
				stats.Downloaded += cs.Downloaded
				stats.Queued += cs.Queued
				stats.Downloading += cs.Downloading
				stats.OnSeedbox += cs.OnSeedbox
				stats.Ignored += cs.Ignored
				stats.Failed += cs.Failed
			}
		}
		node.Stats = stats
		return stats
	}
	rollup(rootNode)

	return &treeRoot{
		WatchedPath: watchedPath,
		Type:        "folder",
		Name:        rootNode.Name,
		Path:        "/",
		Stats:       rootNode.Stats,
		Children:    rootNode.Children,
	}
}

// ---- queue-folder endpoint ----

type queueFolderRequest struct {
	WatchedPath string `json:"watched_path"`
	FolderPath  string `json:"folder_path"`
}

type queueFolderResponse struct {
	Queued int `json:"queued"`
	Failed int `json:"failed"`
}

// QueueFolder creates download jobs for all on_seedbox files under the given folder path.
func (h *Handlers) QueueFolder(w http.ResponseWriter, r *http.Request) {
	var req queueFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body", err)
		return
	}

	if req.WatchedPath == "" || req.FolderPath == "" {
		h.writeError(w, http.StatusBadRequest, "watched_path and folder_path are required", nil)
		return
	}

	// Reject path traversal attempts.
	if strings.Contains(req.FolderPath, "..") || strings.Contains(req.WatchedPath, "..") {
		h.writeError(w, http.StatusBadRequest, "invalid path", nil)
		return
	}

	files, err := h.remoteFileRepo.GetRemoteFilesByPathPrefix(req.WatchedPath, req.FolderPath)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list files in folder", err)
		return
	}

	var queued, failed int
	for _, rf := range files {
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
			FileSize:   rf.Size,
		}

		if err := h.queue.Enqueue(job); err != nil {
			slog.Warn("queue-folder: failed to enqueue job", "file", rf.RemotePath, "error", err)
			failed++
			continue
		}

		if err := h.remoteFileRepo.LinkRemoteFileToJob(rf.ID, job.ID, models.FileStatusQueued); err != nil {
			slog.Warn("queue-folder: failed to link file to job", "file_id", rf.ID, "job_id", job.ID, "error", err)
			// Job was created but linking failed; count as queued anyway.
		}

		queued++
	}

	resp := queueFolderResponse{Queued: queued, Failed: failed}
	if queued == 0 && failed > 0 {
		h.writeError(w, http.StatusInternalServerError, "failed to queue any files", nil)
		return
	}

	h.writeSuccess(w, http.StatusOK, resp, "")
}
