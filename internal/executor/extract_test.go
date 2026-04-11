package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"grabarr/internal/archive"
	"grabarr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupArchiveFiles(t *testing.T) {
	// Create a temp directory with archive and non-archive files
	dir := t.TempDir()

	// Create fake archive files for group "Movie"
	archiveFiles := []string{"Movie.rar", "Movie.r00", "Movie.r01", "Movie.r02"}
	for _, f := range archiveFiles {
		require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("archive data"), 0644))
	}

	// Create a non-archive file (should NOT be deleted)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Movie.mkv"), []byte("video"), 0644))

	// Create an archive file for a DIFFERENT group (should NOT be deleted)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Other.rar"), []byte("other archive"), 0644))

	job := &models.Job{
		LocalPath: dir,
		Metadata: models.JobMetadata{
			ExtraFields: map[string]interface{}{
				"job_type":      "extraction",
				"archive_group": archive.GroupKey(filepath.Join(dir, "Movie.rar")),
			},
		},
	}

	err := cleanupArchiveFiles(job)
	require.NoError(t, err)

	// Archive files for "Movie" should be gone
	for _, f := range archiveFiles {
		_, err := os.Stat(filepath.Join(dir, f))
		assert.True(t, os.IsNotExist(err), "expected %s to be deleted", f)
	}

	// Non-archive file should still exist
	_, err = os.Stat(filepath.Join(dir, "Movie.mkv"))
	assert.NoError(t, err, "Movie.mkv should not be deleted")

	// Other group's archive should still exist
	_, err = os.Stat(filepath.Join(dir, "Other.rar"))
	assert.NoError(t, err, "Other.rar should not be deleted")
}

func TestCleanupArchiveFiles_NoGroup(t *testing.T) {
	job := &models.Job{
		LocalPath: t.TempDir(),
		Metadata:  models.JobMetadata{},
	}

	// Should be a no-op, not an error
	err := cleanupArchiveFiles(job)
	assert.NoError(t, err)
}

func TestIsExtractionToolMissing(t *testing.T) {
	t.Run("missing tool", func(t *testing.T) {
		_, err := exec.LookPath("nonexistent_tool_xyz")
		if err != nil {
			// This confirms tools can be missing - test the actual function
			execErr := &exec.Error{Name: "unrar", Err: exec.ErrNotFound}
			assert.True(t, isExtractionToolMissing(execErr))
		}
	})

	t.Run("other error", func(t *testing.T) {
		assert.False(t, isExtractionToolMissing(assert.AnError))
	})
}
