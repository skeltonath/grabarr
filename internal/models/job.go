package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

type Job struct {
	ID               int64           `json:"id" db:"id"`
	Name             string          `json:"name" db:"name"`
	RemotePath       string          `json:"remote_path" db:"remote_path"`
	LocalPath        string          `json:"local_path" db:"local_path"`
	Status           JobStatus       `json:"status" db:"status"`
	Priority         int             `json:"priority" db:"priority"`
	Retries          int             `json:"retries" db:"retries"`
	MaxRetries       int             `json:"max_retries" db:"max_retries"`
	ErrorMessage     string          `json:"error_message,omitempty" db:"error_message"`
	Progress         JobProgress     `json:"progress" db:"progress"`
	Metadata         JobMetadata     `json:"metadata" db:"metadata"`
	DownloadConfig   *DownloadConfig `json:"download_config,omitempty" db:"download_config"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
	StartedAt        *time.Time      `json:"started_at,omitempty" db:"started_at"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty" db:"completed_at"`
	FileSize         int64           `json:"file_size,omitempty" db:"file_size"`
	TransferredBytes int64           `json:"transferred_bytes" db:"transferred_bytes"`
	TransferSpeed    int64           `json:"transfer_speed,omitempty" db:"transfer_speed"`
}

type JobProgress struct {
	Percentage       float64    `json:"percentage"`
	TransferredBytes int64      `json:"transferred_bytes"`
	TotalBytes       int64      `json:"total_bytes"`
	TransferSpeed    int64      `json:"transfer_speed"`
	ETA              *time.Time `json:"eta,omitempty"`
	CurrentFile      string     `json:"current_file,omitempty"`
	FilesCompleted   int        `json:"files_completed"`
	FilesTotal       int        `json:"files_total"`
	LastUpdateTime   time.Time  `json:"last_update_time"`
}

type JobMetadata struct {
	QBittorrentHash string                 `json:"qbittorrent_hash,omitempty"`
	Category        string                 `json:"category,omitempty"`
	TorrentName     string                 `json:"torrent_name,omitempty"`
	Tags            []string               `json:"tags,omitempty"`
	SourceIP        string                 `json:"source_ip,omitempty"`
	UserAgent       string                 `json:"user_agent,omitempty"`
	RCloneArgs      []string               `json:"rclone_args,omitempty"`
	ExtraFields     map[string]interface{} `json:"extra_fields,omitempty"`
}

type JobAttempt struct {
	ID           int64      `json:"id" db:"id"`
	JobID        int64      `json:"job_id" db:"job_id"`
	AttemptNum   int        `json:"attempt_num" db:"attempt_num"`
	Status       JobStatus  `json:"status" db:"status"`
	ErrorMessage string     `json:"error_message,omitempty" db:"error_message"`
	StartedAt    time.Time  `json:"started_at" db:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty" db:"ended_at"`
	LogData      string     `json:"log_data,omitempty" db:"log_data"`
}

// Database value methods for custom types
func (jp JobProgress) Value() (driver.Value, error) {
	return json.Marshal(jp)
}

func (jp *JobProgress) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JobProgress", value)
	}

	return json.Unmarshal(bytes, jp)
}

func (jm JobMetadata) Value() (driver.Value, error) {
	return json.Marshal(jm)
}

func (jm *JobMetadata) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JobMetadata", value)
	}

	return json.Unmarshal(bytes, jm)
}

// Helper methods
func (j *Job) IsActive() bool {
	return j.Status == JobStatusRunning || j.Status == JobStatusPending
}

func (j *Job) IsCompleted() bool {
	return j.Status == JobStatusCompleted || j.Status == JobStatusFailed || j.Status == JobStatusCancelled
}

func (j *Job) CanRetry() bool {
	return j.Status == JobStatusFailed && j.Retries < j.MaxRetries
}

func (j *Job) UpdateProgress(progress JobProgress) {
	progress.LastUpdateTime = time.Now()
	j.Progress = progress
	j.TransferredBytes = progress.TransferredBytes
	j.TransferSpeed = progress.TransferSpeed
	j.UpdatedAt = time.Now()
}

func (j *Job) MarkStarted() {
	now := time.Now()
	j.Status = JobStatusRunning
	j.StartedAt = &now
	j.UpdatedAt = now
}

func (j *Job) MarkCompleted() {
	now := time.Now()
	j.Status = JobStatusCompleted
	j.CompletedAt = &now
	j.UpdatedAt = now
	j.Progress.Percentage = 100.0
}

func (j *Job) MarkFailed(errorMsg string) {
	now := time.Now()
	j.Status = JobStatusFailed
	j.ErrorMessage = errorMsg
	j.UpdatedAt = now
}

func (j *Job) MarkCancelled() {
	now := time.Now()
	j.Status = JobStatusCancelled
	j.UpdatedAt = now
}

func (j *Job) IncrementRetry() {
	j.Retries++
	j.Status = JobStatusQueued
	j.UpdatedAt = time.Now()
	j.ErrorMessage = ""
}

// JobFilter represents filtering options for job queries
type JobFilter struct {
	Status      []JobStatus `json:"status,omitempty"`
	Category    string      `json:"category,omitempty"`
	MinPriority *int        `json:"min_priority,omitempty"`
	MaxPriority *int        `json:"max_priority,omitempty"`
	Limit       int         `json:"limit,omitempty"`
	Offset      int         `json:"offset,omitempty"`
	SortBy      string      `json:"sort_by,omitempty"`
	SortOrder   string      `json:"sort_order,omitempty"`
}

// JobSummary represents aggregated job statistics
type JobSummary struct {
	TotalJobs     int `json:"total_jobs"`
	QueuedJobs    int `json:"queued_jobs"`
	PendingJobs   int `json:"pending_jobs"`
	RunningJobs   int `json:"running_jobs"`
	CompletedJobs int `json:"completed_jobs"`
	FailedJobs    int `json:"failed_jobs"`
	CancelledJobs int `json:"cancelled_jobs"`
}
