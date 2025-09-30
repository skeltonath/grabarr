package interfaces

import (
	"context"

	"grabarr/internal/models"
)

// JobQueue manages the job queue, scheduling, and execution
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

// JobExecutor executes individual jobs
type JobExecutor interface {
	Execute(ctx context.Context, job *models.Job) error
	CanExecute() bool
}

// ResourceChecker checks system resources before scheduling jobs
type ResourceChecker interface {
	CanScheduleJob() bool
	GetResourceStatus() ResourceStatus
}

// ResourceStatus represents system resource availability
type ResourceStatus struct {
	BandwidthAvailable bool    `json:"bandwidth_available"`
	BandwidthUsage     float64 `json:"bandwidth_usage_percent"`
	DiskSpaceAvailable bool    `json:"disk_space_available"`
	CacheDiskFree      int64   `json:"cache_disk_free_bytes"`
	ArrayDiskFree      int64   `json:"array_disk_free_bytes"`
}

// ResourceMonitor provides system resource metrics
type ResourceMonitor interface {
	GetResourceStatus() ResourceStatus
	GetMetrics() map[string]interface{}
}

// SyncService manages sync operations
type SyncService interface {
	StartSync(ctx context.Context, remotePath string) (*models.SyncJob, error)
	GetSyncJob(id int64) (*models.SyncJob, error)
	GetSyncJobs(filter models.SyncFilter) ([]*models.SyncJob, error)
	CancelSync(ctx context.Context, id int64) error
	GetSyncSummary() (*models.SyncSummary, error)
}

// SyncRepository provides database access for sync jobs
type SyncRepository interface {
	CreateSyncJob(syncJob *models.SyncJob) error
	GetSyncJob(id int64) (*models.SyncJob, error)
	GetSyncJobs(filter models.SyncFilter) ([]*models.SyncJob, error)
	UpdateSyncJob(syncJob *models.SyncJob) error
	DeleteSyncJob(id int64) error
	GetSyncSummary() (*models.SyncSummary, error)
	GetActiveSyncJobsCount() (int, error)
}

// BandwidthMonitor monitors network bandwidth usage
type BandwidthMonitor interface {
	GetCurrentUsage() (float64, error) // Returns usage percentage
	IsAvailable() bool
}

// RCloneClient provides an interface to interact with RClone daemon
type RCloneClient interface {
	Copy(ctx context.Context, srcFs, dstFs string, filter map[string]interface{}) (*models.RCloneCopyResponse, error)
	CopyWithIgnoreExisting(ctx context.Context, srcFs, dstFs string, filter map[string]interface{}) (*models.RCloneCopyResponse, error)
	GetJobStatus(ctx context.Context, jobID int64) (*models.RCloneJobStatus, error)
	ListJobs(ctx context.Context) (*models.RCloneJobListResponse, error)
	StopJob(ctx context.Context, jobID int64) error
	Ping(ctx context.Context) error
}