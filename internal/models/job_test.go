package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobProgress_MarshalUnmarshal(t *testing.T) {
	now := time.Now()
	eta := now.Add(1 * time.Hour)

	progress := JobProgress{
		Percentage:       45.5,
		TransferredBytes: 1024 * 1024,
		TotalBytes:       2048 * 1024,
		TransferSpeed:    1024,
		ETA:              &eta,
		CurrentFile:      "test.mkv",
		FilesCompleted:   5,
		FilesTotal:       10,
		LastUpdateTime:   now,
	}

	// Marshal
	data, err := json.Marshal(progress)
	require.NoError(t, err)

	// Unmarshal
	var decoded JobProgress
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, progress.Percentage, decoded.Percentage)
	assert.Equal(t, progress.TransferredBytes, decoded.TransferredBytes)
	assert.Equal(t, progress.TotalBytes, decoded.TotalBytes)
	assert.Equal(t, progress.TransferSpeed, decoded.TransferSpeed)
	assert.Equal(t, progress.CurrentFile, decoded.CurrentFile)
	assert.Equal(t, progress.FilesCompleted, decoded.FilesCompleted)
	assert.Equal(t, progress.FilesTotal, decoded.FilesTotal)
}

func TestJobMetadata_MarshalUnmarshal(t *testing.T) {
	metadata := JobMetadata{
		QBittorrentHash: "abc123",
		Category:        "movies",
		Tags:            []string{"tag1", "tag2"},
		SourceIP:        "192.168.1.1",
		UserAgent:       "test-agent",
		RCloneArgs:      []string{"--arg1", "--arg2"},
		ExtraFields: map[string]interface{}{
			"custom": "value",
			"number": float64(42),
		},
	}

	// Marshal
	data, err := json.Marshal(metadata)
	require.NoError(t, err)

	// Unmarshal
	var decoded JobMetadata
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, metadata.QBittorrentHash, decoded.QBittorrentHash)
	assert.Equal(t, metadata.Category, decoded.Category)
	assert.Equal(t, metadata.Tags, decoded.Tags)
	assert.Equal(t, metadata.SourceIP, decoded.SourceIP)
	assert.Equal(t, metadata.UserAgent, decoded.UserAgent)
	assert.Equal(t, metadata.RCloneArgs, decoded.RCloneArgs)
	assert.Equal(t, metadata.ExtraFields["custom"], decoded.ExtraFields["custom"])
}

func TestJobProgress_DatabaseScan(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{
			name:    "valid JSON bytes",
			input:   []byte(`{"percentage":50.0,"transferred_bytes":1024}`),
			wantErr: false,
		},
		{
			name:    "valid JSON string",
			input:   `{"percentage":50.0,"transferred_bytes":1024}`,
			wantErr: false,
		},
		{
			name:    "nil value",
			input:   nil,
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   123,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   []byte(`{invalid}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var progress JobProgress
			err := progress.Scan(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestJobMetadata_DatabaseScan(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{
			name:    "valid JSON bytes",
			input:   []byte(`{"category":"movies","tags":["tag1"]}`),
			wantErr: false,
		},
		{
			name:    "valid JSON string",
			input:   `{"category":"tv"}`,
			wantErr: false,
		},
		{
			name:    "nil value",
			input:   nil,
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   123,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var metadata JobMetadata
			err := metadata.Scan(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestJob_IsActive(t *testing.T) {
	tests := []struct {
		name   string
		status JobStatus
		want   bool
	}{
		{"running is active", JobStatusRunning, true},
		{"pending is active", JobStatusPending, true},
		{"queued is not active", JobStatusQueued, false},
		{"completed is not active", JobStatusCompleted, false},
		{"failed is not active", JobStatusFailed, false},
		{"cancelled is not active", JobStatusCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Status: tt.status}
			assert.Equal(t, tt.want, job.IsActive())
		})
	}
}

func TestJob_IsCompleted(t *testing.T) {
	tests := []struct {
		name   string
		status JobStatus
		want   bool
	}{
		{"completed is completed", JobStatusCompleted, true},
		{"failed is completed", JobStatusFailed, true},
		{"cancelled is completed", JobStatusCancelled, true},
		{"running is not completed", JobStatusRunning, false},
		{"pending is not completed", JobStatusPending, false},
		{"queued is not completed", JobStatusQueued, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Status: tt.status}
			assert.Equal(t, tt.want, job.IsCompleted())
		})
	}
}

func TestJob_CanRetry(t *testing.T) {
	tests := []struct {
		name       string
		status     JobStatus
		retries    int
		maxRetries int
		want       bool
	}{
		{"can retry when failed with retries remaining", JobStatusFailed, 1, 3, true},
		{"cannot retry when max retries reached", JobStatusFailed, 3, 3, false},
		{"cannot retry when not failed", JobStatusCompleted, 0, 3, false},
		{"can retry on first failure", JobStatusFailed, 0, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{
				Status:     tt.status,
				Retries:    tt.retries,
				MaxRetries: tt.maxRetries,
			}
			assert.Equal(t, tt.want, job.CanRetry())
		})
	}
}

func TestJob_UpdateProgress(t *testing.T) {
	job := &Job{}
	beforeUpdate := time.Now()

	progress := JobProgress{
		Percentage:       50.0,
		TransferredBytes: 1024,
		TotalBytes:       2048,
		TransferSpeed:    512,
	}

	job.UpdateProgress(progress)

	assert.Equal(t, progress.Percentage, job.Progress.Percentage)
	assert.Equal(t, progress.TransferredBytes, job.TransferredBytes)
	assert.Equal(t, progress.TransferSpeed, job.TransferSpeed)
	assert.True(t, job.Progress.LastUpdateTime.After(beforeUpdate) || job.Progress.LastUpdateTime.Equal(beforeUpdate))
	assert.True(t, job.UpdatedAt.After(beforeUpdate) || job.UpdatedAt.Equal(beforeUpdate))
}

func TestJob_MarkStarted(t *testing.T) {
	job := &Job{Status: JobStatusQueued}
	beforeMark := time.Now()

	job.MarkStarted()

	assert.Equal(t, JobStatusRunning, job.Status)
	assert.NotNil(t, job.StartedAt)
	assert.True(t, job.StartedAt.After(beforeMark) || job.StartedAt.Equal(beforeMark))
	assert.True(t, job.UpdatedAt.After(beforeMark) || job.UpdatedAt.Equal(beforeMark))
}

func TestJob_MarkCompleted(t *testing.T) {
	job := &Job{Status: JobStatusRunning}
	beforeMark := time.Now()

	job.MarkCompleted()

	assert.Equal(t, JobStatusCompleted, job.Status)
	assert.NotNil(t, job.CompletedAt)
	assert.Equal(t, 100.0, job.Progress.Percentage)
	assert.True(t, job.CompletedAt.After(beforeMark) || job.CompletedAt.Equal(beforeMark))
	assert.True(t, job.UpdatedAt.After(beforeMark) || job.UpdatedAt.Equal(beforeMark))
}

func TestJob_MarkFailed(t *testing.T) {
	job := &Job{Status: JobStatusRunning}
	beforeMark := time.Now()
	errorMsg := "test error"

	job.MarkFailed(errorMsg)

	assert.Equal(t, JobStatusFailed, job.Status)
	assert.Equal(t, errorMsg, job.ErrorMessage)
	assert.True(t, job.UpdatedAt.After(beforeMark) || job.UpdatedAt.Equal(beforeMark))
}

func TestJob_MarkCancelled(t *testing.T) {
	job := &Job{Status: JobStatusRunning}
	beforeMark := time.Now()

	job.MarkCancelled()

	assert.Equal(t, JobStatusCancelled, job.Status)
	assert.True(t, job.UpdatedAt.After(beforeMark) || job.UpdatedAt.Equal(beforeMark))
}

func TestJob_IncrementRetry(t *testing.T) {
	job := &Job{
		Status:       JobStatusFailed,
		Retries:      2,
		ErrorMessage: "previous error",
	}
	beforeIncrement := time.Now()

	job.IncrementRetry()

	assert.Equal(t, 3, job.Retries)
	assert.Equal(t, JobStatusQueued, job.Status)
	assert.Empty(t, job.ErrorMessage)
	assert.True(t, job.UpdatedAt.After(beforeIncrement) || job.UpdatedAt.Equal(beforeIncrement))
}
