package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"grabarr/internal/config"
	"grabarr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractionTestExecutor builds an executor whose extraction step is stubbed.
func extractionTestExecutor(t *testing.T, runner extractRunner) *RsyncExecutor {
	t.Helper()
	return &RsyncExecutor{
		config:    &config.Config{},
		extractFn: runner,
	}
}

func extractionJob(t *testing.T, destDir string) *models.Job {
	t.Helper()
	return &models.Job{
		ID:         42,
		Name:       "extract: movie",
		RemotePath: filepath.Join(destDir, "movie.rar"),
		LocalPath:  destDir,
		Metadata: models.JobMetadata{
			ExtraFields: map[string]interface{}{
				"job_type":      "extraction",
				"archive_group": filepath.Join(destDir, "movie"),
			},
		},
	}
}

func TestExecuteExtractionPromotesOutputOnSuccess(t *testing.T) {
	destDir := t.TempDir()

	r := extractionTestExecutor(t, func(_ context.Context, _, stagingDir string) ([]byte, error) {
		// Stand in for unrar writing the payload into the staging directory.
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "movie.mkv"), []byte("video"), 0644))
		return []byte("ok"), nil
	})

	err := r.executeExtraction(context.Background(), extractionJob(t, destDir))
	require.NoError(t, err)

	body, err := os.ReadFile(filepath.Join(destDir, "movie.mkv"))
	require.NoError(t, err, "extracted file should be promoted into the destination")
	assert.Equal(t, "video", string(body))
}

// The retry added for missing volumes only helps if a failed attempt leaves
// nothing behind: unrar will not overwrite an existing file, so a truncated
// payload from attempt 1 would otherwise be silently kept by attempt 2.
func TestExecuteExtractionLeavesNoPartialOutputOnFailure(t *testing.T) {
	destDir := t.TempDir()

	r := extractionTestExecutor(t, func(_ context.Context, _, stagingDir string) ([]byte, error) {
		// A truncated payload, as unrar leaves when it runs out of volumes.
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "movie.mkv"), []byte("trunc"), 0644))
		return []byte("Cannot find volume movie.r06"), errors.New("exit status 3")
	})

	err := r.executeExtraction(context.Background(), extractionJob(t, destDir))
	require.Error(t, err)
	assert.False(t, IsPermanent(err), "a missing volume should stay retryable")

	entries, readErr := os.ReadDir(destDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "a failed extraction must not leave partial output behind")
}

func TestExecuteExtractionRemovesStagingDirOnSuccess(t *testing.T) {
	destDir := t.TempDir()

	r := extractionTestExecutor(t, func(_ context.Context, _, stagingDir string) ([]byte, error) {
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "movie.mkv"), []byte("video"), 0644))
		return nil, nil
	})

	require.NoError(t, r.executeExtraction(context.Background(), extractionJob(t, destDir)))

	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, e.IsDir(), "no staging directory should survive: found %s", e.Name())
	}
}

// A retry must be able to write the payload the failed attempt could not.
func TestExecuteExtractionRetrySucceedsAfterFailure(t *testing.T) {
	destDir := t.TempDir()
	attempt := 0

	r := extractionTestExecutor(t, func(_ context.Context, _, stagingDir string) ([]byte, error) {
		attempt++
		if attempt == 1 {
			require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "movie.mkv"), []byte("trunc"), 0644))
			return []byte("Cannot find volume movie.r06"), errors.New("exit status 3")
		}
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "movie.mkv"), []byte("complete video"), 0644))
		return nil, nil
	})

	job := extractionJob(t, destDir)
	require.Error(t, r.executeExtraction(context.Background(), job))
	require.NoError(t, r.executeExtraction(context.Background(), job))

	body, err := os.ReadFile(filepath.Join(destDir, "movie.mkv"))
	require.NoError(t, err)
	assert.Equal(t, "complete video", string(body),
		"the retry's full payload must replace what the failed attempt produced")
}

func TestExecuteExtractionRejectsUnsupportedArchiveType(t *testing.T) {
	destDir := t.TempDir()

	r := extractionTestExecutor(t, func(context.Context, string, string) ([]byte, error) {
		t.Fatal("extraction should not run for an unsupported type")
		return nil, nil
	})

	job := extractionJob(t, destDir)
	job.RemotePath = filepath.Join(destDir, "movie.tar.gz")

	err := r.executeExtraction(context.Background(), job)
	require.Error(t, err)
	assert.True(t, IsPermanent(err), "an unsupported archive type cannot be fixed by retrying")
}
