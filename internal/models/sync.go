package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

type SyncStatus string

const (
	SyncStatusQueued    SyncStatus = "queued"
	SyncStatusRunning   SyncStatus = "running"
	SyncStatusCompleted SyncStatus = "completed"
	SyncStatusFailed    SyncStatus = "failed"
	SyncStatusCancelled SyncStatus = "cancelled"
)

type SyncJob struct {
	ID             int64           `json:"id" db:"id"`
	RemotePath     string          `json:"remote_path" db:"remote_path"`
	LocalPath      string          `json:"local_path" db:"local_path"`
	Status         SyncStatus      `json:"status" db:"status"`
	ErrorMessage   string          `json:"error_message,omitempty" db:"error_message"`
	Progress       SyncProgress    `json:"progress" db:"progress"`
	Stats          SyncStats       `json:"stats" db:"stats"`
	DownloadConfig *DownloadConfig `json:"download_config,omitempty" db:"download_config"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" db:"updated_at"`
	StartedAt      *time.Time      `json:"started_at,omitempty" db:"started_at"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty" db:"completed_at"`
	RCloneJobID    *int64          `json:"rclone_job_id,omitempty" db:"rclone_job_id"`
}

type SyncProgress struct {
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

type SyncStats struct {
	FilesTransferred int   `json:"files_transferred"`
	FilesSkipped     int   `json:"files_skipped"`
	FilesErrored     int   `json:"files_errored"`
	BytesTransferred int64 `json:"bytes_transferred"`
	TotalFiles       int   `json:"total_files"`
	TotalBytes       int64 `json:"total_bytes"`
}

// Database value methods for custom types
func (sp SyncProgress) Value() (driver.Value, error) {
	return json.Marshal(sp)
}

func (sp *SyncProgress) Scan(value interface{}) error {
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
		return fmt.Errorf("cannot scan %T into SyncProgress", value)
	}

	return json.Unmarshal(bytes, sp)
}

func (ss SyncStats) Value() (driver.Value, error) {
	return json.Marshal(ss)
}

func (ss *SyncStats) Scan(value interface{}) error {
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
		return fmt.Errorf("cannot scan %T into SyncStats", value)
	}

	return json.Unmarshal(bytes, ss)
}

// Helper methods
func (s *SyncJob) IsActive() bool {
	return s.Status == SyncStatusRunning
}

func (s *SyncJob) IsCompleted() bool {
	return s.Status == SyncStatusCompleted || s.Status == SyncStatusFailed || s.Status == SyncStatusCancelled
}

func (s *SyncJob) UpdateProgress(progress SyncProgress) {
	progress.LastUpdateTime = time.Now()
	s.Progress = progress
	s.UpdatedAt = time.Now()
}

func (s *SyncJob) MarkStarted(rcloneJobID int64) {
	now := time.Now()
	s.Status = SyncStatusRunning
	s.StartedAt = &now
	s.UpdatedAt = now
	s.RCloneJobID = &rcloneJobID
}

func (s *SyncJob) MarkCompleted(stats SyncStats) {
	now := time.Now()
	s.Status = SyncStatusCompleted
	s.CompletedAt = &now
	s.UpdatedAt = now
	s.Progress.Percentage = 100.0
	s.Stats = stats
}

func (s *SyncJob) MarkFailed(errorMsg string) {
	now := time.Now()
	s.Status = SyncStatusFailed
	s.ErrorMessage = errorMsg
	s.UpdatedAt = now
}

func (s *SyncJob) MarkCancelled() {
	now := time.Now()
	s.Status = SyncStatusCancelled
	s.UpdatedAt = now
}

// SyncFilter represents filtering options for sync job queries
type SyncFilter struct {
	Status    []SyncStatus `json:"status,omitempty"`
	Limit     int          `json:"limit,omitempty"`
	Offset    int          `json:"offset,omitempty"`
	SortBy    string       `json:"sort_by,omitempty"`
	SortOrder string       `json:"sort_order,omitempty"`
}

// SyncSummary represents aggregated sync job statistics
type SyncSummary struct {
	TotalSyncs     int `json:"total_syncs"`
	QueuedSyncs    int `json:"queued_syncs"`
	RunningSyncs   int `json:"running_syncs"`
	CompletedSyncs int `json:"completed_syncs"`
	FailedSyncs    int `json:"failed_syncs"`
	CancelledSyncs int `json:"cancelled_syncs"`
}
