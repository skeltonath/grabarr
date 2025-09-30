package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncProgress_MarshalUnmarshal(t *testing.T) {
	now := time.Now()
	eta := now.Add(30 * time.Minute)

	progress := SyncProgress{
		Percentage:       75.5,
		TransferredBytes: 1024 * 1024 * 500,
		TotalBytes:       1024 * 1024 * 1000,
		TransferSpeed:    1024 * 1024,
		ETA:              &eta,
		CurrentFile:      "movie.mkv",
		FilesCompleted:   50,
		FilesTotal:       100,
		LastUpdateTime:   now,
	}

	// Marshal
	data, err := json.Marshal(progress)
	require.NoError(t, err)

	// Unmarshal
	var decoded SyncProgress
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

func TestSyncStats_MarshalUnmarshal(t *testing.T) {
	stats := SyncStats{
		FilesTransferred: 100,
		FilesSkipped:     50,
		FilesErrored:     2,
		BytesTransferred: 1024 * 1024 * 1024,
		TotalFiles:       152,
		TotalBytes:       2048 * 1024 * 1024,
	}

	// Marshal
	data, err := json.Marshal(stats)
	require.NoError(t, err)

	// Unmarshal
	var decoded SyncStats
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, stats.FilesTransferred, decoded.FilesTransferred)
	assert.Equal(t, stats.FilesSkipped, decoded.FilesSkipped)
	assert.Equal(t, stats.FilesErrored, decoded.FilesErrored)
	assert.Equal(t, stats.BytesTransferred, decoded.BytesTransferred)
	assert.Equal(t, stats.TotalFiles, decoded.TotalFiles)
	assert.Equal(t, stats.TotalBytes, decoded.TotalBytes)
}

func TestSyncProgress_DatabaseScan(t *testing.T) {
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
			input:   `{"percentage":75.0}`,
			wantErr: false,
		},
		{
			name:    "nil value",
			input:   nil,
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   12345,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   []byte(`{invalid json}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var progress SyncProgress
			err := progress.Scan(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSyncStats_DatabaseScan(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{
			name:    "valid JSON bytes",
			input:   []byte(`{"files_transferred":10,"files_skipped":5}`),
			wantErr: false,
		},
		{
			name:    "valid JSON string",
			input:   `{"total_files":100}`,
			wantErr: false,
		},
		{
			name:    "nil value",
			input:   nil,
			wantErr: false,
		},
		{
			name:    "invalid type",
			input:   true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stats SyncStats
			err := stats.Scan(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSyncJob_IsActive(t *testing.T) {
	tests := []struct {
		name   string
		status SyncStatus
		want   bool
	}{
		{"running is active", SyncStatusRunning, true},
		{"queued is not active", SyncStatusQueued, false},
		{"completed is not active", SyncStatusCompleted, false},
		{"failed is not active", SyncStatusFailed, false},
		{"cancelled is not active", SyncStatusCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncJob := &SyncJob{Status: tt.status}
			assert.Equal(t, tt.want, syncJob.IsActive())
		})
	}
}

func TestSyncJob_IsCompleted(t *testing.T) {
	tests := []struct {
		name   string
		status SyncStatus
		want   bool
	}{
		{"completed is completed", SyncStatusCompleted, true},
		{"failed is completed", SyncStatusFailed, true},
		{"cancelled is completed", SyncStatusCancelled, true},
		{"running is not completed", SyncStatusRunning, false},
		{"queued is not completed", SyncStatusQueued, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncJob := &SyncJob{Status: tt.status}
			assert.Equal(t, tt.want, syncJob.IsCompleted())
		})
	}
}

func TestSyncJob_UpdateProgress(t *testing.T) {
	syncJob := &SyncJob{}
	beforeUpdate := time.Now()

	progress := SyncProgress{
		Percentage:       50.0,
		TransferredBytes: 1024 * 1024,
		TotalBytes:       2048 * 1024,
		TransferSpeed:    512 * 1024,
		FilesCompleted:   10,
		FilesTotal:       20,
	}

	syncJob.UpdateProgress(progress)

	assert.Equal(t, progress.Percentage, syncJob.Progress.Percentage)
	assert.Equal(t, progress.TransferredBytes, syncJob.Progress.TransferredBytes)
	assert.Equal(t, progress.TotalBytes, syncJob.Progress.TotalBytes)
	assert.Equal(t, progress.TransferSpeed, syncJob.Progress.TransferSpeed)
	assert.True(t, syncJob.Progress.LastUpdateTime.After(beforeUpdate) || syncJob.Progress.LastUpdateTime.Equal(beforeUpdate))
	assert.True(t, syncJob.UpdatedAt.After(beforeUpdate) || syncJob.UpdatedAt.Equal(beforeUpdate))
}

func TestSyncJob_MarkStarted(t *testing.T) {
	syncJob := &SyncJob{Status: SyncStatusQueued}
	beforeMark := time.Now()
	rcloneJobID := int64(12345)

	syncJob.MarkStarted(rcloneJobID)

	assert.Equal(t, SyncStatusRunning, syncJob.Status)
	assert.NotNil(t, syncJob.StartedAt)
	assert.NotNil(t, syncJob.RCloneJobID)
	assert.Equal(t, rcloneJobID, *syncJob.RCloneJobID)
	assert.True(t, syncJob.StartedAt.After(beforeMark) || syncJob.StartedAt.Equal(beforeMark))
	assert.True(t, syncJob.UpdatedAt.After(beforeMark) || syncJob.UpdatedAt.Equal(beforeMark))
}

func TestSyncJob_MarkCompleted(t *testing.T) {
	syncJob := &SyncJob{Status: SyncStatusRunning}
	beforeMark := time.Now()

	stats := SyncStats{
		FilesTransferred: 100,
		FilesSkipped:     10,
		FilesErrored:     1,
		BytesTransferred: 1024 * 1024 * 500,
		TotalFiles:       111,
		TotalBytes:       1024 * 1024 * 550,
	}

	syncJob.MarkCompleted(stats)

	assert.Equal(t, SyncStatusCompleted, syncJob.Status)
	assert.NotNil(t, syncJob.CompletedAt)
	assert.Equal(t, 100.0, syncJob.Progress.Percentage)
	assert.Equal(t, stats, syncJob.Stats)
	assert.True(t, syncJob.CompletedAt.After(beforeMark) || syncJob.CompletedAt.Equal(beforeMark))
	assert.True(t, syncJob.UpdatedAt.After(beforeMark) || syncJob.UpdatedAt.Equal(beforeMark))
}

func TestSyncJob_MarkFailed(t *testing.T) {
	syncJob := &SyncJob{Status: SyncStatusRunning}
	beforeMark := time.Now()
	errorMsg := "sync operation failed"

	syncJob.MarkFailed(errorMsg)

	assert.Equal(t, SyncStatusFailed, syncJob.Status)
	assert.Equal(t, errorMsg, syncJob.ErrorMessage)
	assert.True(t, syncJob.UpdatedAt.After(beforeMark) || syncJob.UpdatedAt.Equal(beforeMark))
}

func TestSyncJob_MarkCancelled(t *testing.T) {
	syncJob := &SyncJob{Status: SyncStatusRunning}
	beforeMark := time.Now()

	syncJob.MarkCancelled()

	assert.Equal(t, SyncStatusCancelled, syncJob.Status)
	assert.True(t, syncJob.UpdatedAt.After(beforeMark) || syncJob.UpdatedAt.Equal(beforeMark))
}