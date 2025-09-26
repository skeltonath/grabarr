package queue

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/models"
	"grabarr/internal/repository"
)

type JobQueue interface {
	Start(ctx context.Context) error
	Stop() error
	Enqueue(job *models.Job) error
	GetJob(id int64) (*models.Job, error)
	GetJobs(filter models.JobFilter) ([]*models.Job, error)
	CancelJob(id int64) error
	GetSummary() (*models.JobSummary, error)
	SetJobExecutor(executor JobExecutor)
}

type JobExecutor interface {
	Execute(ctx context.Context, job *models.Job) error
	CanExecute() bool
}

type queue struct {
	repo     *repository.Repository
	config   *config.Config
	executor JobExecutor

	// Internal state
	mu              sync.RWMutex
	running         bool
	activeJobs      map[int64]context.CancelFunc
	jobQueue        chan *models.Job
	schedulerCtx    context.Context
	schedulerCancel context.CancelFunc

	// Resource management
	resourceChecker ResourceChecker

	// Cleanup
	lastCleanup time.Time
}

type ResourceChecker interface {
	CanScheduleJob() bool
	GetResourceStatus() ResourceStatus
}

type ResourceStatus struct {
	BandwidthAvailable bool    `json:"bandwidth_available"`
	BandwidthUsage     float64 `json:"bandwidth_usage_percent"`
	DiskSpaceAvailable bool    `json:"disk_space_available"`
	CacheDiskFree      int64   `json:"cache_disk_free_bytes"`
	ArrayDiskFree      int64   `json:"array_disk_free_bytes"`
}

func New(repo *repository.Repository, config *config.Config, resourceChecker ResourceChecker) JobQueue {
	return &queue{
		repo:            repo,
		config:          config,
		activeJobs:      make(map[int64]context.CancelFunc),
		jobQueue:        make(chan *models.Job, 1000), // Buffered channel for job queue
		resourceChecker: resourceChecker,
		lastCleanup:     time.Now(),
	}
}

func (q *queue) SetJobExecutor(executor JobExecutor) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.executor = executor
}

func (q *queue) Start(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.running {
		return fmt.Errorf("queue already running")
	}

	if q.executor == nil {
		return fmt.Errorf("job executor not set")
	}

	q.running = true
	q.schedulerCtx, q.schedulerCancel = context.WithCancel(ctx)

	// Load existing queued/pending jobs from database
	if err := q.loadExistingJobs(); err != nil {
		return fmt.Errorf("failed to load existing jobs: %w", err)
	}

	// Start the scheduler goroutine
	go q.scheduler()

	// Start cleanup goroutine
	go q.cleanupRoutine()

	slog.Info("job queue started")
	return nil
}

func (q *queue) Stop() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.running {
		return nil
	}

	q.running = false

	// Cancel scheduler
	if q.schedulerCancel != nil {
		q.schedulerCancel()
	}

	// Cancel all active jobs
	for jobID, cancel := range q.activeJobs {
		slog.Info("cancelling active job", "job_id", jobID)
		cancel()
	}

	// Wait for jobs to finish or timeout
	timeout := time.After(q.config.GetServer().ShutdownTimeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			slog.Warn("timeout waiting for jobs to finish", "active_jobs", len(q.activeJobs))
			return nil
		case <-ticker.C:
			if len(q.activeJobs) == 0 {
				slog.Info("all jobs finished, queue stopped")
				return nil
			}
		}
	}
}

func (q *queue) Enqueue(job *models.Job) error {
	// Set defaults
	if job.Status == "" {
		job.Status = models.JobStatusQueued
	}
	if job.MaxRetries == 0 {
		job.MaxRetries = q.config.GetJobs().MaxRetries
	}

	// Create job in database
	if err := q.repo.CreateJob(job); err != nil {
		return fmt.Errorf("failed to create job in database: %w", err)
	}

	// Add to in-memory queue
	select {
	case q.jobQueue <- job:
		slog.Info("job enqueued", "job_id", job.ID, "name", job.Name)
		return nil
	default:
		// Queue is full, job is still in database but not in memory queue
		slog.Warn("job queue full, job saved to database", "job_id", job.ID)
		return nil
	}
}

func (q *queue) GetJob(id int64) (*models.Job, error) {
	return q.repo.GetJob(id)
}

func (q *queue) GetJobs(filter models.JobFilter) ([]*models.Job, error) {
	return q.repo.GetJobs(filter)
}

func (q *queue) CancelJob(id int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Cancel if currently running
	if cancel, exists := q.activeJobs[id]; exists {
		cancel()
		delete(q.activeJobs, id)
	}

	// Update job status in database
	job, err := q.repo.GetJob(id)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	if !job.IsCompleted() {
		job.MarkCancelled()
		if err := q.repo.UpdateJob(job); err != nil {
			return fmt.Errorf("failed to update job status: %w", err)
		}
	}

	slog.Info("job cancelled", "job_id", id)
	return nil
}

func (q *queue) GetSummary() (*models.JobSummary, error) {
	return q.repo.GetJobSummary()
}

func (q *queue) loadExistingJobs() error {
	// Load jobs that need to be recovered: queued, pending, and running
	jobs, err := q.repo.GetJobs(models.JobFilter{
		Status: []models.JobStatus{models.JobStatusQueued, models.JobStatusPending, models.JobStatusRunning},
		SortBy: "priority",
		SortOrder: "DESC",
	})
	if err != nil {
		return err
	}

	for _, job := range jobs {
		// Reset pending and running jobs to queued for recovery
		if job.Status == models.JobStatusPending || job.Status == models.JobStatusRunning {
			oldStatus := job.Status
			job.Status = models.JobStatusQueued
			if err := q.repo.UpdateJob(job); err != nil {
				slog.Error("failed to reset job to queued", "job_id", job.ID, "old_status", oldStatus, "error", err)
				continue
			}
			slog.Info("recovered interrupted job", "job_id", job.ID, "name", job.Name, "previous_status", oldStatus)
		}

		select {
		case q.jobQueue <- job:
		default:
			slog.Warn("job queue full during startup, some jobs may be delayed", "job_id", job.ID)
		}
	}

	slog.Info("loaded existing jobs", "count", len(jobs))
	return nil
}

func (q *queue) scheduler() {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-q.schedulerCtx.Done():
			return
		case <-ticker.C:
			q.processQueue()
		case job := <-q.jobQueue:
			// Process job immediately if resources allow
			if q.canScheduleNewJob() && q.resourceChecker.CanScheduleJob() {
				q.scheduleJob(job)
			} else {
				// Put job back in queue for later
				job.Status = models.JobStatusPending
				if err := q.repo.UpdateJob(job); err != nil {
					slog.Error("failed to update job status to pending", "job_id", job.ID, "error", err)
				}

				select {
				case q.jobQueue <- job:
				default:
					slog.Error("failed to re-queue job", "job_id", job.ID)
				}
			}
		}
	}
}

func (q *queue) processQueue() {
	if !q.canScheduleNewJob() || !q.resourceChecker.CanScheduleJob() {
		return
	}

	// Try to process jobs from the queue
	for q.canScheduleNewJob() && q.resourceChecker.CanScheduleJob() {
		select {
		case job := <-q.jobQueue:
			q.scheduleJob(job)
		default:
			// No jobs in queue, try to load from database
			jobs, err := q.repo.GetJobs(models.JobFilter{
				Status: []models.JobStatus{models.JobStatusQueued, models.JobStatusPending},
				SortBy: "priority",
				SortOrder: "DESC",
				Limit: 10,
			})
			if err != nil {
				slog.Error("failed to load jobs from database", "error", err)
				return
			}

			if len(jobs) == 0 {
				return // No more jobs to process
			}

			// Add jobs to queue
			for _, job := range jobs {
				if q.canScheduleNewJob() && q.resourceChecker.CanScheduleJob() {
					q.scheduleJob(job)
				} else {
					break
				}
			}
			return
		}
	}
}

func (q *queue) canScheduleNewJob() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	maxConcurrent := q.config.GetJobs().MaxConcurrent
	return len(q.activeJobs) < maxConcurrent
}

func (q *queue) scheduleJob(job *models.Job) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Create context for this job
	ctx, cancel := context.WithCancel(q.schedulerCtx)
	q.activeJobs[job.ID] = cancel

	// Start job execution in goroutine
	go func() {
		defer func() {
			q.mu.Lock()
			delete(q.activeJobs, job.ID)
			q.mu.Unlock()
		}()

		q.executeJob(ctx, job)
	}()

	slog.Info("job scheduled", "job_id", job.ID, "name", job.Name)
}

func (q *queue) executeJob(ctx context.Context, job *models.Job) {
	// Mark job as started
	job.MarkStarted()
	if err := q.repo.UpdateJob(job); err != nil {
		slog.Error("failed to mark job as started", "job_id", job.ID, "error", err)
		return
	}

	// Create job attempt record
	attempt := &models.JobAttempt{
		JobID:      job.ID,
		AttemptNum: job.Retries + 1,
		Status:     models.JobStatusRunning,
	}
	if err := q.repo.CreateJobAttempt(attempt); err != nil {
		slog.Error("failed to create job attempt", "job_id", job.ID, "error", err)
		// Continue execution despite logging error
	}

	// Execute the job
	err := q.executor.Execute(ctx, job)

	// Update attempt record
	now := time.Now()
	attempt.EndedAt = &now

	if err != nil {
		slog.Error("job execution failed", "job_id", job.ID, "attempt", attempt.AttemptNum, "error", err)

		attempt.Status = models.JobStatusFailed
		attempt.ErrorMessage = err.Error()

		// Handle retry logic
		if job.CanRetry() {
			backoff := q.calculateRetryBackoff(job.Retries)
			slog.Info("job will be retried", "job_id", job.ID, "retry_in", backoff)

			job.IncrementRetry()
			if err := q.repo.UpdateJob(job); err != nil {
				slog.Error("failed to update job for retry", "job_id", job.ID, "error", err)
			}

			// Schedule retry
			go func() {
				time.Sleep(backoff)
				select {
				case q.jobQueue <- job:
				case <-q.schedulerCtx.Done():
				}
			}()
		} else {
			job.MarkFailed(err.Error())
			if err := q.repo.UpdateJob(job); err != nil {
				slog.Error("failed to mark job as failed", "job_id", job.ID, "error", err)
			}

			// TODO: Send notification about failed job
		}
	} else {
		slog.Info("job completed successfully", "job_id", job.ID)

		attempt.Status = models.JobStatusCompleted
		job.MarkCompleted()

		if err := q.repo.UpdateJob(job); err != nil {
			slog.Error("failed to mark job as completed", "job_id", job.ID, "error", err)
		}
	}

	// Update attempt record
	if err := q.repo.UpdateJobAttempt(attempt); err != nil {
		slog.Error("failed to update job attempt", "job_id", job.ID, "error", err)
	}
}

func (q *queue) calculateRetryBackoff(retryCount int) time.Duration {
	cfg := q.config.GetJobs()

	// Exponential backoff: base * (2 ^ retryCount)
	backoff := cfg.RetryBackoffBase * time.Duration(math.Pow(2, float64(retryCount)))

	// Cap at maximum backoff
	if backoff > cfg.RetryBackoffMax {
		backoff = cfg.RetryBackoffMax
	}

	return backoff
}

func (q *queue) cleanupRoutine() {
	ticker := time.NewTicker(1 * time.Hour) // Run cleanup every hour
	defer ticker.Stop()

	for {
		select {
		case <-q.schedulerCtx.Done():
			return
		case <-ticker.C:
			q.performCleanup()
		}
	}
}

func (q *queue) performCleanup() {
	cfg := q.config.GetJobs()
	now := time.Now()

	completedBefore := now.Add(-cfg.CleanupCompletedAfter)
	failedBefore := now.Add(-cfg.CleanupFailedAfter)

	count, err := q.repo.CleanupOldJobs(completedBefore, failedBefore)
	if err != nil {
		slog.Error("failed to cleanup old jobs", "error", err)
		return
	}

	if count > 0 {
		slog.Info("cleaned up old jobs", "count", count)
	}

	// Update last cleanup time
	if err := q.repo.SetConfig("last_cleanup", now.Format(time.RFC3339)); err != nil {
		slog.Error("failed to update last cleanup time", "error", err)
	}
}