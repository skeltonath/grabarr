package executor

import (
	"context"
	"errors"
	"testing"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
	"grabarr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewRCloneExecutor(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			DaemonAddr: "localhost:5572",
		},
	}
	mockMonitor := mocks.NewMockGatekeeper(t)
	mockRepo := mocks.NewMockJobRepository(t)

	executor := NewRCloneExecutor(cfg, mockMonitor, mockRepo)

	assert.NotNil(t, executor)
	assert.Equal(t, cfg, executor.config)
	assert.Equal(t, mockMonitor, executor.gatekeeper)
	assert.NotNil(t, executor.client)
	assert.NotNil(t, executor.progressChan)
	assert.Equal(t, mockRepo, executor.repo)
}

func TestExecute_Success(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			RemoteName: "seedbox",
		},
	}
	mockMonitor := mocks.NewMockGatekeeper(t)
	mockClient := mocks.NewMockRCloneClient(t)
	mockRepo := mocks.NewMockJobRepository(t)

	progressMonitor := NewProgressMonitor(mockClient, mockRepo)
	executor := &RCloneExecutor{
		config:          cfg,
		gatekeeper:      mockMonitor,
		progressChan:    make(chan models.JobProgress, 100),
		client:          mockClient,
		repo:            mockRepo,
		progressMonitor: progressMonitor,
	}

	job := &models.Job{
		ID:         1,
		Name:       "test-job",
		RemotePath: "/downloads/test-file.mkv",
		LocalPath:  "/local/downloads",
		Status:     models.JobStatusQueued,
	}

	ctx := context.Background()

	// Mock Ping success
	mockClient.EXPECT().
		Ping(ctx).
		Return(nil).
		Once()

	// Mock Copy success
	mockClient.EXPECT().
		Copy(ctx, "seedbox:/downloads/", "/local/downloads/", mock.Anything).
		Return(&models.RCloneCopyResponse{JobID: 123}, nil).
		Once()

	// Mock GetJobStatus - return finished successfully
	mockClient.EXPECT().
		GetJobStatus(ctx, int64(123)).
		Return(&models.RCloneJobStatus{
			ID:       123,
			Finished: true,
			Success:  true,
			Output: models.RCloneOutput{
				Bytes:          1024 * 1024 * 100,
				TotalBytes:     1024 * 1024 * 100,
				Transfers:      1,
				TotalTransfers: 1,
				Speed:          1024 * 1024,
			},
		}, nil).
		Once()

	// Mock UpdateJob for final persist
	mockRepo.EXPECT().
		UpdateJob(mock.Anything).
		Return(nil).
		Once()

	err := executor.Execute(ctx, job)

	assert.NoError(t, err)
}

func TestExecute_DaemonNotResponsive(t *testing.T) {
	cfg := &config.Config{}
	mockMonitor := mocks.NewMockGatekeeper(t)
	mockClient := mocks.NewMockRCloneClient(t)
	mockRepo := mocks.NewMockJobRepository(t)

	progressMonitor := NewProgressMonitor(mockClient, mockRepo)
	executor := &RCloneExecutor{
		config:          cfg,
		gatekeeper:      mockMonitor,
		progressChan:    make(chan models.JobProgress, 100),
		client:          mockClient,
		repo:            mockRepo,
		progressMonitor: progressMonitor,
	}

	job := &models.Job{
		ID:         1,
		RemotePath: "/test",
		LocalPath:  "/local",
	}

	ctx := context.Background()

	// Mock Ping failure
	mockClient.EXPECT().
		Ping(ctx).
		Return(errors.New("connection refused")).
		Once()

	err := executor.Execute(ctx, job)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rclone daemon not responsive")
}

func TestExecute_CopyFails(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			RemoteName: "seedbox",
		},
	}
	mockMonitor := mocks.NewMockGatekeeper(t)
	mockClient := mocks.NewMockRCloneClient(t)
	mockRepo := mocks.NewMockJobRepository(t)

	progressMonitor := NewProgressMonitor(mockClient, mockRepo)
	executor := &RCloneExecutor{
		config:          cfg,
		gatekeeper:      mockMonitor,
		progressChan:    make(chan models.JobProgress, 100),
		client:          mockClient,
		repo:            mockRepo,
		progressMonitor: progressMonitor,
	}

	job := &models.Job{
		ID:         1,
		RemotePath: "/downloads/test.mkv",
		LocalPath:  "/local",
	}

	ctx := context.Background()

	mockClient.EXPECT().
		Ping(ctx).
		Return(nil).
		Once()

	mockClient.EXPECT().
		Copy(ctx, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("copy failed")).
		Once()

	err := executor.Execute(ctx, job)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start copy operation")
}

func TestPrepareCopyRequest_File(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			RemoteName: "seedbox",
		},
	}
	mockMonitor := mocks.NewMockGatekeeper(t)

	executor := &RCloneExecutor{
		config:     cfg,
		gatekeeper: mockMonitor,
	}

	job := &models.Job{
		RemotePath: "/downloads/movie.mkv",
		LocalPath:  "/local/downloads",
	}

	srcFs, dstFs, filter := executor.prepareCopyRequest(job)

	assert.Equal(t, "seedbox:/downloads/", srcFs)
	assert.Equal(t, "/local/downloads/", dstFs)
	assert.NotNil(t, filter)

	includeRules, ok := filter["IncludeRule"].([]string)
	require.True(t, ok)
	assert.Contains(t, includeRules, "movie.mkv")
	assert.Contains(t, includeRules, "movie.mkv/**")
}

func TestPrepareCopyRequest_Directory(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			RemoteName: "remote",
		},
	}
	mockMonitor := mocks.NewMockGatekeeper(t)

	executor := &RCloneExecutor{
		config:     cfg,
		gatekeeper: mockMonitor,
	}

	job := &models.Job{
		RemotePath: "/tv/show-season-01",
		LocalPath:  "/local/tv/",
	}

	srcFs, dstFs, filter := executor.prepareCopyRequest(job)

	assert.Equal(t, "remote:/tv/", srcFs)
	assert.Equal(t, "/local/tv/", dstFs)

	includeRules, ok := filter["IncludeRule"].([]string)
	require.True(t, ok)
	assert.Contains(t, includeRules, "show-season-01")
}

func TestPrepareCopyRequest_NestedPath(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			RemoteName: "seedbox",
		},
	}
	mockMonitor := mocks.NewMockGatekeeper(t)

	executor := &RCloneExecutor{
		config:     cfg,
		gatekeeper: mockMonitor,
	}

	job := &models.Job{
		RemotePath: "/media/movies/2024/movie.mkv",
		LocalPath:  "/local",
	}

	srcFs, dstFs, filter := executor.prepareCopyRequest(job)

	assert.Equal(t, "seedbox:/media/movies/2024/", srcFs)
	assert.Equal(t, "/local/", dstFs)

	includeRules, ok := filter["IncludeRule"].([]string)
	require.True(t, ok)
	assert.Contains(t, includeRules, "movie.mkv")
	assert.Contains(t, includeRules, "movie.mkv/**")
}

func TestMonitorJob_Success(t *testing.T) {
	mockClient := mocks.NewMockRCloneClient(t)
	mockRepo := mocks.NewMockJobRepository(t)

	executor := &RCloneExecutor{
		client:       mockClient,
		progressChan: make(chan models.JobProgress, 100),
		repo:         mockRepo,
	}

	job := &models.Job{
		ID: 1,
	}

	ctx := context.Background()
	rcloneJobID := int64(123)

	mockClient.EXPECT().
		GetJobStatus(ctx, rcloneJobID).
		Return(&models.RCloneJobStatus{
			ID:       rcloneJobID,
			Finished: true,
			Success:  true,
			Output: models.RCloneOutput{
				Bytes:      1000,
				TotalBytes: 1000,
			},
		}, nil).
		Once()

	// Expect UpdateJob for final persist
	mockRepo.EXPECT().
		UpdateJob(mock.Anything).
		Return(nil).
		Once()

	err := executor.monitorJob(ctx, job, rcloneJobID)

	assert.NoError(t, err)
}

func TestMonitorJob_Failure(t *testing.T) {
	mockClient := mocks.NewMockRCloneClient(t)
	mockRepo := mocks.NewMockJobRepository(t)

	executor := &RCloneExecutor{
		client:       mockClient,
		progressChan: make(chan models.JobProgress, 100),
		repo:         mockRepo,
	}

	job := &models.Job{
		ID: 1,
	}

	ctx := context.Background()
	rcloneJobID := int64(123)

	mockClient.EXPECT().
		GetJobStatus(ctx, rcloneJobID).
		Return(&models.RCloneJobStatus{
			ID:       rcloneJobID,
			Finished: true,
			Success:  false,
			Error:    "transfer failed",
		}, nil).
		Once()

	// Expect UpdateJob for final persist even on failure
	mockRepo.EXPECT().
		UpdateJob(mock.Anything).
		Return(nil).
		Once()

	err := executor.monitorJob(ctx, job, rcloneJobID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rclone job failed")
	assert.Contains(t, err.Error(), "transfer failed")
}

func TestMonitorJob_ContextCancelled(t *testing.T) {
	mockClient := mocks.NewMockRCloneClient(t)
	mockRepo := mocks.NewMockJobRepository(t)

	executor := &RCloneExecutor{
		client:       mockClient,
		progressChan: make(chan models.JobProgress, 100),
		repo:         mockRepo,
	}

	job := &models.Job{
		ID: 1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	rcloneJobID := int64(123)

	// Should call StopJob when context is cancelled
	mockClient.EXPECT().
		StopJob(mock.Anything, rcloneJobID).
		Return(nil).
		Once()

	err := executor.monitorJob(ctx, job, rcloneJobID)

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestMonitorJob_StatusCheckError(t *testing.T) {
	mockClient := mocks.NewMockRCloneClient(t)
	mockRepo := mocks.NewMockJobRepository(t)

	executor := &RCloneExecutor{
		client:       mockClient,
		progressChan: make(chan models.JobProgress, 100),
		repo:         mockRepo,
	}

	job := &models.Job{
		ID: 1,
	}

	ctx := context.Background()
	rcloneJobID := int64(123)

	// First call fails with temporary error, second call succeeds
	mockClient.EXPECT().
		GetJobStatus(ctx, rcloneJobID).
		Return(nil, errors.New("temporary error")).
		Once()

	mockClient.EXPECT().
		GetJobStatus(ctx, rcloneJobID).
		Return(&models.RCloneJobStatus{
			ID:       rcloneJobID,
			Finished: true,
			Success:  true,
		}, nil).
		Once()

	// Expect UpdateJob calls - one periodic, one final
	mockRepo.EXPECT().
		UpdateJob(mock.Anything).
		Return(nil).
		Maybe() // Allow multiple calls during monitoring

	err := executor.monitorJob(ctx, job, rcloneJobID)

	assert.NoError(t, err)
}

func TestGetProgressChannel(t *testing.T) {
	progressChan := make(chan models.JobProgress, 100)

	executor := &RCloneExecutor{
		progressChan: progressChan,
	}

	result := executor.GetProgressChannel()

	// result is read-only channel, progressChan is bidirectional
	// Just verify they're connected
	assert.NotNil(t, result)

	// Test that we can receive from result when we send to progressChan
	testProgress := models.JobProgress{
		TransferredBytes: 1234,
	}
	progressChan <- testProgress
	received := <-result
	assert.Equal(t, testProgress.TransferredBytes, received.TransferredBytes)
}

func TestEscapeGlobChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special chars",
			input:    "normal-file.mkv",
			expected: "normal-file.mkv",
		},
		{
			name:     "square brackets",
			input:    "file[TGx].mkv",
			expected: "file\\[TGx\\].mkv",
		},
		{
			name:     "asterisk",
			input:    "file*.mkv",
			expected: "file\\*.mkv",
		},
		{
			name:     "question mark",
			input:    "file?.mkv",
			expected: "file\\?.mkv",
		},
		{
			name:     "curly braces",
			input:    "file{1080p}.mkv",
			expected: "file\\{1080p\\}.mkv",
		},
		{
			name:     "multiple special chars",
			input:    "The.Morning.Show.S01E02.1080p.WEBRip.X264-EVO[TGx]",
			expected: "The.Morning.Show.S01E02.1080p.WEBRip.X264-EVO\\[TGx\\]",
		},
		{
			name:     "all special chars",
			input:    "file[]*?{}.mkv",
			expected: "file\\[\\]\\*\\?\\{\\}.mkv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeGlobChars(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPrepareCopyRequest_WithSpecialChars(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			RemoteName: "seedbox",
		},
	}
	mockMonitor := mocks.NewMockGatekeeper(t)

	executor := &RCloneExecutor{
		config:     cfg,
		gatekeeper: mockMonitor,
	}

	job := &models.Job{
		RemotePath: "/downloads/The.Morning.Show.S01E02[TGx]",
		LocalPath:  "/local/downloads",
	}

	srcFs, dstFs, filter := executor.prepareCopyRequest(job)

	assert.Equal(t, "seedbox:/downloads/", srcFs)
	assert.Equal(t, "/local/downloads/", dstFs)
	assert.NotNil(t, filter)

	includeRules, ok := filter["IncludeRule"].([]string)
	require.True(t, ok)
	// Verify brackets are escaped
	assert.Contains(t, includeRules, "The.Morning.Show.S01E02\\[TGx\\]")
	assert.Contains(t, includeRules, "The.Morning.Show.S01E02\\[TGx\\]/**")
}
