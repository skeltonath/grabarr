package models

import "time"

type FileStatus string

const (
	FileStatusOnSeedbox   FileStatus = "on_seedbox"
	FileStatusQueued      FileStatus = "queued"
	FileStatusDownloading FileStatus = "downloading"
	FileStatusDownloaded  FileStatus = "downloaded"
	FileStatusIgnored     FileStatus = "ignored"
)

type RemoteFile struct {
	ID          int64      `json:"id"`
	RemotePath  string     `json:"remote_path"` // full path on seedbox
	Name        string     `json:"name"`
	Size        int64      `json:"size"`
	Extension   string     `json:"extension"`
	Status      FileStatus `json:"status"`
	JobID       *int64     `json:"job_id,omitempty"` // linked job if queued/downloading/downloaded
	WatchedPath string     `json:"watched_path"`     // which watched path config this came from
	FirstSeenAt time.Time  `json:"first_seen_at"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// RemoteFileFilter is used to filter remote file queries
type RemoteFileFilter struct {
	Status      FileStatus
	WatchedPath string
	Extension   string
	Limit       int
	Offset      int
}
