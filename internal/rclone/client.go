package rclone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	Bytes             int64   `json:"bytes"`
	Checks            int64   `json:"checks"`
	Deletes           int64   `json:"deletes"`
	ElapsedTime       float64 `json:"elapsedTime"`
	Errors            int64   `json:"errors"`
	ETA               *int64  `json:"eta"`
	FatalError        bool    `json:"fatalError"`
	Renames           int64   `json:"renames"`
	RetryError        bool    `json:"retryError"`
	ServerSideCopies  int64   `json:"serverSideCopies"`
	ServerSideCopyBytes int64 `json:"serverSideCopyBytes"`
	ServerSideMoveBytes int64 `json:"serverSideMoveBytes"`
	ServerSideMoves   int64   `json:"serverSideMoves"`
	Speed             float64 `json:"speed"`
	TotalBytes        int64   `json:"totalBytes"`
	TotalChecks       int64   `json:"totalChecks"`
	TotalTransfers    int64   `json:"totalTransfers"`
	TransferTime      float64 `json:"transferTime"`
	Transfers         int64   `json:"transfers"`
}

// JobListResponse represents the response from job/list
type JobListResponse struct {
	JobIDs []int64 `json:"jobids"`
}


// Copy initiates a copy operation for files or directories with optional filtering
func (c *Client) Copy(ctx context.Context, srcFs, dstFs string, filter map[string]interface{}) (*CopyResponse, error) {
	req := SyncCopyRequest{
		SrcFs:  srcFs,
		DstFs:  dstFs,
		Filter: filter,
		Async:  true, // Always use async to avoid timeouts on large transfers
	}

	var resp CopyResponse
	err := c.makeRequest(ctx, "POST", "/sync/copy", req, &resp)
	return &resp, err
}


// GetJobStatus gets the status of a specific job
func (c *Client) GetJobStatus(ctx context.Context, jobID int64) (*JobStatus, error) {
	var status JobStatus
	endpoint := fmt.Sprintf("/job/status?jobid=%d", jobID)
	err := c.makeRequest(ctx, "POST", endpoint, nil, &status)
	return &status, err
}

// ListJobs lists all active jobs
func (c *Client) ListJobs(ctx context.Context) (*JobListResponse, error) {
	var resp JobListResponse
	err := c.makeRequest(ctx, "POST", "/job/list", nil, &resp)
	return &resp, err
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