package executor

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"grabarr/internal/interfaces"
	"grabarr/internal/models"
)

// ProgressMonitor polls /core/stats and updates all registered jobs
type ProgressMonitor struct {
	client interfaces.RCloneClient
	repo   interfaces.JobRepository

	mu   sync.RWMutex
	jobs map[int64]*models.Job // rcloneJobID -> Job

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewProgressMonitor creates a new progress monitor
func NewProgressMonitor(client interfaces.RCloneClient, repo interfaces.JobRepository) *ProgressMonitor {
	return &ProgressMonitor{
		client: client,
		repo:   repo,
		jobs:   make(map[int64]*models.Job),
	}
}

// Start begins polling for progress updates
func (pm *ProgressMonitor) Start(ctx context.Context) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.cancel != nil {
		slog.Warn("progress monitor already started")
		return
	}

	pm.ctx, pm.cancel = context.WithCancel(ctx)

	pm.wg.Add(1)
	go pm.pollLoop()

	slog.Info("progress monitor started")
}

// Stop stops the progress monitor
func (pm *ProgressMonitor) Stop() {
	pm.mu.Lock()
	if pm.cancel == nil {
		pm.mu.Unlock()
		return
	}

	pm.cancel()
	pm.mu.Unlock()

	pm.wg.Wait()
	slog.Info("progress monitor stopped")
}

// Register adds a job to be monitored
func (pm *ProgressMonitor) Register(rcloneJobID int64, job *models.Job) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.jobs[rcloneJobID] = job
	slog.Debug("registered job for progress monitoring", "job_id", job.ID, "rclone_job_id", rcloneJobID)
}

// Unregister removes a job from monitoring
func (pm *ProgressMonitor) Unregister(rcloneJobID int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.jobs, rcloneJobID)
	slog.Debug("unregistered job from progress monitoring", "rclone_job_id", rcloneJobID)
}

// pollLoop continuously polls /core/stats and updates jobs
func (pm *ProgressMonitor) pollLoop() {
	defer pm.wg.Done()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastPersist := time.Now()
	persistInterval := 2 * time.Second

	for {
		select {
		case <-pm.ctx.Done():
			return

		case <-ticker.C:
			if err := pm.updateAllJobs(); err != nil {
				slog.Error("failed to update job progress", "error", err)
				continue
			}

			// Persist jobs to database periodically
			now := time.Now()
			if now.Sub(lastPersist) >= persistInterval {
				pm.persistJobs()
				lastPersist = now
			}
		}
	}
}

// updateAllJobs fetches /core/stats and updates all registered jobs
func (pm *ProgressMonitor) updateAllJobs() error {
	stats, err := pm.client.GetCoreStats(pm.ctx)
	if err != nil {
		return fmt.Errorf("failed to get core stats: %w", err)
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Create a map of group -> transfer stats for quick lookup
	transferMap := make(map[int64]*struct {
		bytes      int64
		totalBytes int64
		speed      float64
		percentage int
		eta        *int64
	})

	for _, tf := range stats.Transferring {
		// Extract job ID from group (format: "job/497670")
		rcloneJobID := extractJobIDFromGroup(tf.Group)
		if rcloneJobID == 0 {
			continue
		}

		transferMap[rcloneJobID] = &struct {
			bytes      int64
			totalBytes int64
			speed      float64
			percentage int
			eta        *int64
		}{
			bytes:      tf.Bytes,
			totalBytes: tf.Size,
			speed:      tf.SpeedAvg,
			percentage: tf.Percentage,
			eta:        tf.ETA,
		}
	}

	// Update each registered job
	for rcloneJobID, job := range pm.jobs {
		if transfer, found := transferMap[rcloneJobID]; found {
			// Active transfer - update with current stats
			pm.updateJobProgress(job, transfer.bytes, transfer.totalBytes, transfer.speed, transfer.percentage, transfer.eta)
		} else {
			// No active transfer for this job (might be queued, checking, or just finished)
			// Use global stats if available
			if stats.TotalBytes > 0 {
				percentage := float64(stats.Bytes) / float64(stats.TotalBytes) * 100
				pm.updateJobProgress(job, stats.Bytes, stats.TotalBytes, stats.Speed, int(percentage), stats.ETA)
			}
		}
	}

	return nil
}

// updateJobProgress updates a job's progress information
func (pm *ProgressMonitor) updateJobProgress(job *models.Job, bytes, totalBytes int64, speed float64, percentage int, eta *int64) {
	progress := models.JobProgress{
		LastUpdateTime:   time.Now(),
		TransferredBytes: bytes,
		TotalBytes:       totalBytes,
		TransferSpeed:    int64(speed),
		Percentage:       float64(percentage),
	}

	// Calculate ETA if provided
	if eta != nil && *eta > 0 {
		etaTime := time.Now().Add(time.Duration(*eta) * time.Second)
		progress.ETA = &etaTime
	}

	job.UpdateProgress(progress)

	slog.Debug("updated job progress",
		"job_id", job.ID,
		"percentage", progress.Percentage,
		"transferred", progress.TransferredBytes,
		"total", progress.TotalBytes,
		"speed", progress.TransferSpeed)
}

// persistJobs saves all registered jobs to the database
func (pm *ProgressMonitor) persistJobs() {
	pm.mu.RLock()
	jobsToUpdate := make([]*models.Job, 0, len(pm.jobs))
	for _, job := range pm.jobs {
		jobsToUpdate = append(jobsToUpdate, job)
	}
	pm.mu.RUnlock()

	for _, job := range jobsToUpdate {
		if err := pm.repo.UpdateJob(job); err != nil {
			slog.Error("failed to persist job progress", "job_id", job.ID, "error", err)
		}
	}

	if len(jobsToUpdate) > 0 {
		slog.Debug("persisted job progress", "count", len(jobsToUpdate))
	}
}

// extractJobIDFromGroup extracts the rclone job ID from a group string like "job/497670"
func extractJobIDFromGroup(group string) int64 {
	parts := strings.Split(group, "/")
	if len(parts) != 2 || parts[0] != "job" {
		return 0
	}

	jobID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}

	return jobID
}
