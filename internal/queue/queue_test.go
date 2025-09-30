package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
	"grabarr/internal/models"
	"grabarr/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ========================================
// 1. Constructor Tests
// ========================================

func TestNew(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	assert.NotNil(t, q)
	queue := q.(*queue)
	assert.Equal(t, repo, queue.repo)
	assert.Equal(t, cfg, queue.config)
	assert.Equal(t, mockChecker, queue.resourceChecker)
	assert.NotNil(t, queue.activeJobs)
	assert.NotNil(t, queue.jobQueue)
	assert.False(t, queue.running)
}

func TestSetJobExecutor(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockResourceChecker(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker)
	q.SetJobExecutor(mockExecutor)

	queue := q.(*queue)
	assert.Equal(t, mockExecutor, queue.executor)
}

// ========================================
// 2. Lifecycle Tests
// ========================================

func TestStart_Success(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
			MaxRetries:    3,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker)
	q.SetJobExecutor(mockExecutor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := q.Start(ctx)
	require.NoError(t, err)

	queue := q.(*queue)
	assert.True(t, queue.running)
	assert.NotNil(t, queue.schedulerCtx)
	assert.NotNil(t, queue.schedulerCancel)

	// Cleanup
	q.Stop()
}

func TestStart_AlreadyRunning(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker)
	q.SetJobExecutor(mockExecutor)

	ctx := context.Background()
	err := q.Start(ctx)
	require.NoError(t, err)
	defer q.Stop()

	// Try to start again
	err = q.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestStart_NoExecutor(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	ctx := context.Background()
	err := q.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "job executor not set")
}

func TestStop_Success(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
		},
		Server: config.ServerConfig{
			ShutdownTimeout: 5 * time.Second,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker)
	q.SetJobExecutor(mockExecutor)

	ctx := context.Background()
	err := q.Start(ctx)
	require.NoError(t, err)

	err = q.Stop()
	assert.NoError(t, err)

	queue := q.(*queue)
	assert.False(t, queue.running)
}

// ========================================
// 3. Enqueue Tests
// ========================================

func TestEnqueue_Success(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxRetries: 3,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	job := testutil.CreateTestJob()
	err := q.Enqueue(job)
	require.NoError(t, err)

	assert.NotZero(t, job.ID)

	// Verify job was saved to database
	savedJob, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.Name, savedJob.Name)
	assert.Equal(t, models.JobStatusQueued, savedJob.Status)
}

func TestEnqueue_SetsDefaults(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxRetries: 5,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	job := testutil.CreateTestJob(func(j *models.Job) {
		j.Status = ""
		j.MaxRetries = 0
	})

	err := q.Enqueue(job)
	require.NoError(t, err)

	assert.Equal(t, models.JobStatusQueued, job.Status)
	assert.Equal(t, 5, job.MaxRetries)
}

// ========================================
// 4. Job Retrieval Tests
// ========================================

func TestGetJob_Success(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	job := testutil.CreateTestJob()
	require.NoError(t, repo.CreateJob(job))

	retrievedJob, err := q.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, retrievedJob.ID)
	assert.Equal(t, job.Name, retrievedJob.Name)
}

func TestGetJobs_WithFilters(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	// Create some jobs with different statuses
	queuedJob := testutil.CreateTestJob(func(j *models.Job) {
		j.Status = models.JobStatusQueued
	})
	completedJob := testutil.CreateTestJob(func(j *models.Job) {
		j.Name = "completed-job"
		j.Status = models.JobStatusCompleted
	})

	require.NoError(t, repo.CreateJob(queuedJob))
	require.NoError(t, repo.CreateJob(completedJob))

	// Filter for queued jobs only
	jobs, err := q.GetJobs(models.JobFilter{
		Status: []models.JobStatus{models.JobStatusQueued},
	})
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, models.JobStatusQueued, jobs[0].Status)
}

func TestGetSummary_Success(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	// Create jobs with different statuses
	require.NoError(t, repo.CreateJob(testutil.CreateTestJob(func(j *models.Job) {
		j.Status = models.JobStatusQueued
	})))
	require.NoError(t, repo.CreateJob(testutil.CreateTestJob(func(j *models.Job) {
		j.Name = "running"
		j.Status = models.JobStatusRunning
	})))
	require.NoError(t, repo.CreateJob(testutil.CreateTestJob(func(j *models.Job) {
		j.Name = "completed"
		j.Status = models.JobStatusCompleted
	})))

	summary, err := q.GetSummary()
	require.NoError(t, err)
	assert.Equal(t, 3, summary.TotalJobs)
	assert.Equal(t, 1, summary.QueuedJobs)
	assert.Equal(t, 1, summary.RunningJobs)
	assert.Equal(t, 1, summary.CompletedJobs)
}

// ========================================
// 5. Cancel Tests
// ========================================

func TestCancelJob_QueuedJob(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	job := testutil.CreateTestJob(func(j *models.Job) {
		j.Status = models.JobStatusQueued
	})
	require.NoError(t, repo.CreateJob(job))

	err := q.CancelJob(job.ID)
	assert.NoError(t, err)

	updatedJob, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCancelled, updatedJob.Status)
}

func TestCancelJob_NotFound(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)

	err := q.CancelJob(99999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get job")
}

// ========================================
// 6. Scheduling Tests
// ========================================

func TestCanScheduleNewJob_UnderLimit(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 3,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)
	queue := q.(*queue)

	// Add 2 active jobs (under limit of 3)
	queue.activeJobs[1] = func() {}
	queue.activeJobs[2] = func() {}

	assert.True(t, queue.canScheduleNewJob())
}

func TestCanScheduleNewJob_AtLimit(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)
	queue := q.(*queue)

	// Add 2 active jobs (at limit of 2)
	queue.activeJobs[1] = func() {}
	queue.activeJobs[2] = func() {}

	assert.False(t, queue.canScheduleNewJob())
}

// ========================================
// 7. Execution Tests
// ========================================

func TestExecuteJob_Success(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
			MaxRetries:    3,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(nil).
		Once()

	q := New(repo, cfg, mockChecker)
	q.SetJobExecutor(mockExecutor)
	queue := q.(*queue)

	ctx := context.Background()
	queue.schedulerCtx = ctx

	job := testutil.CreateTestJob(func(j *models.Job) {
		j.Status = models.JobStatusQueued
	})
	require.NoError(t, repo.CreateJob(job))

	queue.executeJob(ctx, job)

	// Verify job was marked as completed
	updatedJob, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, updatedJob.Status)
	assert.NotNil(t, updatedJob.StartedAt)
	assert.NotNil(t, updatedJob.CompletedAt)
}

func TestExecuteJob_Failure(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
			MaxRetries:    0, // No retries
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(errors.New("execution failed")).
		Once()

	q := New(repo, cfg, mockChecker)
	q.SetJobExecutor(mockExecutor)
	queue := q.(*queue)

	ctx := context.Background()
	queue.schedulerCtx = ctx

	job := testutil.CreateTestJob(func(j *models.Job) {
		j.Status = models.JobStatusQueued
		j.MaxRetries = 0
	})
	require.NoError(t, repo.CreateJob(job))

	queue.executeJob(ctx, job)

	// Verify job was marked as failed
	updatedJob, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusFailed, updatedJob.Status)
	assert.Contains(t, updatedJob.ErrorMessage, "execution failed")
}

// ========================================
// 8. Retry Tests
// ========================================

func TestCalculateRetryBackoff_ExponentialGrowth(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			RetryBackoffBase: 10 * time.Second,
			RetryBackoffMax:  10 * time.Minute,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)
	queue := q.(*queue)

	tests := []struct {
		retryCount int
		expected   time.Duration
	}{
		{0, 10 * time.Second},  // 10 * 2^0 = 10
		{1, 20 * time.Second},  // 10 * 2^1 = 20
		{2, 40 * time.Second},  // 10 * 2^2 = 40
		{3, 80 * time.Second},  // 10 * 2^3 = 80
		{4, 160 * time.Second}, // 10 * 2^4 = 160
	}

	for _, tt := range tests {
		backoff := queue.calculateRetryBackoff(tt.retryCount)
		assert.Equal(t, tt.expected, backoff, "retry count %d", tt.retryCount)
	}
}

func TestCalculateRetryBackoff_CappedAtMax(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			RetryBackoffBase: 10 * time.Second,
			RetryBackoffMax:  1 * time.Minute, // Cap at 1 minute
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)

	q := New(repo, cfg, mockChecker)
	queue := q.(*queue)

	// After enough retries, should cap at max
	backoff := queue.calculateRetryBackoff(10) // Would be 10 * 2^10 = 10240 seconds
	assert.Equal(t, 1*time.Minute, backoff)
}

// ========================================
// 9. Integration Test
// ========================================

func TestQueueIntegration_SimpleExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent:    2,
			MaxRetries:       0,
			RetryBackoffBase: 1 * time.Second,
			RetryBackoffMax:  1 * time.Minute,
		},
		Server: config.ServerConfig{
			ShutdownTimeout: 5 * time.Second,
		},
	}
	mockChecker := mocks.NewMockResourceChecker(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	// Allow resource checks
	mockChecker.EXPECT().
		CanScheduleJob().
		Return(true).
		Maybe()

	// Mock successful execution
	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(nil).
		Maybe()

	q := New(repo, cfg, mockChecker)
	q.SetJobExecutor(mockExecutor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := q.Start(ctx)
	require.NoError(t, err)
	defer q.Stop()

	// Enqueue a job
	job := testutil.CreateTestJob()
	err = q.Enqueue(job)
	require.NoError(t, err)

	// Wait for execution to complete
	time.Sleep(1 * time.Second)

	// Verify job completed
	updatedJob, err := q.GetJob(job.ID)
	require.NoError(t, err)
	assert.NotEqual(t, models.JobStatusQueued, updatedJob.Status)
}