package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/skeltonath/grabarr/internal/models"
	"github.com/skeltonath/grabarr/internal/repository"
)

func TestSeed(t *testing.T) {
	db := filepath.Join(t.TempDir(), "preview.db")
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, run(db, false, now))

	repo, err := repository.New(db)
	require.NoError(t, err)

	summary, err := repo.GetJobSummary()
	require.NoError(t, err)
	// Every state the UI renders differently is present.
	assert.Positive(t, summary.QueuedJobs)
	assert.Positive(t, summary.RunningJobs)
	assert.Positive(t, summary.CompletedJobs)
	assert.Positive(t, summary.FailedJobs)
	assert.Positive(t, summary.CancelledJobs)

	for _, status := range []models.FileStatus{
		models.FileStatusOnSeedbox, models.FileStatusQueued, models.FileStatusDownloading,
		models.FileStatusDownloaded, models.FileStatusIgnored,
	} {
		n, err := repo.CountRemoteFiles(models.RemoteFileFilter{Status: status})
		require.NoError(t, err)
		assert.Positive(t, n, "remote files with status %s", status)
	}
	for _, root := range []string{videoRoot, booksRoot} {
		n, err := repo.CountRemoteFiles(models.RemoteFileFilter{WatchedPath: root})
		require.NoError(t, err)
		assert.Positive(t, n, "files under %s", root)
	}

	running, err := repo.GetJobs(models.JobFilter{Status: []models.JobStatus{models.JobStatusRunning}})
	require.NoError(t, err)
	for _, j := range running {
		assert.Greater(t, j.Progress.Percentage, 0.0, j.Name)
		assert.NotZero(t, j.TransferSpeed, j.Name)
	}
	jobs, err := repo.GetJobs(models.JobFilter{})
	require.NoError(t, err)
	for _, j := range jobs {
		assert.True(t, j.CreatedAt.Before(now), "%s is backdated", j.Name)
	}
	require.NoError(t, repo.Close())

	// Seeding again needs -reset rather than doubling everything.
	assert.ErrorContains(t, run(db, false, now), "pass -reset")
	require.NoError(t, run(db, true, now))
}
