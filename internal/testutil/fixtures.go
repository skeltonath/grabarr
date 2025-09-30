package testutil

import (
	"grabarr/internal/models"
	"time"
)

// CreateTestJob creates a test job with default values
func CreateTestJob(overrides ...func(*models.Job)) *models.Job {
	job := &models.Job{
		Name:       "test-job",
		RemotePath: "/remote/path",
		LocalPath:  "/local/path",
		Status:     models.JobStatusQueued,
		Priority:   0,
		MaxRetries: 3,
		Progress: models.JobProgress{
			LastUpdateTime: time.Now(),
		},
		Metadata: models.JobMetadata{
			Category: "test",
		},
	}

	for _, override := range overrides {
		override(job)
	}

	return job
}

// CreateTestSyncJob creates a test sync job with default values
func CreateTestSyncJob(overrides ...func(*models.SyncJob)) *models.SyncJob {
	syncJob := &models.SyncJob{
		RemotePath: "/remote/sync/path",
		LocalPath:  "/local/sync/path",
		Status:     models.SyncStatusQueued,
		Progress: models.SyncProgress{
			LastUpdateTime: time.Now(),
		},
		Stats: models.SyncStats{},
	}

	for _, override := range overrides {
		override(syncJob)
	}

	return syncJob
}
