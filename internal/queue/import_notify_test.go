package queue

import (
	"context"
	"sync"
	"testing"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
	"grabarr/internal/models"
	"grabarr/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// recordingImportNotifier captures the categories it was notified about.
type recordingImportNotifier struct {
	mu         sync.Mutex
	categories []string
}

func (r *recordingImportNotifier) NotifyCompleted(_ context.Context, category string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.categories = append(r.categories, category)
}

func (r *recordingImportNotifier) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.categories...)
}

func newNotifyTestQueue(t *testing.T) (*queue, *mocks.MockJobExecutor) {
	t.Helper()

	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Extraction: config.ExtractionConfig{Enabled: true},
		Jobs:       config.JobsConfig{MaxRetries: 5},
	}

	exec := mocks.NewMockJobExecutor(t)
	q := New(repo, cfg, mocks.NewMockGatekeeper(t), mocks.NewMockNotifier(t))
	q.SetJobExecutor(exec)

	return q.(*queue), exec
}

func jobWithCategory(t *testing.T, q *queue, category string) *models.Job {
	t.Helper()

	job := &models.Job{
		Name:       "Movie.2024.1080p.mkv",
		RemotePath: "/remote/Movie.2024.1080p.mkv",
		LocalPath:  "/downloads/",
		Status:     models.JobStatusQueued,
		MaxRetries: 5,
		Metadata:   models.JobMetadata{Category: category},
	}
	require.NoError(t, q.repo.CreateJob(job))
	return job
}

func TestExecuteJobNotifiesImportOnSuccess(t *testing.T) {
	q, exec := newNotifyTestQueue(t)
	notifier := &recordingImportNotifier{}
	q.SetImportNotifier(notifier)

	exec.On("Execute", mock.Anything, mock.Anything).Return(nil)

	q.executeJob(context.Background(), jobWithCategory(t, q, "dp-movies"))

	assert.Equal(t, []string{"dp-movies"}, notifier.seen(),
		"a completed download should tell the media manager to look for it")
}

func TestExecuteJobDoesNotNotifyImportOnFailure(t *testing.T) {
	q, exec := newNotifyTestQueue(t)
	notifier := &recordingImportNotifier{}
	q.SetImportNotifier(notifier)

	exec.On("Execute", mock.Anything, mock.Anything).Return(assert.AnError)

	q.executeJob(context.Background(), jobWithCategory(t, q, "dp-movies"))

	assert.Empty(t, notifier.seen(), "a failed job has nothing to import")
}

// Sync-scanner jobs carry no category, so the queue must still emit the event
// and let the notifier decide how to route it.
func TestExecuteJobNotifiesImportWithEmptyCategory(t *testing.T) {
	q, exec := newNotifyTestQueue(t)
	notifier := &recordingImportNotifier{}
	q.SetImportNotifier(notifier)

	exec.On("Execute", mock.Anything, mock.Anything).Return(nil)

	q.executeJob(context.Background(), jobWithCategory(t, q, ""))

	assert.Equal(t, []string{""}, notifier.seen(),
		"an uncategorised completion is still an import event")
}

// Nothing is importable until the archive is unpacked, so the download of a
// volume must not trigger a scan — only the extraction that follows it.
func TestExecuteJobDoesNotNotifyImportForArchiveVolumeDownload(t *testing.T) {
	q, exec := newNotifyTestQueue(t)
	notifier := &recordingImportNotifier{}
	q.SetImportNotifier(notifier)

	exec.On("Execute", mock.Anything, mock.Anything).Return(nil)

	job := &models.Job{
		Name:       "movie.r00",
		RemotePath: "/remote/movie.r00",
		LocalPath:  "/downloads/set/",
		Status:     models.JobStatusQueued,
		MaxRetries: 5,
		Metadata: models.JobMetadata{
			Category:    "dp-movies",
			ExtraFields: map[string]interface{}{"archive_group": "/downloads/set/movie"},
		},
	}
	require.NoError(t, q.repo.CreateJob(job))

	q.executeJob(context.Background(), job)

	assert.Empty(t, notifier.seen(), "an archive volume is not importable on its own")
}

func TestExecuteJobNotifiesImportAfterExtraction(t *testing.T) {
	q, exec := newNotifyTestQueue(t)
	notifier := &recordingImportNotifier{}
	q.SetImportNotifier(notifier)

	exec.On("Execute", mock.Anything, mock.Anything).Return(nil)

	job := &models.Job{
		Name:       "extract: movie",
		RemotePath: "/downloads/set/movie.rar",
		LocalPath:  "/downloads/set/",
		Status:     models.JobStatusQueued,
		MaxRetries: 5,
		Metadata: models.JobMetadata{
			Category: "dp-movies",
			ExtraFields: map[string]interface{}{
				"job_type":      "extraction",
				"archive_group": "/downloads/set/movie",
			},
		},
	}
	require.NoError(t, q.repo.CreateJob(job))

	q.executeJob(context.Background(), job)

	assert.Equal(t, []string{"dp-movies"}, notifier.seen(),
		"extraction produces the importable media file")
}

func TestExecuteJobSucceedsWithoutImportNotifier(t *testing.T) {
	q, exec := newNotifyTestQueue(t)

	exec.On("Execute", mock.Anything, mock.Anything).Return(nil)

	job := jobWithCategory(t, q, "dp-movies")
	q.executeJob(context.Background(), job)

	updated, err := q.repo.GetJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, updated.Status,
		"an unconfigured notifier must not break job completion")
}
