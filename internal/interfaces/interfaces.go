package interfaces

import (
	"context"

	"grabarr/internal/models"
	"grabarr/internal/rclone"
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
}

// Gatekeeper manages resource constraints and enforces operational rules
type Gatekeeper interface {
	Start() error
	Stop() error
	CanStartJob(fileSize int64) GateDecision
	CanStartSync() GateDecision
	GetResourceStatus() GatekeeperResourceStatus
}

// GateDecision represents whether an operation can proceed
type GateDecision struct {
	Allowed bool
	Reason  string
	Details map[string]interface{}
}

// GatekeeperResourceStatus provides current resource status
type GatekeeperResourceStatus struct {
	BandwidthUsageMbps float64 `json:"bandwidth_usage_mbps"`
	BandwidthLimitMbps int     `json:"bandwidth_limit_mbps"`
	CacheUsagePercent  float64 `json:"cache_usage_percent"`
	CacheMaxPercent    int     `json:"cache_max_percent"`
	CacheFreeBytes     int64   `json:"cache_free_bytes"`
	CacheTotalBytes    int64   `json:"cache_total_bytes"`
	ActiveSyncs        int     `json:"active_syncs"`
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

// JobRepository provides database access for jobs
type JobRepository interface {
	UpdateJob(job *models.Job) error
	GetJob(id int64) (*models.Job, error)
}

// RCloneClient provides an interface to interact with RClone daemon
type RCloneClient interface {
	Copy(ctx context.Context, srcFs, dstFs string, filter map[string]interface{}) (*models.RCloneCopyResponse, error)
	GetJobStatus(ctx context.Context, jobID int64) (*models.RCloneJobStatus, error)
	GetCoreStats(ctx context.Context) (*rclone.CoreStats, error)
	ListJobs(ctx context.Context) (*models.RCloneJobListResponse, error)
	StopJob(ctx context.Context, jobID int64) error
	Ping(ctx context.Context) error
}

// Notifier handles sending notifications for various events
type Notifier interface {
	IsEnabled() bool
	NotifyJobFailed(job *models.Job) error
	NotifyJobCompleted(job *models.Job) error
	NotifySyncFailed(syncJob *models.SyncJob) error
	NotifySyncCompleted(syncJob *models.SyncJob) error
	NotifySystemAlert(title, message string, priority int) error
}
