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
	config       *config.Config
	gatekeeper   interfaces.Gatekeeper
	progressChan chan models.JobProgress
	client       interfaces.RCloneClient
	repo         interfaces.JobRepository
}

func NewRCloneExecutor(cfg *config.Config, gatekeeper interfaces.Gatekeeper, repo interfaces.JobRepository) *RCloneExecutor {
	rcloneConfig := cfg.GetRClone()
	client := rclone.NewClient(fmt.Sprintf("http://%s", rcloneConfig.DaemonAddr))

	return &RCloneExecutor{
		config:       cfg,
		gatekeeper:   gatekeeper,
		progressChan: make(chan models.JobProgress, 100),
		client:       client,
		repo:         repo,
	}
}

func (r *RCloneExecutor) Execute(ctx context.Context, job *models.Job) error {
	slog.Info("starting rclone HTTP execution", "job_id", job.ID, "name", job.Name)

	// Check if daemon is responsive
	if err := r.client.Ping(ctx); err != nil {
		return fmt.Errorf("rclone daemon not responsive: %w", err)
	}

	// Prepare the copy operation using universal filter approach
	srcFs, dstFs, filter := r.prepareCopyRequest(job)

	// Single copy operation - works for both files and directories!
	copyResp, err := r.client.Copy(ctx, srcFs, dstFs, filter)

	if err != nil {
		return fmt.Errorf("failed to start copy operation: %w", err)
	}

	slog.Info("copy operation started", "job_id", job.ID, "rclone_job_id", copyResp.JobID)

	// Monitor the job progress
	return r.monitorJob(ctx, job, copyResp.JobID)
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
	filter := map[string]interface{}{
		"IncludeRule": []string{
			targetName,         // Match exact file
			targetName + "/**", // Match directory contents
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
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Track when we last persisted progress to database
	lastPersist := time.Now()
	persistInterval := 2 * time.Second

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
				slog.Error("failed to get job status", "job_id", job.ID, "rclone_job_id", rcloneJobID, "error", err)
				continue
			}

			// Update progress in memory
			r.updateJobProgress(job, status)

			// Persist to database periodically (every 2 seconds)
			now := time.Now()
			if now.Sub(lastPersist) >= persistInterval {
				if err := r.repo.UpdateJob(job); err != nil {
					slog.Error("failed to persist job progress", "job_id", job.ID, "error", err)
				} else {
					lastPersist = now
					slog.Debug("persisted job progress", "job_id", job.ID, "percentage", job.Progress.Percentage)
				}
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

func (r *RCloneExecutor) updateJobProgress(job *models.Job, status *models.RCloneJobStatus) {
	progress := models.JobProgress{
		LastUpdateTime: time.Now(),
	}

	// Extract progress information from status
	output := status.Output
	if output.TotalBytes > 0 {
		progress.TotalBytes = output.TotalBytes
		progress.TransferredBytes = output.Bytes
		progress.Percentage = float64(output.Bytes) / float64(output.TotalBytes) * 100
	}

	if output.TotalTransfers > 0 {
		progress.FilesTotal = int(output.TotalTransfers)
		progress.FilesCompleted = int(output.Transfers)
	}

	progress.TransferSpeed = int64(output.Speed)

	// Estimate ETA if we have transfer speed
	if output.Speed > 0 && output.TotalBytes > 0 {
		remainingBytes := output.TotalBytes - output.Bytes
		etaSeconds := float64(remainingBytes) / output.Speed
		eta := time.Now().Add(time.Duration(etaSeconds) * time.Second)
		progress.ETA = &eta
	}

	// Update job progress
	job.UpdateProgress(progress)

	// Send progress update (non-blocking)
	select {
	case r.progressChan <- progress:
	default:
	}

	slog.Debug("updated job progress",
		"job_id", job.ID,
		"percentage", progress.Percentage,
		"transferred", progress.TransferredBytes,
		"total", progress.TotalBytes,
		"speed", progress.TransferSpeed)
}

func (r *RCloneExecutor) GetProgressChannel() <-chan models.JobProgress {
	return r.progressChan
}
