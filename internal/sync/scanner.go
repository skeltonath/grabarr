package sync

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"
	"grabarr/internal/models"
)

// ScannerRepo is the subset of repository operations the scanner needs.
type ScannerRepo interface {
	UpsertRemoteFile(file *models.RemoteFile) error
	GetRemoteFilesLinkedToJobs() ([]*models.RemoteFile, error)
	GetRemoteFileByPath(remotePath string) (*models.RemoteFile, error)
	UpdateRemoteFileStatus(id int64, status models.FileStatus) error
	LinkRemoteFileToJob(remoteFileID, jobID int64, status models.FileStatus) error
	DeleteStaleRemoteFiles(watchedPath string, seenAfter time.Time) error
}

// ScanStatus holds the result of the last scan.
type ScanStatus struct {
	LastScanAt   *time.Time
	FilesFound   int
	ScanInFlight bool
	Error        string
}

// Scanner periodically scans watched paths on the seedbox and reconciles
// the results with the remote_files table.
type Scanner struct {
	cfg    *config.Config
	repo   ScannerRepo
	queue  interfaces.JobQueue
	mu     sync.Mutex
	status ScanStatus
}

// New creates a new Scanner.
func New(cfg *config.Config, repo ScannerRepo, queue interfaces.JobQueue) *Scanner {
	return &Scanner{
		cfg:   cfg,
		repo:  repo,
		queue: queue,
	}
}

// Start launches the background scan loop. It returns immediately; scanning
// happens in a goroutine that respects ctx cancellation.
func (s *Scanner) Start(ctx context.Context) {
	syncCfg := s.cfg.GetSync()
	if !syncCfg.Enabled {
		slog.Info("sync scanner disabled by config")
		return
	}

	interval := syncCfg.ScanInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	slog.Info("starting sync scanner", "interval", interval, "watched_paths", len(syncCfg.WatchedPaths))

	// Full scan loop (SSH → find files, reconcile).
	go func() {
		if err := s.ScanNow(ctx); err != nil {
			slog.Error("initial scan failed", "error", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("sync scanner stopped")
				return
			case <-ticker.C:
				if err := s.ScanNow(ctx); err != nil {
					slog.Error("periodic scan failed", "error", err)
				}
			}
		}
	}()

	// Job status sync loop — runs more frequently so the Seedbox tab
	// reflects job completions without waiting for a full SSH scan.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.SyncJobStatuses(ctx); err != nil {
					slog.Error("job status sync failed", "error", err)
				}
			}
		}
	}()
}

// ScanNow triggers an immediate full scan across all watched paths.
// It is safe to call concurrently; if a scan is already running it returns
// an error rather than stacking another one.
func (s *Scanner) ScanNow(ctx context.Context) error {
	s.mu.Lock()
	if s.status.ScanInFlight {
		s.mu.Unlock()
		return fmt.Errorf("scan already in progress")
	}
	s.status.ScanInFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.status.ScanInFlight = false
		s.mu.Unlock()
	}()

	syncCfg := s.cfg.GetSync()
	scanStart := time.Now()
	totalFound := 0

	for _, wp := range syncCfg.WatchedPaths {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := s.scanPath(ctx, wp, scanStart)
		if err != nil {
			slog.Error("failed to scan path", "path", wp.RemotePath, "error", err)
			s.mu.Lock()
			s.status.Error = err.Error()
			s.mu.Unlock()
			continue
		}
		totalFound += n
	}

	// Sync job statuses for all files linked to a job.
	if err := s.SyncJobStatuses(ctx); err != nil {
		slog.Error("failed to sync job statuses", "error", err)
	}

	now := time.Now()
	s.mu.Lock()
	s.status.LastScanAt = &now
	s.status.FilesFound = totalFound
	s.status.Error = ""
	s.mu.Unlock()

	slog.Info("scan complete", "files_found", totalFound, "duration", time.Since(scanStart))
	return nil
}

// GetStatus returns the current scan status (safe to call from any goroutine).
func (s *Scanner) GetStatus() ScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// scanPath lists files under a single WatchedPath and reconciles them with the DB.
// Returns the number of files found.
func (s *Scanner) scanPath(ctx context.Context, wp config.WatchedPath, scanStart time.Time) (int, error) {
	excludeREs, err := compilePatterns(wp.ExcludePatterns)
	if err != nil {
		return 0, fmt.Errorf("invalid exclude_patterns: %w", err)
	}

	files, err := s.sshListFiles(ctx, wp, excludeREs)
	if err != nil {
		return 0, fmt.Errorf("ssh list files: %w", err)
	}

	for _, f := range files {
		if err := s.repo.UpsertRemoteFile(f); err != nil {
			slog.Error("failed to upsert remote file", "path", f.RemotePath, "error", err)
		}
	}

	// Auto-download: queue new on_seedbox files if configured.
	if wp.AutoDownload {
		s.autoQueueNewFiles(ctx, files, wp)
	}

	// Stale cleanup: remove records not seen in this scan.
	if err := s.repo.DeleteStaleRemoteFiles(wp.RemotePath, scanStart); err != nil {
		slog.Error("failed to delete stale remote files", "watched_path", wp.RemotePath, "error", err)
	}

	return len(files), nil
}

// sshListFiles SSHes into the seedbox and runs `find` to list files.
func (s *Scanner) sshListFiles(ctx context.Context, wp config.WatchedPath, excludeREs []*regexp.Regexp) ([]*models.RemoteFile, error) {
	rsyncCfg := s.cfg.GetRsync()

	// Build the find command.
	depth := ""
	if !wp.Recursive {
		depth = "-maxdepth 1"
	}

	extFilter := ""
	if len(wp.Extensions) > 0 {
		parts := make([]string, len(wp.Extensions))
		for i, ext := range wp.Extensions {
			parts[i] = fmt.Sprintf("-name '*.%s'", ext)
		}
		extFilter = "\\( " + strings.Join(parts, " -o ") + " \\)"
	}

	findCmd := fmt.Sprintf("find %s -type f %s %s -printf '%%p\\t%%s\\n' 2>/dev/null",
		wp.RemotePath, depth, extFilter)

	sshCmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=15",
		"-i", rsyncCfg.SSHKeyFile,
		fmt.Sprintf("%s@%s", rsyncCfg.SSHUser, rsyncCfg.SSHHost),
		findCmd,
	)

	var stdout, stderr bytes.Buffer
	sshCmd.Stdout = &stdout
	sshCmd.Stderr = &stderr

	if err := sshCmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh find failed: %w (stderr: %s)", err, stderr.String())
	}

	return parseSSHFindOutput(stdout.String(), wp.RemotePath, excludeREs), nil
}

// parseSSHFindOutput parses `find -printf '%p\t%s\n'` output into RemoteFile records.
// Files whose names match any of the excludeREs are skipped.
func parseSSHFindOutput(output, watchedPath string, excludeREs []*regexp.Regexp) []*models.RemoteFile {
	var files []*models.RemoteFile

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		remotePath := parts[0]
		size, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			size = 0
		}

		name := filepath.Base(remotePath)

		if matchesAny(name, excludeREs) {
			slog.Debug("excluding file matching exclude_pattern", "name", name)
			continue
		}

		ext := strings.TrimPrefix(filepath.Ext(name), ".")

		files = append(files, &models.RemoteFile{
			RemotePath:  remotePath,
			Name:        name,
			Size:        size,
			Extension:   ext,
			Status:      models.FileStatusOnSeedbox,
			WatchedPath: watchedPath,
			LastSeenAt:  time.Now(),
		})
	}

	return files
}

// compilePatterns compiles a list of regex strings. Returns an error if any pattern is invalid.
func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	res := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", p, err)
		}
		res = append(res, re)
	}
	return res, nil
}

// matchesAny returns true if s matches any of the provided regexes.
func matchesAny(s string, res []*regexp.Regexp) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// SyncJobStatuses updates remote_file.status to match the status of linked jobs.
func (s *Scanner) SyncJobStatuses(ctx context.Context) error {
	linked, err := s.repo.GetRemoteFilesLinkedToJobs()
	if err != nil {
		return fmt.Errorf("get linked remote files: %w", err)
	}

	for _, rf := range linked {
		if rf.JobID == nil {
			continue
		}
		job, err := s.queue.GetJob(*rf.JobID)
		if err != nil {
			// Job may have been deleted; unlink isn't worth the complexity, skip.
			continue
		}

		newStatus := remoteFileStatusFromJob(job.Status)
		if newStatus != rf.Status {
			if err := s.repo.UpdateRemoteFileStatus(rf.ID, newStatus); err != nil {
				slog.Error("failed to update remote file status", "id", rf.ID, "error", err)
			}
		}
	}

	return nil
}

// autoQueueNewFiles creates download jobs for files that are still on_seedbox.
func (s *Scanner) autoQueueNewFiles(ctx context.Context, files []*models.RemoteFile, wp config.WatchedPath) {
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return
		}

		// Re-read from DB to get current status (the upsert may not have set status for existing records).
		existing, err := s.repo.GetRemoteFileByPath(f.RemotePath)
		if err != nil || existing == nil {
			continue
		}
		if existing.Status != models.FileStatusOnSeedbox {
			continue
		}

		// Determine local path, preserving the relative directory structure.
		baseLocalPath := wp.LocalPath
		if baseLocalPath == "" {
			baseLocalPath = s.cfg.GetDownloads().LocalPath
		}
		localPath := localPathForRemoteFile(f.RemotePath, wp.RemotePath, baseLocalPath)

		job := &models.Job{
			Name:       f.Name,
			RemotePath: f.RemotePath,
			LocalPath:  localPath,
			Status:     models.JobStatusQueued,
			Priority:   0,
			MaxRetries: s.cfg.GetJobs().MaxRetries,
			FileSize:   f.Size,
		}

		if err := s.queue.Enqueue(job); err != nil {
			slog.Error("auto-queue failed", "path", f.RemotePath, "error", err)
			continue
		}

		if err := s.repo.LinkRemoteFileToJob(existing.ID, job.ID, models.FileStatusQueued); err != nil {
			slog.Error("failed to link remote file to auto-queued job", "file_id", existing.ID, "job_id", job.ID, "error", err)
		}
	}
}

// localPathForRemoteFile computes the local destination preserving the relative
// directory structure from watchedPath under baseLocalPath.
func localPathForRemoteFile(remotePath, watchedPath, baseLocalPath string) string {
	rel := remotePath
	if strings.HasPrefix(remotePath, watchedPath) {
		rel = remotePath[len(watchedPath):]
	}
	dir := filepath.Dir(rel)
	if dir == "." || dir == "" {
		return baseLocalPath
	}
	return filepath.Join(baseLocalPath, dir) + "/"
}

// remoteFileStatusFromJob maps a job status to the corresponding remote file status.
func remoteFileStatusFromJob(js models.JobStatus) models.FileStatus {
	switch js {
	case models.JobStatusQueued, models.JobStatusPending:
		return models.FileStatusQueued
	case models.JobStatusRunning:
		return models.FileStatusDownloading
	case models.JobStatusCompleted:
		return models.FileStatusDownloaded
	default:
		// failed, cancelled → back to on_seedbox so the user can retry
		return models.FileStatusOnSeedbox
	}
}
