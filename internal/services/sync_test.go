package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"
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
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockNotifier := mocks.NewMockNotifier(t)

	service := NewSyncService(cfg, mockRepo, mockGatekeeper, mockNotifier)

	assert.NotNil(t, service)
	assert.Equal(t, cfg, service.config)
	assert.Equal(t, mockRepo, service.repository)
	assert.NotNil(t, service.client)
	assert.Equal(t, mockGatekeeper, service.gatekeeper)
	assert.Equal(t, mockNotifier, service.notifier)
}

func TestStartSync_Success(t *testing.T) {
	cfg := &config.Config{
		Downloads: config.DownloadsConfig{
			LocalPath: "/local/downloads",
		},
	}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)

	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	defer serviceCancel()

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
		gatekeeper: mockGatekeeper,
		ctx:        serviceCtx,
		cancel:     serviceCancel,
	}

	ctx := context.Background()
	remotePath := "/remote/path/to/sync"

	// Mock gatekeeper check
	mockGatekeeper.EXPECT().
		CanStartSync().
		Return(interfaces.GateDecision{Allowed: true}).
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
	mockGatekeeper := mocks.NewMockGatekeeper(t)

	// Mock gatekeeper blocking the sync
	mockGatekeeper.EXPECT().
		CanStartSync().
		Return(interfaces.GateDecision{Allowed: false, Reason: "Another sync is already running"}).
		Once()

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
		gatekeeper: mockGatekeeper,
	}

	ctx := context.Background()

	syncJob, err := service.StartSync(ctx, "/remote/path")

	assert.Error(t, err)
	assert.Nil(t, syncJob)
	assert.Contains(t, err.Error(), "cannot start sync")
	assert.Contains(t, err.Error(), "Another sync is already running")
}

func TestStartSync_GatekeeperBlocked(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)

	// Mock gatekeeper blocking for bandwidth reasons
	mockGatekeeper.EXPECT().
		CanStartSync().
		Return(interfaces.GateDecision{Allowed: false, Reason: "Bandwidth limit exceeded"}).
		Once()

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
		gatekeeper: mockGatekeeper,
	}

	ctx := context.Background()

	syncJob, err := service.StartSync(ctx, "/remote/path")

	assert.Error(t, err)
	assert.Nil(t, syncJob)
	assert.Contains(t, err.Error(), "cannot start sync")
	assert.Contains(t, err.Error(), "Bandwidth limit exceeded")
}

func TestStartSync_DaemonNotResponsive(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)

	// Mock gatekeeper allowing the sync
	mockGatekeeper.EXPECT().
		CanStartSync().
		Return(interfaces.GateDecision{Allowed: true}).
		Once()

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
		gatekeeper: mockGatekeeper,
	}

	ctx := context.Background()

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
	mockGatekeeper := mocks.NewMockGatekeeper(t)

	// Mock gatekeeper allowing the sync
	mockGatekeeper.EXPECT().
		CanStartSync().
		Return(interfaces.GateDecision{Allowed: true}).
		Once()

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
		gatekeeper: mockGatekeeper,
	}

	ctx := context.Background()

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
		ID:          123,
		Status:      models.SyncStatusRunning,
		RCloneJobID: &rcloneJobID,
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

func TestRecoverInterruptedSyncs_Success(t *testing.T) {
	cfg := &config.Config{
		Downloads: config.DownloadsConfig{
			LocalPath: "/local/downloads",
		},
	}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Create mock running syncs
	runningSyncs := []*models.SyncJob{
		{
			ID:         1,
			RemotePath: "/remote/path1",
			LocalPath:  "/local/downloads",
			Status:     models.SyncStatusRunning,
		},
		{
			ID:         2,
			RemotePath: "/remote/path2",
			LocalPath:  "/local/downloads",
			Status:     models.SyncStatusRunning,
		},
	}

	// Mock GetSyncJobs to return running syncs
	mockRepo.EXPECT().
		GetSyncJobs(mock.MatchedBy(func(filter models.SyncFilter) bool {
			return len(filter.Status) == 1 && filter.Status[0] == models.SyncStatusRunning
		})).
		Return(runningSyncs, nil).
		Once()

	// Mock UpdateSyncJob - will be called during recovery and potentially during async execution
	mockRepo.EXPECT().
		UpdateSyncJob(mock.Anything).
		Return(nil).
		Maybe()

	// Mock the async executeSyncJob calls (they'll happen in goroutines)
	mockClient.EXPECT().
		CopyWithIgnoreExisting(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&models.RCloneCopyResponse{JobID: 123}, nil).
		Maybe()

	// Execute recovery
	err := service.RecoverInterruptedSyncs()
	assert.NoError(t, err)
}

func TestRecoverInterruptedSyncs_NoRunningSyncs(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Mock GetSyncJobs to return empty list
	mockRepo.EXPECT().
		GetSyncJobs(mock.Anything).
		Return([]*models.SyncJob{}, nil).
		Once()

	err := service.RecoverInterruptedSyncs()
	assert.NoError(t, err)
}

func TestRecoverInterruptedSyncs_GetJobsError(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Mock GetSyncJobs to return error
	mockRepo.EXPECT().
		GetSyncJobs(mock.Anything).
		Return(nil, errors.New("database error")).
		Once()

	err := service.RecoverInterruptedSyncs()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get running syncs")
}

func TestShutdown_Success(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	ctx, cancel := context.WithCancel(context.Background())

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
		ctx:        ctx,
		cancel:     cancel,
	}

	rcloneJobID := int64(999)
	runningSyncs := []*models.SyncJob{
		{
			ID:          1,
			RemotePath:  "/remote/path1",
			Status:      models.SyncStatusRunning,
			RCloneJobID: &rcloneJobID,
		},
	}

	// Mock GetSyncJobs to return running syncs
	mockRepo.EXPECT().
		GetSyncJobs(mock.MatchedBy(func(filter models.SyncFilter) bool {
			return len(filter.Status) == 1 && filter.Status[0] == models.SyncStatusRunning
		})).
		Return(runningSyncs, nil).
		Once()

	// Mock StopJob
	mockClient.EXPECT().
		StopJob(mock.Anything, rcloneJobID).
		Return(nil).
		Once()

	// Mock UpdateSyncJob
	mockRepo.EXPECT().
		UpdateSyncJob(mock.MatchedBy(func(job *models.SyncJob) bool {
			return job.ID == 1 && job.Status == models.SyncStatusQueued
		})).
		Return(nil).
		Once()

	err := service.Shutdown()
	assert.NoError(t, err)
}

func TestShutdown_NoActiveSyncs(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)

	ctx, cancel := context.WithCancel(context.Background())

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		ctx:        ctx,
		cancel:     cancel,
	}

	// Mock GetSyncJobs to return empty list
	mockRepo.EXPECT().
		GetSyncJobs(mock.Anything).
		Return([]*models.SyncJob{}, nil).
		Once()

	err := service.Shutdown()
	assert.NoError(t, err)
}

func TestShutdown_StopJobError(t *testing.T) {
	cfg := &config.Config{}
	mockRepo := mocks.NewMockSyncRepository(t)
	mockClient := mocks.NewMockRCloneClient(t)

	ctx, cancel := context.WithCancel(context.Background())

	service := &SyncService{
		config:     cfg,
		repository: mockRepo,
		client:     mockClient,
		ctx:        ctx,
		cancel:     cancel,
	}

	rcloneJobID := int64(999)
	runningSyncs := []*models.SyncJob{
		{
			ID:          1,
			RemotePath:  "/remote/path1",
			Status:      models.SyncStatusRunning,
			RCloneJobID: &rcloneJobID,
		},
	}

	// Mock GetSyncJobs
	mockRepo.EXPECT().
		GetSyncJobs(mock.Anything).
		Return(runningSyncs, nil).
		Once()

	// Mock StopJob to return error (should be logged but not fail shutdown)
	mockClient.EXPECT().
		StopJob(mock.Anything, rcloneJobID).
		Return(errors.New("stop job failed")).
		Once()

	// Mock UpdateSyncJob (should still be called)
	mockRepo.EXPECT().
		UpdateSyncJob(mock.MatchedBy(func(job *models.SyncJob) bool {
			return job.ID == 1 && job.Status == models.SyncStatusQueued
		})).
		Return(nil).
		Once()

	err := service.Shutdown()
	assert.NoError(t, err) // Shutdown should succeed even if stop job fails
}
