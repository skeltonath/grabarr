package notifications

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skeltonath/grabarr/internal/models"
)

// recorder is a Notifier that records what reaches Pushover.
type recorder struct {
	mu     sync.Mutex
	failed []*models.Job
	alerts []string // "title|message|priority"
}

func (r *recorder) IsEnabled() bool                          { return true }
func (r *recorder) NotifyJobCompleted(job *models.Job) error { return nil }
func (r *recorder) NotifyJobFailed(job *models.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = append(r.failed, job)
	return nil
}
func (r *recorder) NotifySystemAlert(title, message string, priority int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alerts = append(r.alerts, fmt.Sprintf("%s|%s|%d", title, message, priority))
	return nil
}

func failedJob(id int64, remotePath, msg string) *models.Job {
	return &models.Job{ID: id, Name: remotePath, RemotePath: remotePath, Status: models.JobStatusFailed, ErrorMessage: msg}
}

func TestFailureDigest_OneFailureKeepsTheUsualAlert(t *testing.T) {
	rec := &recorder{}
	d := NewFailureDigest(rec, time.Hour, time.Hour, "")
	require.NoError(t, d.NotifyJobFailed(failedJob(1, "/s/Movie/movie.mkv", "boom")))

	assert.Empty(t, rec.failed, "nothing is sent until the window closes")
	d.Flush()
	require.Len(t, rec.failed, 1)
	assert.Equal(t, int64(1), rec.failed[0].ID)
	assert.Empty(t, rec.alerts)
}

func TestFailureDigest_ManyFailuresBecomeOneAlert(t *testing.T) {
	rec := &recorder{}
	d := NewFailureDigest(rec, time.Hour, time.Hour, "https://grabarr.example/jobs?status=failed")
	for i := 0; i < 57; i++ {
		require.NoError(t, d.NotifyJobFailed(failedJob(int64(i), fmt.Sprintf("/s/alice/19033-h/img%02d.jpg", i), "rsync permanent failure (exit 23)")))
	}
	require.NoError(t, d.NotifyJobFailed(failedJob(100, "/s/alice/alice.epub", "rsync permanent failure (exit 23)")))
	require.NoError(t, d.NotifyJobFailed(failedJob(101, "/s/Other/x.mkv", "local disk full")))
	d.Flush()

	assert.Empty(t, rec.failed, "no per-job alerts")
	require.Len(t, rec.alerts, 1)
	assert.Equal(t, "Grabarr: 59 copies failed|57 in 19033-h\n1 in Other\n1 in alice\n\n"+
		"Error: rsync permanent failure (exit 23) (58 of 59)\n\nhttps://grabarr.example/jobs?status=failed|1", rec.alerts[0])

	d.Flush()
	assert.Len(t, rec.alerts, 1, "flushing again sends nothing new")
}

func TestFailureDigest_WindowClosesOnItsOwn(t *testing.T) {
	rec := &recorder{}
	d := NewFailureDigest(rec, 20*time.Millisecond, time.Second, "")
	require.NoError(t, d.NotifyJobFailed(failedJob(1, "/s/A/a.mkv", "x")))
	require.NoError(t, d.NotifyJobFailed(failedJob(2, "/s/A/b.mkv", "x")))

	require.Eventually(t, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.alerts) == 1
	}, time.Second, 5*time.Millisecond)
	assert.Contains(t, rec.alerts[0], "2 copies failed")
}

// Steady failures can't hold an alert back forever: past maxWait the window
// stops extending.
func TestFailureDigest_MaxWaitCapsTheWindow(t *testing.T) {
	rec := &recorder{}
	d := NewFailureDigest(rec, time.Minute, 3*time.Minute, "")
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return clock }

	require.NoError(t, d.NotifyJobFailed(failedJob(1, "/s/A/a.mkv", "x")))
	clock = clock.Add(90 * time.Second)
	require.NoError(t, d.NotifyJobFailed(failedJob(2, "/s/A/b.mkv", "x")))
	assert.True(t, d.opened.Equal(clock.Add(-90*time.Second)), "the window keeps its opening time")

	clock = clock.Add(150 * time.Second) // 4 min after opening: no more extending
	d.mu.Lock()
	extendable := clock.Sub(d.opened)+d.window <= d.maxWait
	d.mu.Unlock()
	assert.False(t, extendable)
	d.Flush()
}

func TestFailureDigest_PassesOtherAlertsThrough(t *testing.T) {
	rec := &recorder{}
	d := NewFailureDigest(rec, time.Hour, time.Hour, "")
	require.NoError(t, d.NotifySystemAlert("Service Started", "hi", 0))
	assert.Equal(t, []string{"Service Started|hi|0"}, rec.alerts)
	assert.True(t, d.IsEnabled())
}
