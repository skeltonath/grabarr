package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"
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
	mockChecker := mocks.NewMockGatekeeper(t)
	mockNotifier := mocks.NewMockNotifier(t)

	q := New(repo, cfg, mockChecker, mockNotifier)

	assert.NotNil(t, q)
	queue := q.(*queue)
	assert.Equal(t, repo, queue.repo)
	assert.Equal(t, cfg, queue.config)
	assert.Equal(t, mockChecker, queue.gatekeeper)
	assert.Equal(t, mockNotifier, queue.notifier)
	assert.NotNil(t, queue.activeJobs)
	assert.NotNil(t, queue.jobQueue)
	assert.False(t, queue.running)
}

func TestSetJobExecutor(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockGatekeeper(t)
	mockNotifier := mocks.NewMockNotifier(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker, mockNotifier)
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
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker, nil)
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
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker, nil)
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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

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
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker, nil)
	q.SetJobExecutor(mockExecutor)

	ctx := context.Background()
	err := q.Start(ctx)
	require.NoError(t, err)

	err = q.Stop()
	assert.NoError(t, err)

	queue := q.(*queue)
	assert.False(t, queue.running)
}

func TestStop_MarksRunningJobsAsQueued(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
		},
		Server: config.ServerConfig{
			ShutdownTimeout: 1 * time.Second,
		},
	}
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker, nil)
	q.SetJobExecutor(mockExecutor)

	ctx := context.Background()
	err := q.Start(ctx)
	require.NoError(t, err)

	// Manually create a job and mark it as running (simulating a running job)
	job := testutil.CreateTestJob()
	err = repo.CreateJob(job)
	require.NoError(t, err)

	// Mark it as running directly in the database
	job.MarkStarted()
	err = repo.UpdateJob(job)
	require.NoError(t, err)

	// Manually add to active jobs map to simulate it's being tracked
	queue := q.(*queue)
	queue.mu.Lock()
	_, cancel := context.WithCancel(ctx)
	queue.activeJobs[job.ID] = cancel
	queue.mu.Unlock()

	// Stop the queue
	err = q.Stop()
	assert.NoError(t, err)

	// Verify job was marked as queued
	queuedJob, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusQueued, queuedJob.Status)
}

func TestStop_HandlesMultipleRunningJobs(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 3,
		},
		Server: config.ServerConfig{
			ShutdownTimeout: 1 * time.Second,
		},
	}
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	q := New(repo, cfg, mockChecker, nil)
	q.SetJobExecutor(mockExecutor)

	ctx := context.Background()
	err := q.Start(ctx)
	require.NoError(t, err)

	// Create multiple jobs and mark them as running
	jobs := []*models.Job{
		testutil.CreateTestJob(),
		testutil.CreateTestJob(),
		testutil.CreateTestJob(),
	}

	queue := q.(*queue)
	for _, job := range jobs {
		// Create in database
		err = repo.CreateJob(job)
		require.NoError(t, err)

		// Mark as running
		job.MarkStarted()
		err = repo.UpdateJob(job)
		require.NoError(t, err)

		// Add to active jobs map
		queue.mu.Lock()
		_, cancel := context.WithCancel(ctx)
		queue.activeJobs[job.ID] = cancel
		queue.mu.Unlock()
	}

	// Stop the queue
	err = q.Stop()
	assert.NoError(t, err)

	// Verify all jobs were marked as queued
	for _, job := range jobs {
		queuedJob, err := repo.GetJob(job.ID)
		require.NoError(t, err)
		assert.Equal(t, models.JobStatusQueued, queuedJob.Status)
	}
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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

	err := q.CancelJob(99999)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get job")
}

func TestDeleteJob_Success(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

	job := testutil.CreateTestJob(func(j *models.Job) {
		j.Status = models.JobStatusCompleted
	})
	require.NoError(t, repo.CreateJob(job))

	err := q.DeleteJob(job.ID)
	assert.NoError(t, err)

	// Verify job is deleted from database
	_, err = repo.GetJob(job.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteJob_NotFound(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{}
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)

	// Deleting a non-existent job should succeed (SQL DELETE just affects 0 rows)
	err := q.DeleteJob(99999)
	assert.NoError(t, err)
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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)
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
	mockChecker := mocks.NewMockGatekeeper(t)

	q := New(repo, cfg, mockChecker, nil)
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
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(nil).
		Once()

	q := New(repo, cfg, mockChecker, nil)
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
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(errors.New("execution failed")).
		Once()

	q := New(repo, cfg, mockChecker, nil)
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
// 8. Integration Test
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
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	// Allow resource checks
	mockChecker.EXPECT().
		CanStartJob(mock.AnythingOfType("int64")).
		Return(interfaces.GateDecision{Allowed: true}).
		Maybe()

	// Mock successful execution
	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(nil).
		Maybe()

	q := New(repo, cfg, mockChecker, nil)
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
