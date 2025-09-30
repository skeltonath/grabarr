package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"
	"grabarr/internal/models"
	"grabarr/internal/rclone"
)

const MaxConcurrentSyncs = 1

type SyncService struct {
	config     *config.Config
	repository interfaces.SyncRepository
	client     interfaces.RCloneClient
}

func NewSyncService(cfg *config.Config, repo interfaces.SyncRepository) *SyncService {
	rcloneConfig := cfg.GetRClone()
	client := rclone.NewClient(fmt.Sprintf("http://%s", rcloneConfig.DaemonAddr))

	return &SyncService{
		config:     cfg,
		repository: repo,
		client:     client,
	}
}

func (s *SyncService) StartSync(ctx context.Context, remotePath string) (*models.SyncJob, error) {
	// Check if we've reached the max concurrent syncs
	activeCount, err := s.repository.GetActiveSyncJobsCount()
	if err != nil {
		return nil, fmt.Errorf("failed to check active sync count: %w", err)
	}

	if activeCount >= MaxConcurrentSyncs {
		return nil, fmt.Errorf("maximum concurrent syncs (%d) reached, please wait for existing sync to complete", MaxConcurrentSyncs)
	}

	// Check if daemon is responsive
	if err := s.client.Ping(ctx); err != nil {
		return nil, fmt.Errorf("rclone daemon not responsive: %w", err)
	}

	// Create sync job
	downloadsConfig := s.config.GetDownloads()
	syncJob := &models.SyncJob{
		RemotePath: remotePath,
		LocalPath:  downloadsConfig.LocalPath,
		Status:     models.SyncStatusQueued,
		Progress: models.SyncProgress{
			LastUpdateTime: time.Now(),
		},
		Stats: models.SyncStats{},
	}

	// Save to database
	if err := s.repository.CreateSyncJob(syncJob); err != nil {
		return nil, fmt.Errorf("failed to create sync job: %w", err)
	}

	// Start the sync operation asynchronously
	go s.executeSyncJob(context.Background(), syncJob)

	return syncJob, nil
}

func (s *SyncService) GetSyncJob(id int64) (*models.SyncJob, error) {
	return s.repository.GetSyncJob(id)
}

func (s *SyncService) GetSyncJobs(filter models.SyncFilter) ([]*models.SyncJob, error) {
	return s.repository.GetSyncJobs(filter)
}

func (s *SyncService) CancelSync(ctx context.Context, id int64) error {
	syncJob, err := s.repository.GetSyncJob(id)
	if err != nil {
		return fmt.Errorf("sync job not found: %w", err)
	}

	if !syncJob.IsActive() {
		return fmt.Errorf("sync job is not active")
	}

	// Stop the rclone job if it's running
	if syncJob.RCloneJobID != nil {
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := s.client.StopJob(stopCtx, *syncJob.RCloneJobID); err != nil {
			slog.Error("failed to stop rclone job", "sync_id", syncJob.ID, "rclone_job_id", *syncJob.RCloneJobID, "error", err)
		}
	}

	// Mark as cancelled
	syncJob.MarkCancelled()
	return s.repository.UpdateSyncJob(syncJob)
}

func (s *SyncService) GetSyncSummary() (*models.SyncSummary, error) {
	return s.repository.GetSyncSummary()
}

func (s *SyncService) executeSyncJob(ctx context.Context, syncJob *models.SyncJob) {
	slog.Info("starting sync job execution", "sync_id", syncJob.ID, "remote_path", syncJob.RemotePath)

	// Prepare the copy operation with --ignore-existing
	srcFs, dstFs, filter := s.prepareSyncRequest(syncJob)

	// Start the copy operation
	copyResp, err := s.client.CopyWithIgnoreExisting(ctx, srcFs, dstFs, filter)
	if err != nil {
		slog.Error("failed to start sync operation", "sync_id", syncJob.ID, "error", err)
		syncJob.MarkFailed(fmt.Sprintf("Failed to start sync: %v", err))
		s.repository.UpdateSyncJob(syncJob)
		return
	}

	slog.Info("sync operation started", "sync_id", syncJob.ID, "rclone_job_id", copyResp.JobID)

	// Mark as started
	syncJob.MarkStarted(copyResp.JobID)
	if err := s.repository.UpdateSyncJob(syncJob); err != nil {
		slog.Error("failed to update sync job", "sync_id", syncJob.ID, "error", err)
	}

	// Monitor the job progress
	s.monitorSyncJob(ctx, syncJob, copyResp.JobID)
}

func (s *SyncService) prepareSyncRequest(syncJob *models.SyncJob) (string, string, map[string]interface{}) {
	rcloneConfig := s.config.GetRClone()

	// Source filesystem - the remote path
	srcFs := rcloneConfig.RemoteName + ":" + syncJob.RemotePath
	if !strings.HasSuffix(srcFs, "/") {
		srcFs += "/"
	}

	// Destination filesystem - local path
	dstFs := syncJob.LocalPath
	if !strings.HasSuffix(dstFs, "/") {
		dstFs += "/"
	}

	// No specific filter needed for bulk sync - copy everything
	filter := map[string]interface{}{}

	slog.Info("prepared sync request",
		"sync_id", syncJob.ID,
		"src_fs", srcFs,
		"dst_fs", dstFs)

	return srcFs, dstFs, filter
}

func (s *SyncService) monitorSyncJob(ctx context.Context, syncJob *models.SyncJob, rcloneJobID int64) {
	ticker := time.NewTicker(2 * time.Second) // Poll every 2 seconds for sync jobs
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			status, err := s.client.GetJobStatus(ctx, rcloneJobID)
			if err != nil {
				slog.Error("failed to get sync job status", "sync_id", syncJob.ID, "rclone_job_id", rcloneJobID, "error", err)
				continue
			}

			// Update progress
			s.updateSyncProgress(syncJob, status)

			// Update sync job in database
			if err := s.repository.UpdateSyncJob(syncJob); err != nil {
				slog.Error("failed to update sync job", "sync_id", syncJob.ID, "error", err)
			}

			// Check if job is finished
			if status.Finished {
				if !status.Success {
					slog.Error("sync job failed", "sync_id", syncJob.ID, "rclone_job_id", rcloneJobID, "error", status.Error)
					syncJob.MarkFailed(status.Error)
				} else {
					slog.Info("sync job completed successfully", "sync_id", syncJob.ID, "rclone_job_id", rcloneJobID)

					// Calculate final stats
					stats := models.SyncStats{
						FilesTransferred: int(status.Output.Transfers),
						TotalFiles:       int(status.Output.TotalTransfers),
						BytesTransferred: status.Output.Bytes,
						TotalBytes:       status.Output.TotalBytes,
						FilesSkipped:     int(status.Output.TotalTransfers - status.Output.Transfers), // Approximation
						FilesErrored:     int(status.Output.Errors),
					}

					syncJob.MarkCompleted(stats)
				}

				// Final update
				s.repository.UpdateSyncJob(syncJob)
				return
			}
		}
	}
}

func (s *SyncService) updateSyncProgress(syncJob *models.SyncJob, status *models.RCloneJobStatus) {
	progress := models.SyncProgress{
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

	// Update sync job progress
	syncJob.UpdateProgress(progress)

	slog.Debug("updated sync progress",
		"sync_id", syncJob.ID,
		"percentage", progress.Percentage,
		"transferred", progress.TransferredBytes,
		"total", progress.TotalBytes,
		"speed", progress.TransferSpeed)
}