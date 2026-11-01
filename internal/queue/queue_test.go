package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skeltonath/grabarr/internal/config"
	"github.com/skeltonath/grabarr/internal/executor"
	"github.com/skeltonath/grabarr/internal/interfaces"
	"github.com/skeltonath/grabarr/internal/mocks"
	"github.com/skeltonath/grabarr/internal/models"
	"github.com/skeltonath/grabarr/internal/repository"
	"github.com/skeltonath/grabarr/internal/testutil"

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
	assert.NotNil(t, queue.wake)
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
		// Insert already running. The scheduler is live, so a job inserted as
		// queued could be picked up (calling the gatekeeper mock) before it
		// was marked running.
		job.MarkStarted()
		err = repo.CreateJob(job)
		require.NoError(t, err)
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
		},
	}
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)

	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(&executor.PermanentError{Msg: "execution failed", Cause: errors.New("bad path")}).
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

	// Verify job was marked as failed (permanent error skips retry)
	updatedJob, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusFailed, updatedJob.Status)
	assert.Contains(t, updatedJob.ErrorMessage, "execution failed")
}

func TestExecuteJob_PermanentError(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
		},
	}
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)
	mockNotifier := mocks.NewMockNotifier(t)

	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(&executor.PermanentError{Msg: "file not found", Cause: errors.New("no such file")}).
		Once()

	mockNotifier.EXPECT().IsEnabled().Return(true).Once()
	mockNotifier.EXPECT().NotifyJobFailed(mock.Anything).Return(nil).Once()

	q := New(repo, cfg, mockChecker, mockNotifier)
	q.SetJobExecutor(mockExecutor)
	queue := q.(*queue)

	ctx := context.Background()
	queue.schedulerCtx = ctx

	job := testutil.CreateTestJob(func(j *models.Job) {
		j.Status = models.JobStatusQueued
	})
	require.NoError(t, repo.CreateJob(job))

	queue.executeJob(ctx, job)

	updatedJob, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusFailed, updatedJob.Status)
	assert.Equal(t, 0, updatedJob.Retries)
	assert.Contains(t, updatedJob.ErrorMessage, "file not found")
}

// A torrent removed from the seedbox mid-copy cancels the job with a plain
// message and sends no alert (the notifier mock fails the test if called).
func TestExecuteJob_SourceGoneCancelsQuietly(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{Jobs: config.JobsConfig{MaxConcurrent: 2}}
	mockExecutor := mocks.NewMockJobExecutor(t)
	mockNotifier := mocks.NewMockNotifier(t)
	mockExecutor.EXPECT().Execute(mock.Anything, mock.Anything).
		Return(&executor.PermanentError{Msg: "removed from the seedbox", Cause: errors.New("exit 24"), SourceGone: true}).
		Once()

	q := New(repo, cfg, mocks.NewMockGatekeeper(t), mockNotifier)
	q.SetJobExecutor(mockExecutor)
	queue := q.(*queue)
	ctx := context.Background()
	queue.schedulerCtx = ctx

	job := testutil.CreateTestJob(func(j *models.Job) { j.Status = models.JobStatusQueued })
	require.NoError(t, repo.CreateJob(job))
	queue.executeJob(ctx, job)

	updated, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCancelled, updated.Status)
	assert.Equal(t, "Removed from the seedbox before the copy finished", updated.ErrorMessage)
	assert.Equal(t, 0, updated.Retries)
}

func TestExecuteJob_RetryableError(t *testing.T) {
	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent: 2,
		},
	}
	mockChecker := mocks.NewMockGatekeeper(t)
	mockExecutor := mocks.NewMockJobExecutor(t)
	mockNotifier := mocks.NewMockNotifier(t)

	mockExecutor.EXPECT().
		Execute(mock.Anything, mock.Anything).
		Return(errors.New("connection reset by peer")).
		Once()

	q := New(repo, cfg, mockChecker, mockNotifier)
	q.SetJobExecutor(mockExecutor)
	queue := q.(*queue)

	ctx := context.Background()
	queue.schedulerCtx = ctx

	job := testutil.CreateTestJob(func(j *models.Job) {
		j.Status = models.JobStatusQueued
	})
	require.NoError(t, repo.CreateJob(job))

	queue.executeJob(ctx, job)

	// Job should be re-queued for retry, not failed
	updatedJob, err := repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusQueued, updatedJob.Status)
	assert.Equal(t, 1, updatedJob.Retries)
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
			MaxConcurrent: 2,
			MaxRetries:    0,
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

// "Download now" starts a waiting job past max_concurrent and past the
// bandwidth ceiling, but not onto a full cache disk.
func TestStartNow(t *testing.T) {
	setup := func(t *testing.T, decision interfaces.GateDecision) (*queue, *repository.Repository, *mocks.MockJobExecutor) {
		repo := testutil.SetupTestDB(t)
		cfg := &config.Config{Jobs: config.JobsConfig{MaxConcurrent: 1}}
		gk := mocks.NewMockGatekeeper(t)
		gk.EXPECT().CanStartJob(mock.Anything).Return(decision).Maybe()
		exec := mocks.NewMockJobExecutor(t)
		q := New(repo, cfg, gk, nil).(*queue)
		q.SetJobExecutor(exec)
		q.running = true
		q.schedulerCtx = context.Background()
		// One copy already running: the normal limit is full.
		q.activeJobs[999] = func() {}
		return q, repo, exec
	}

	t.Run("starts an extra copy past the limit", func(t *testing.T) {
		q, r, exec := setup(t, interfaces.GateDecision{Allowed: false, Reason: "Bandwidth limit reached"})
		job := testutil.CreateTestJob(func(j *models.Job) { j.Status = models.JobStatusQueued })
		require.NoError(t, r.CreateJob(job))
		done := make(chan struct{})
		exec.EXPECT().Execute(mock.Anything, mock.Anything).RunAndReturn(func(context.Context, *models.Job) error {
			close(done)
			return nil
		}).Once()

		require.NoError(t, q.StartNow(job.ID))
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("job didn't start")
		}
		assert.False(t, q.canScheduleNewJob(), "the regular queue still sees the limit as full")
	})

	t.Run("refuses a full cache disk", func(t *testing.T) {
		q, r, _ := setup(t, interfaces.GateDecision{Allowed: false, Reason: "Cache disk usage too high"})
		job := testutil.CreateTestJob(func(j *models.Job) { j.Status = models.JobStatusQueued })
		require.NoError(t, r.CreateJob(job))
		err := q.StartNow(job.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cache disk usage too high")
	})

	t.Run("only waiting jobs", func(t *testing.T) {
		q, r, _ := setup(t, interfaces.GateDecision{Allowed: true})
		job := testutil.CreateTestJob(func(j *models.Job) { j.Status = models.JobStatusCompleted })
		require.NoError(t, r.CreateJob(job))
		assert.ErrorIs(t, q.StartNow(job.ID), ErrNotWaiting)
	})

	t.Run("not while the scheduler is off (preview)", func(t *testing.T) {
		q, r, _ := setup(t, interfaces.GateDecision{Allowed: true})
		q.running = false
		job := testutil.CreateTestJob(func(j *models.Job) { j.Status = models.JobStatusQueued })
		require.NoError(t, r.CreateJob(job))
		assert.ErrorIs(t, q.StartNow(job.ID), ErrNotRunning)
	})
}
