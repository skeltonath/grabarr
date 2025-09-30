package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
	"grabarr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewSyncService(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			DaemonAddr: "localhost:5572",
		},
	}
	mockRepo := mocks.NewMockSyncRepository(t)

	service := NewSyncService(cfg, mockRepo)

	assert.NotNil(t, service)
	assert.Equal(t, cfg, service.config)
	assert.Equal(t, mockRepo, service.repository)
	assert.NotNil(t, service.client)
}

func TestStartSync_Success(t *testing.T) {
	cfg := &config.Config{
		Downloads: config.DownloadsConfig{
			LocalPath: "/local/downloads",
		},
	}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
	}

	ctx := context.Background()
	remotePath := "/remote/path/to/sync"

	// Mock active count check
	mockRepo.EXPECT().
		GetActiveSyncJobsCount().
		Return(0, nil).
		Once()

	// Mock daemon ping
	mockClient.EXPECT().
		Ping(ctx).
		Return(nil).
		Once()

	// Mock CreateSyncJob
	mockRepo.EXPECT().
		CreateSyncJob(mock.MatchedBy(func(job *models.SyncJob) bool {
			return job.RemotePath == remotePath &&
				job.LocalPath == "/local/downloads" &&
				job.Status == models.SyncStatusQueued
		})).
		Return(nil).
		Once()

	// Mock the CopyWithIgnoreExisting call that will happen in the goroutine
	// We use Maybe() because timing is unpredictable in goroutines
	mockClient.EXPECT().
		CopyWithIgnoreExisting(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&models.RCloneCopyResponse{JobID: 999}, nil).
		Maybe()

	// Mock GetJobStatus for the monitoring loop (will be called repeatedly)
	mockClient.EXPECT().
		GetJobStatus(mock.Anything, int64(999)).
		Return(&models.RCloneJobStatus{
			ID:       999,
			Finished: true,
			Success:  true,
		}, nil).
		Maybe()

	// Mock UpdateSyncJob for progress updates and completion
	mockRepo.EXPECT().
		UpdateSyncJob(mock.Anything).
		Return(nil).
		Maybe()

	syncJob, err := service.StartSync(ctx, remotePath)

	require.NoError(t, err)
	assert.NotNil(t, syncJob)
	assert.Equal(t, remotePath, syncJob.RemotePath)
	assert.Equal(t, "/local/downloads", syncJob.LocalPath)
	assert.Equal(t, models.SyncStatusQueued, syncJob.Status)

	// Give goroutine time to start (it will fail on CopyWithIgnoreExisting but that's expected)
	time.Sleep(50 * time.Millisecond)
}

func TestStartSync_MaxConcurrentReached(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
	}

	ctx := context.Background()

	// Mock active count at max
	mockRepo.EXPECT().
		GetActiveSyncJobsCount().
		Return(MaxConcurrentSyncs, nil).
		Once()

	syncJob, err := service.StartSync(ctx, "/remote/path")

	assert.Error(t, err)
	assert.Nil(t, syncJob)
	assert.Contains(t, err.Error(), "maximum concurrent syncs")
}

func TestStartSync_ActiveCountError(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
	}

	ctx := context.Background()

	// Mock active count check fails
	mockRepo.EXPECT().
		GetActiveSyncJobsCount().
		Return(0, errors.New("database error")).
		Once()

	syncJob, err := service.StartSync(ctx, "/remote/path")

	assert.Error(t, err)
	assert.Nil(t, syncJob)
	assert.Contains(t, err.Error(), "failed to check active sync count")
}

func TestStartSync_DaemonNotResponsive(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
	}

	ctx := context.Background()

	mockRepo.EXPECT().
		GetActiveSyncJobsCount().
		Return(0, nil).
		Once()

	// Mock daemon not responsive
	mockClient.EXPECT().
		Ping(ctx).
		Return(errors.New("connection refused")).
		Once()

	syncJob, err := service.StartSync(ctx, "/remote/path")

	assert.Error(t, err)
	assert.Nil(t, syncJob)
	assert.Contains(t, err.Error(), "rclone daemon not responsive")
}

func TestStartSync_CreateJobError(t *testing.T) {
	cfg := &config.Config{
		Downloads: config.DownloadsConfig{
			LocalPath: "/local",
		},
	}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
	}

	ctx := context.Background()

	mockRepo.EXPECT().
		GetActiveSyncJobsCount().
		Return(0, nil).
		Once()

	mockClient.EXPECT().
		Ping(ctx).
		Return(nil).
		Once()

	// Mock CreateSyncJob fails
	mockRepo.EXPECT().
		CreateSyncJob(mock.Anything).
		Return(errors.New("database write error")).
		Once()

	syncJob, err := service.StartSync(ctx, "/remote/path")

	assert.Error(t, err)
	assert.Nil(t, syncJob)
	assert.Contains(t, err.Error(), "failed to create sync job")
}

func TestGetSyncJob_Success(t *testing.T) {
	mockRepo := mocks.NewMockSyncRepository(t)

	service := &SyncService{
		repository: mockRepo,
	}

	expectedJob := &models.SyncJob{
		ID:         123,
		RemotePath: "/remote/path",
		Status:     models.SyncStatusCompleted,
	}

	mockRepo.EXPECT().
		GetSyncJob(int64(123)).
		Return(expectedJob, nil).
		Once()

	job, err := service.GetSyncJob(123)

	require.NoError(t, err)
	assert.Equal(t, expectedJob, job)
}

func TestGetSyncJobs_WithFilter(t *testing.T) {
	mockRepo := mocks.NewMockSyncRepository(t)

	service := &SyncService{
		repository: mockRepo,
	}

	filter := models.SyncFilter{
		Status: []models.SyncStatus{models.SyncStatusRunning},
	}

	expectedJobs := []*models.SyncJob{
		{ID: 1, Status: models.SyncStatusRunning},
		{ID: 2, Status: models.SyncStatusRunning},
	}

	mockRepo.EXPECT().
		GetSyncJobs(filter).
		Return(expectedJobs, nil).
		Once()

	jobs, err := service.GetSyncJobs(filter)

	require.NoError(t, err)
	assert.Equal(t, expectedJobs, jobs)
}

func TestGetSyncSummary_Success(t *testing.T) {
	mockRepo := mocks.NewMockSyncRepository(t)

	service := &SyncService{
		repository: mockRepo,
	}

	expectedSummary := &models.SyncSummary{
		TotalSyncs:     100,
		CompletedSyncs: 80,
		RunningSyncs:   5,
	}

	mockRepo.EXPECT().
		GetSyncSummary().
		Return(expectedSummary, nil).
		Once()

	summary, err := service.GetSyncSummary()

	require.NoError(t, err)
	assert.Equal(t, expectedSummary, summary)
}

func TestCancelSync_Success(t *testing.T) {
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	service := &SyncService{
		repository: mockRepo,
		client:     mockClient,
	}

	ctx := context.Background()
	rcloneJobID := int64(456)

	syncJob := &models.SyncJob{
		ID:           123,
		Status:       models.SyncStatusRunning,
		RCloneJobID:  &rcloneJobID,
	}

	mockRepo.EXPECT().
		GetSyncJob(int64(123)).
		Return(syncJob, nil).
		Once()

	// Mock StopJob
	mockClient.EXPECT().
		StopJob(mock.Anything, rcloneJobID).
		Return(nil).
		Once()

	// Mock UpdateSyncJob
	mockRepo.EXPECT().
		UpdateSyncJob(mock.MatchedBy(func(job *models.SyncJob) bool {
			return job.ID == 123 && job.Status == models.SyncStatusCancelled
		})).
		Return(nil).
		Once()

	err := service.CancelSync(ctx, 123)

	assert.NoError(t, err)
}

func TestCancelSync_NotFound(t *testing.T) {
	mockRepo := mocks.NewMockSyncRepository(t)

	service := &SyncService{
		repository: mockRepo,
	}

	ctx := context.Background()

	mockRepo.EXPECT().
		GetSyncJob(int64(123)).
		Return(nil, errors.New("not found")).
		Once()

	err := service.CancelSync(ctx, 123)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sync job not found")
}

func TestCancelSync_NotActive(t *testing.T) {
	mockRepo := mocks.NewMockSyncRepository(t)

	service := &SyncService{
		repository: mockRepo,
	}

	ctx := context.Background()

	syncJob := &models.SyncJob{
		ID:     123,
		Status: models.SyncStatusCompleted, // Not active
	}

	mockRepo.EXPECT().
		GetSyncJob(int64(123)).
		Return(syncJob, nil).
		Once()

	err := service.CancelSync(ctx, 123)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sync job is not active")
}

func TestCancelSync_StopJobError(t *testing.T) {
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	service := &SyncService{
		repository: mockRepo,
		client:     mockClient,
	}

	ctx := context.Background()
	rcloneJobID := int64(456)

	syncJob := &models.SyncJob{
		ID:          123,
		Status:      models.SyncStatusRunning,
		RCloneJobID: &rcloneJobID,
	}

	mockRepo.EXPECT().
		GetSyncJob(int64(123)).
		Return(syncJob, nil).
		Once()

	// Mock StopJob fails (but should still cancel)
	mockClient.EXPECT().
		StopJob(mock.Anything, rcloneJobID).
		Return(errors.New("rclone error")).
		Once()

	// Mock UpdateSyncJob - should still be called
	mockRepo.EXPECT().
		UpdateSyncJob(mock.MatchedBy(func(job *models.SyncJob) bool {
			return job.Status == models.SyncStatusCancelled
		})).
		Return(nil).
		Once()

	err := service.CancelSync(ctx, 123)

	// Should succeed despite StopJob error
	assert.NoError(t, err)
}

func TestPrepareSyncRequest_BasicPath(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			RemoteName: "seedbox",
		},
	}

	service := &SyncService{
		config: cfg,
	}

	syncJob := &models.SyncJob{
		RemotePath: "/downloads/movies",
		LocalPath:  "/local/media",
	}

	srcFs, dstFs, filter := service.prepareSyncRequest(syncJob)

	assert.Equal(t, "seedbox:/downloads/movies/", srcFs)
	assert.Equal(t, "/local/media/", dstFs)
	assert.Empty(t, filter)
}

func TestPrepareSyncRequest_TrailingSlash(t *testing.T) {
	cfg := &config.Config{
		Rclone: config.RcloneConfig{
			RemoteName: "remote",
		},
	}

	service := &SyncService{
		config: cfg,
	}

	syncJob := &models.SyncJob{
		RemotePath: "/path/with/slash/",
		LocalPath:  "/local/path/",
	}

	srcFs, dstFs, filter := service.prepareSyncRequest(syncJob)

	// Should still have trailing slash
	assert.Equal(t, "remote:/path/with/slash/", srcFs)
	assert.Equal(t, "/local/path/", dstFs)
	assert.Empty(t, filter)
}

func TestUpdateSyncProgress_WithData(t *testing.T) {
	service := &SyncService{}

	syncJob := &models.SyncJob{
		ID: 123,
	}

	status := &models.RCloneJobStatus{
		Output: models.RCloneOutput{
			Bytes:          5000,
			TotalBytes:     10000,
			Transfers:      2,
			TotalTransfers: 5,
			Speed:          1000,
		},
	}

	service.updateSyncProgress(syncJob, status)

	assert.Equal(t, int64(10000), syncJob.Progress.TotalBytes)
	assert.Equal(t, int64(5000), syncJob.Progress.TransferredBytes)
	assert.Equal(t, float64(50), syncJob.Progress.Percentage)
	assert.Equal(t, 5, syncJob.Progress.FilesTotal)
	assert.Equal(t, 2, syncJob.Progress.FilesCompleted)
	assert.Equal(t, int64(1000), syncJob.Progress.TransferSpeed)
}

func TestUpdateSyncProgress_ETACalculation(t *testing.T) {
	service := &SyncService{}

	syncJob := &models.SyncJob{
		ID: 123,
	}

	status := &models.RCloneJobStatus{
		Output: models.RCloneOutput{
			Bytes:      3000,
			TotalBytes: 10000,
			Speed:      1000, // 1000 bytes/sec
		},
	}

	before := time.Now()
	service.updateSyncProgress(syncJob, status)
	after := time.Now().Add(10 * time.Second)

	require.NotNil(t, syncJob.Progress.ETA)
	assert.True(t, syncJob.Progress.ETA.After(before))
	assert.True(t, syncJob.Progress.ETA.Before(after))

	// ETA should be roughly 7 seconds from now (7000 bytes remaining / 1000 bytes/sec)
	expectedETA := time.Now().Add(7 * time.Second)
	assert.WithinDuration(t, expectedETA, *syncJob.Progress.ETA, 2*time.Second)
}