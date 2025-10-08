package rclone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"grabarr/internal/models"
	"io"
	"net/http"
	"time"
)

// Client represents an HTTP client for the rclone daemon
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new rclone HTTP client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second, // Short timeout since we use async operations
		},
	}
}

// SyncCopyRequest represents a request to copy files/directories using sync/copy
type SyncCopyRequest struct {
	SrcFs  string                 `json:"srcFs"`
	DstFs  string                 `json:"dstFs"`
	Filter map[string]interface{} `json:"_filter,omitempty"`
	Async  bool                   `json:"_async,omitempty"`
	Config map[string]interface{} `json:"_config,omitempty"`
}

// CopyResponse represents the response from a copy operation
type CopyResponse struct {
	JobID int64 `json:"jobid"`
}

// JobStatus represents the status of a running job
type JobStatus struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Group     string    `json:"group"`
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
	Error     string    `json:"error"`
	Finished  bool      `json:"finished"`
	Success   bool      `json:"success"`
	Duration  float64   `json:"duration"`
	Progress  string    `json:"progress"`
	Output    Output    `json:"output"`
}

// Output represents the transfer statistics from rclone
type Output struct {
	Bytes               int64   `json:"bytes"`
	Checks              int64   `json:"checks"`
	Deletes             int64   `json:"deletes"`
	ElapsedTime         float64 `json:"elapsedTime"`
	Errors              int64   `json:"errors"`
	ETA                 *int64  `json:"eta"`
	FatalError          bool    `json:"fatalError"`
	Renames             int64   `json:"renames"`
	RetryError          bool    `json:"retryError"`
	ServerSideCopies    int64   `json:"serverSideCopies"`
	ServerSideCopyBytes int64   `json:"serverSideCopyBytes"`
	ServerSideMoveBytes int64   `json:"serverSideMoveBytes"`
	ServerSideMoves     int64   `json:"serverSideMoves"`
	Speed               float64 `json:"speed"`
	TotalBytes          int64   `json:"totalBytes"`
	TotalChecks         int64   `json:"totalChecks"`
	TotalTransfers      int64   `json:"totalTransfers"`
	TransferTime        float64 `json:"transferTime"`
	Transfers           int64   `json:"transfers"`
}

// JobListResponse represents the response from job/list
type JobListResponse struct {
	JobIDs []int64 `json:"jobids"`
}

// CoreStats represents global transfer statistics from /core/stats
type CoreStats struct {
	Bytes               int64              `json:"bytes"`
	Checks              int64              `json:"checks"`
	DeletedDirs         int64              `json:"deletedDirs"`
	Deletes             int64              `json:"deletes"`
	ElapsedTime         float64            `json:"elapsedTime"`
	Errors              int64              `json:"errors"`
	ETA                 *int64             `json:"eta"`
	FatalError          bool               `json:"fatalError"`
	LastError           string             `json:"lastError"`
	Renames             int64              `json:"renames"`
	RetryError          bool               `json:"retryError"`
	ServerSideCopies    int64              `json:"serverSideCopies"`
	ServerSideCopyBytes int64              `json:"serverSideCopyBytes"`
	ServerSideMoveBytes int64              `json:"serverSideMoveBytes"`
	ServerSideMoves     int64              `json:"serverSideMoves"`
	Speed               float64            `json:"speed"`
	TotalBytes          int64              `json:"totalBytes"`
	TotalChecks         int64              `json:"totalChecks"`
	TotalTransfers      int64              `json:"totalTransfers"`
	TransferTime        float64            `json:"transferTime"`
	Transfers           int64              `json:"transfers"`
	Transferring        []TransferringFile `json:"transferring"`
}

// TransferringFile represents a file currently being transferred
type TransferringFile struct {
	Bytes      int64   `json:"bytes"`
	DstFs      string  `json:"dstFs"`
	ETA        *int64  `json:"eta"`
	Group      string  `json:"group"`
	Name       string  `json:"name"`
	Percentage int     `json:"percentage"`
	Size       int64   `json:"size"`
	Speed      float64 `json:"speed"`
	SpeedAvg   float64 `json:"speedAvg"`
	SrcFs      string  `json:"srcFs"`
}

// Copy initiates a copy operation for files or directories with optional filtering
func (c *Client) Copy(ctx context.Context, srcFs, dstFs string, filter map[string]interface{}) (*models.RCloneCopyResponse, error) {
	req := SyncCopyRequest{
		SrcFs:  srcFs,
		DstFs:  dstFs,
		Filter: filter,
		Async:  true, // Always use async to avoid timeouts on large transfers
		Config: map[string]interface{}{
			// Limit concurrency
			"Transfers":          2,       // Two files at once
			"Checkers":           2,       // Two parallel stat operations

			// Bandwidth control
			"BwLimit":            "8M",   // Overall cap (adjust if safe)
			"BwLimitFile":        "4M",   // Per-file cap to smooth bursts

			// Disk I/O tuning
			"BufferSize":         "32M",    // Smallish read buffer
			"UseMmap":            true,    // Efficient reads from remote
			"MultiThreadStreams": 1,       // Disable multi-threaded reads
			"MultiThreadCutoff":  "10G",   // Effectively disables it

			// Behavior
			"IgnoreExisting":     true,    // Skip anything already present
			"NoTraverse":         true,    // Don't scan full dest tree
			"UpdateOlder":        true,    // Only replace older files (safe add-only)
		},
	}

	var resp CopyResponse
	err := c.makeRequest(ctx, "POST", "/sync/copy", req, &resp)
	return &models.RCloneCopyResponse{JobID: resp.JobID}, err
}


// GetJobStatus gets the status of a specific job
func (c *Client) GetJobStatus(ctx context.Context, jobID int64) (*models.RCloneJobStatus, error) {
	var status JobStatus
	endpoint := fmt.Sprintf("/job/status?jobid=%d", jobID)
	err := c.makeRequest(ctx, "POST", endpoint, nil, &status)
	if err != nil {
		return nil, err
	}

	// Convert to model type
	return &models.RCloneJobStatus{
		ID:       status.ID,
		Name:     status.Name,
		Group:    status.Group,
		Error:    status.Error,
		Finished: status.Finished,
		Success:  status.Success,
		Duration: status.Duration,
		Progress: status.Progress,
		Output: models.RCloneOutput{
			Bytes:          status.Output.Bytes,
			Speed:          status.Output.Speed,
			TotalBytes:     status.Output.TotalBytes,
			TotalTransfers: status.Output.TotalTransfers,
			Transfers:      status.Output.Transfers,
			Errors:         status.Output.Errors,
		},
	}, nil
}

// ListJobs lists all active jobs
func (c *Client) ListJobs(ctx context.Context) (*models.RCloneJobListResponse, error) {
	var resp JobListResponse
	err := c.makeRequest(ctx, "POST", "/job/list", nil, &resp)
	return &models.RCloneJobListResponse{JobIDs: resp.JobIDs}, err
}

// StopJob stops a running job
func (c *Client) StopJob(ctx context.Context, jobID int64) error {
	endpoint := fmt.Sprintf("/job/stop?jobid=%d", jobID)
	return c.makeRequest(ctx, "POST", endpoint, nil, nil)
}

// Ping checks if the rclone daemon is responsive
func (c *Client) Ping(ctx context.Context) error {
	return c.makeRequest(ctx, "POST", "/core/pid", nil, nil)
}

// GetCoreStats gets global transfer statistics from /core/stats
func (c *Client) GetCoreStats(ctx context.Context) (*CoreStats, error) {
	var stats CoreStats
	err := c.makeRequest(ctx, "POST", "/core/stats", nil, &stats)
	if err != nil {
		return nil, err
	}
	return &stats, nil
}

// makeRequest makes an HTTP request to the rclone daemon
func (c *Client) makeRequest(ctx context.Context, method, endpoint string, request interface{}, response interface{}) error {
	var body io.Reader
	if request != nil {
		jsonData, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if request != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if response != nil {
		if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
