package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"
	"grabarr/internal/mocks"
	"grabarr/internal/models"
	"grabarr/internal/repository"
	"grabarr/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// blockingExecutor records the order jobs were handed to it and holds each one
// open until released, so a test can inspect which jobs occupy the concurrency
// slots.
type blockingExecutor struct {
	mu      sync.Mutex
	started []string
	release chan struct{}
}

func newBlockingExecutor() *blockingExecutor {
	return &blockingExecutor{release: make(chan struct{})}
}

func (e *blockingExecutor) Execute(ctx context.Context, job *models.Job) error {
	e.mu.Lock()
	e.started = append(e.started, job.Name)
	e.mu.Unlock()

	select {
	case <-e.release:
	case <-ctx.Done():
	}
	return nil
}

func (e *blockingExecutor) startedJobs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.started...)
}

// newTestQueue builds a queue wired for direct processQueue() calls, without
// running the scheduler goroutine.
func newTestQueue(t *testing.T, maxConcurrent int, gatekeeper interfaces.Gatekeeper) (*queue, *blockingExecutor, *repository.Repository) {
	t.Helper()

	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Jobs: config.JobsConfig{MaxConcurrent: maxConcurrent, MaxRetries: 3},
	}

	exec := newBlockingExecutor()
	q := New(repo, cfg, gatekeeper, nil).(*queue)
	q.SetJobExecutor(exec)
	q.schedulerCtx, q.schedulerCancel = context.WithCancel(context.Background())

	t.Cleanup(func() {
		close(exec.release)
		q.schedulerCancel()
	})

	return q, exec, repo
}

func allowAllGatekeeper(t *testing.T) *mocks.MockGatekeeper {
	t.Helper()
	gk := mocks.NewMockGatekeeper(t)
	gk.EXPECT().
		CanStartJob(mock.AnythingOfType("int64")).
		Return(interfaces.GateDecision{Allowed: true}).
		Maybe()
	return gk
}

func enqueueNamed(t *testing.T, q *queue, names ...string) {
	t.Helper()
	for _, name := range names {
		job := testutil.CreateTestJob(func(j *models.Job) { j.Name = name })
		require.NoError(t, q.Enqueue(job))
	}
}

func waitForStarted(t *testing.T, exec *blockingExecutor, want []string) {
	t.Helper()
	assert.Eventually(t, func() bool {
		started := exec.startedJobs()
		if len(started) != len(want) {
			return false
		}
		for i := range want {
			if started[i] != want[i] {
				return false
			}
		}
		return true
	}, 2*time.Second, 5*time.Millisecond, "started jobs never became %v (got %v)", want, exec.startedJobs())
}

func TestProcessQueueStartsOldestJobFirst(t *testing.T) {
	q, exec, _ := newTestQueue(t, 1, allowAllGatekeeper(t))

	enqueueNamed(t, q, "first", "second", "third")

	q.processQueue()

	waitForStarted(t, exec, []string{"first"})
}

func TestProcessQueueFillsConcurrencySlotsWithOldestJobs(t *testing.T) {
	q, exec, _ := newTestQueue(t, 2, allowAllGatekeeper(t))

	enqueueNamed(t, q, "first", "second", "third")

	q.processQueue()

	// Both slots go to the two oldest jobs. Which of the two reaches the
	// executor goroutine first is not ordered, but the third job is not in the
	// running set at all.
	assert.Eventually(t, func() bool {
		return len(exec.startedJobs()) == 2
	}, 2*time.Second, 5*time.Millisecond)
	assert.ElementsMatch(t, []string{"first", "second"}, exec.startedJobs())
}

func TestProcessQueueRunsRemainingJobsInOrderAsSlotsFree(t *testing.T) {
	q, exec, repo := newTestQueue(t, 1, allowAllGatekeeper(t))

	enqueueNamed(t, q, "first", "second", "third")

	q.processQueue()
	waitForStarted(t, exec, []string{"first"})

	// Retire the running job the way executeJob would, then let the scheduler
	// pick the next one.
	first, err := repo.GetJobs(models.JobFilter{Status: []models.JobStatus{models.JobStatusRunning}})
	require.NoError(t, err)
	require.Len(t, first, 1)
	q.CancelJob(first[0].ID)
	first[0].MarkCompleted()
	require.NoError(t, repo.UpdateJob(first[0]))

	q.processQueue()
	waitForStarted(t, exec, []string{"first", "second"})
}

func TestProcessQueueLetsHigherPriorityJumpTheLine(t *testing.T) {
	q, exec, _ := newTestQueue(t, 1, allowAllGatekeeper(t))

	enqueueNamed(t, q, "older")

	// Extraction jobs are created with a raised priority so they run soon after
	// the downloads they depend on, rather than behind the whole backlog.
	urgent := testutil.CreateTestJob(func(j *models.Job) {
		j.Name = "urgent"
		j.Priority = 1
	})
	require.NoError(t, q.Enqueue(urgent))

	q.processQueue()

	waitForStarted(t, exec, []string{"urgent"})
}

func TestProcessQueueWaitsBehindBlockedOldestJob(t *testing.T) {
	gk := mocks.NewMockGatekeeper(t)
	// The oldest job is too big to start right now; the newer one would fit.
	gk.EXPECT().CanStartJob(int64(100)).
		Return(interfaces.GateDecision{Allowed: false, Reason: "File size would exceed cache limit"}).Maybe()
	gk.EXPECT().CanStartJob(int64(1)).
		Return(interfaces.GateDecision{Allowed: true}).Maybe()

	q, exec, repo := newTestQueue(t, 2, gk)

	big := testutil.CreateTestJob(func(j *models.Job) {
		j.Name = "big-and-old"
		j.FileSize = 100
	})
	require.NoError(t, q.Enqueue(big))
	small := testutil.CreateTestJob(func(j *models.Job) {
		j.Name = "small-and-new"
		j.FileSize = 1
	})
	require.NoError(t, q.Enqueue(small))

	q.processQueue()

	// Nothing starts: skipping ahead to the small job is the out-of-order
	// behaviour this scheduler exists to avoid.
	assert.Empty(t, exec.startedJobs())

	blocked, err := repo.GetJob(big.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusPending, blocked.Status)

	waiting, err := repo.GetJob(small.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusQueued, waiting.Status)
}

func TestProcessQueueResumesOnceBlockedJobFits(t *testing.T) {
	gk := mocks.NewMockGatekeeper(t)
	blocked := true
	gk.EXPECT().CanStartJob(mock.AnythingOfType("int64")).
		RunAndReturn(func(int64) interfaces.GateDecision {
			if blocked {
				return interfaces.GateDecision{Allowed: false, Reason: "Cache disk usage too high"}
			}
			return interfaces.GateDecision{Allowed: true}
		}).Maybe()

	q, exec, _ := newTestQueue(t, 1, gk)
	enqueueNamed(t, q, "first", "second")

	q.processQueue()
	assert.Empty(t, exec.startedJobs())

	blocked = false
	q.processQueue()

	// A job left in "pending" is still first in line when resources free up.
	waitForStarted(t, exec, []string{"first"})
}

func TestNextWaitingJobSkipsJobsAlreadyRunning(t *testing.T) {
	q, _, _ := newTestQueue(t, 2, allowAllGatekeeper(t))

	enqueueNamed(t, q, "first", "second")

	first, err := q.nextWaitingJob()
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, "first", first.Name)

	// Between scheduling and the job marking itself started, only the in-memory
	// active set knows it is taken.
	q.mu.Lock()
	q.activeJobs[first.ID] = func() {}
	q.mu.Unlock()

	next, err := q.nextWaitingJob()
	require.NoError(t, err)
	require.NotNil(t, next)
	assert.Equal(t, "second", next.Name)
}

func TestNextWaitingJobReturnsNilWhenNothingWaiting(t *testing.T) {
	q, _, _ := newTestQueue(t, 2, allowAllGatekeeper(t))

	job, err := q.nextWaitingJob()
	require.NoError(t, err)
	assert.Nil(t, job)
}

func TestScheduleJobIgnoresJobAlreadyActive(t *testing.T) {
	q, exec, _ := newTestQueue(t, 2, allowAllGatekeeper(t))

	enqueueNamed(t, q, "only")
	job, err := q.nextWaitingJob()
	require.NoError(t, err)

	q.scheduleJob(job)
	q.scheduleJob(job)

	waitForStarted(t, exec, []string{"only"})
	assert.Len(t, exec.startedJobs(), 1)
}
