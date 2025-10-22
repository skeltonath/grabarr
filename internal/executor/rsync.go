package executor

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"
	"grabarr/internal/models"
	"grabarr/internal/rsync"
)

type RsyncExecutor struct {
	config     *config.Config
	gatekeeper interfaces.Gatekeeper
	client     *rsync.Client
	repo       interfaces.JobRepository
}

func NewRsyncExecutor(cfg *config.Config, gatekeeper interfaces.Gatekeeper, repo interfaces.JobRepository) *RsyncExecutor {
	rsyncConfig := cfg.GetRsync()
	client := rsync.NewClient(rsyncConfig.SSHHost, rsyncConfig.SSHUser, rsyncConfig.SSHKeyFile)

	return &RsyncExecutor{
		config:     cfg,
		gatekeeper: gatekeeper,
		client:     client,
		repo:       repo,
	}
}

// Start is a no-op for rsync (no daemon needed)
func (r *RsyncExecutor) Start(ctx context.Context) {
	slog.Info("rsync executor initialized")
}

// Stop is a no-op for rsync
func (r *RsyncExecutor) Stop() {
	slog.Info("rsync executor stopped")
}

func (r *RsyncExecutor) Execute(ctx context.Context, job *models.Job) error {
	slog.Info("starting rsync execution", "job_id", job.ID, "name", job.Name)

	// Prepare rsync paths
	remotePath := job.RemotePath
	localPath := job.LocalPath

	// Ensure local path exists and is a directory
	// rsync will create the target file/directory inside localPath
	if !filepath.IsAbs(localPath) {
		return fmt.Errorf("local path must be absolute: %s", localPath)
	}

	slog.Info("prepared rsync request",
		"job_id", job.ID,
		"remote_path", remotePath,
		"local_path", localPath)

	// Start the transfer
	transfer, err := r.client.Copy(ctx, remotePath, localPath)
	if err != nil {
		return fmt.Errorf("failed to start rsync: %w", err)
	}

	slog.Info("rsync transfer started", "job_id", job.ID)

	// Monitor progress in a goroutine
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		for progress := range transfer.ProgressChan() {
			// Update job progress
			job.Progress.Percentage = progress.Percentage
			job.Progress.TransferredBytes = progress.TransferredBytes
			job.Progress.TransferSpeed = progress.TransferSpeed
			job.Progress.LastUpdateTime = progress.LastUpdateTime
			if progress.ETA != nil {
				job.Progress.ETA = progress.ETA
			}

			// Persist to database
			if err := r.repo.UpdateJob(job); err != nil {
				slog.Error("failed to update job progress", "job_id", job.ID, "error", err)
			}
		}
	}()

	// Wait for transfer to complete or context cancellation
	select {
	case <-ctx.Done():
		// Context cancelled, stop the transfer
		transfer.Stop()
		<-progressDone // Wait for progress goroutine to finish
		return ctx.Err()

	case err := <-transfer.Done():
		// Transfer completed or failed
		<-progressDone // Wait for progress goroutine to finish

		// Final persist
		if err := r.repo.UpdateJob(job); err != nil {
			slog.Error("failed to persist final job state", "job_id", job.ID, "error", err)
		}

		if err != nil {
			return fmt.Errorf("rsync transfer failed: %w", err)
		}

		slog.Info("rsync transfer completed successfully", "job_id", job.ID)
		return nil
	}
}

func (r *RsyncExecutor) GetProgressChannel() <-chan models.JobProgress {
	// rsync executor doesn't use a shared progress channel
	// Progress is handled directly in Execute()
	return nil
}
