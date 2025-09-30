package models

// RCloneCopyResponse represents the response from a copy operation
type RCloneCopyResponse struct {
	JobID int64
}

// RCloneJobStatus represents the status of a running job
type RCloneJobStatus struct {
	ID       int64
	Name     string
	Group    string
	Error    string
	Finished bool
	Success  bool
	Duration float64
	Progress string
	Output   RCloneOutput
}

// RCloneOutput represents the transfer statistics from rclone
type RCloneOutput struct {
	Bytes          int64
	Speed          float64
	TotalBytes     int64
	TotalTransfers int64
	Transfers      int64
	Errors         int64
}

// RCloneJobListResponse represents the response from job/list
type RCloneJobListResponse struct {
	JobIDs []int64
}
