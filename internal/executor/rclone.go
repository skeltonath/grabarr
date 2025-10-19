package executor

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"
	"grabarr/internal/models"
	"grabarr/internal/rclone"
)

type RCloneExecutor struct {
	config          *config.Config
	gatekeeper      interfaces.Gatekeeper
	progressChan    chan models.JobProgress
	client          interfaces.RCloneClient
	repo            interfaces.JobRepository
	progressMonitor *ProgressMonitor
}

func NewRCloneExecutor(cfg *config.Config, gatekeeper interfaces.Gatekeeper, repo interfaces.JobRepository) *RCloneExecutor {
	rcloneConfig := cfg.GetRClone()
	client := rclone.NewClient(fmt.Sprintf("http://%s", rcloneConfig.DaemonAddr))
	progressMonitor := NewProgressMonitor(client, repo)

	return &RCloneExecutor{
		config:          cfg,
		gatekeeper:      gatekeeper,
		progressChan:    make(chan models.JobProgress, 100),
		client:          client,
		repo:            repo,
		progressMonitor: progressMonitor,
	}
}

// Start starts the progress monitor
func (r *RCloneExecutor) Start(ctx context.Context) {
	r.progressMonitor.Start(ctx)
}

// Stop stops the progress monitor
func (r *RCloneExecutor) Stop() {
	r.progressMonitor.Stop()
}

func (r *RCloneExecutor) Execute(ctx context.Context, job *models.Job) error {
	slog.Info("starting rclone HTTP execution", "job_id", job.ID, "name", job.Name)

	// Check if daemon is responsive
	if err := r.client.Ping(ctx); err != nil {
		return fmt.Errorf("rclone daemon not responsive: %w", err)
	}

	// Prepare the copy operation using universal filter approach
	srcFs, dstFs, filter := r.prepareCopyRequest(job)

	// Convert download config to rclone config map
	var rcloneConfig map[string]interface{}
	if job.DownloadConfig != nil {
		rcloneConfig = job.DownloadConfig.ToRCloneConfig()
		slog.Info("using custom download config", "job_id", job.ID, "config", rcloneConfig)
	}

	// Single copy operation - works for both files and directories!
	copyResp, err := r.client.Copy(ctx, srcFs, dstFs, filter, rcloneConfig)

	if err != nil {
		return fmt.Errorf("failed to start copy operation: %w", err)
	}

	slog.Info("copy operation started", "job_id", job.ID, "rclone_job_id", copyResp.JobID)

	// Register with progress monitor
	r.progressMonitor.Register(copyResp.JobID, job)
	defer r.progressMonitor.Unregister(copyResp.JobID)

	// Monitor the job progress
	return r.monitorJob(ctx, job, copyResp.JobID)
}

// escapeGlobChars escapes special glob characters for rclone filters
func escapeGlobChars(s string) string {
	// Characters that need escaping in rclone glob patterns
	specialChars := []string{"[", "]", "*", "?", "{", "}"}
	result := s
	for _, char := range specialChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}
	return result
}

func (r *RCloneExecutor) prepareCopyRequest(job *models.Job) (string, string, map[string]interface{}) {
	rcloneConfig := r.config.GetRClone()

	// Always use parent directory as source
	parentDir := filepath.Dir(job.RemotePath)
	if parentDir == "." {
		parentDir = ""
	}
	srcFs := rcloneConfig.RemoteName + ":" + parentDir + "/"

	// Local destination (ensure trailing slash)
	dstFs := job.LocalPath
	if !strings.HasSuffix(dstFs, "/") {
		dstFs += "/"
	}

	// Universal filter that works for files and directories
	targetName := filepath.Base(job.RemotePath)
	// Escape glob special characters for rclone filters
	escapedName := escapeGlobChars(targetName)
	filter := map[string]interface{}{
		"IncludeRule": []string{
			escapedName,         // Match exact file
			escapedName + "/**", // Match directory contents
		},
	}

	slog.Info("prepared copy request",
		"job_id", job.ID,
		"src_fs", srcFs,
		"dst_fs", dstFs,
		"target_name", targetName,
		"filter", filter)

	return srcFs, dstFs, filter
}

func (r *RCloneExecutor) monitorJob(ctx context.Context, job *models.Job, rcloneJobID int64) error {
	// Check completion every 5 seconds (progress is handled by the global monitor)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Stop the rclone job if context is cancelled
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.client.StopJob(stopCtx, rcloneJobID); err != nil {
				slog.Error("failed to stop rclone job", "job_id", job.ID, "rclone_job_id", rcloneJobID, "error", err)
			}
			return ctx.Err()

		case <-ticker.C:
			status, err := r.client.GetJobStatus(ctx, rcloneJobID)
			if err != nil {
				slog.Warn("failed to get job status", "job_id", job.ID, "rclone_job_id", rcloneJobID, "error", err)
				continue
			}

			// Check if job is finished
			if status.Finished {
				// Final persist before returning
				if err := r.repo.UpdateJob(job); err != nil {
					slog.Error("failed to persist final job state", "job_id", job.ID, "error", err)
				}

				if !status.Success {
					return fmt.Errorf("rclone job failed: %s", status.Error)
				}
				slog.Info("rclone job completed successfully", "job_id", job.ID, "rclone_job_id", rcloneJobID)
				return nil
			}
		}
	}
}

func (r *RCloneExecutor) GetProgressChannel() <-chan models.JobProgress {
	return r.progressChan
}
