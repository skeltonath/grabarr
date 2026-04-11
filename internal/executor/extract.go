package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"grabarr/internal/archive"
	"grabarr/internal/models"
)

// executeExtraction handles extraction jobs by running unrar/unzip on the archive
// and optionally cleaning up archive files afterward.
func (r *RsyncExecutor) executeExtraction(ctx context.Context, job *models.Job) error {
	archivePath := job.RemotePath // reused field: stores the local path to the first-part archive
	destDir := job.LocalPath

	slog.Info("starting archive extraction",
		"job_id", job.ID,
		"archive", archivePath,
		"dest", destDir)

	// Determine extraction command based on file type
	ext := strings.ToLower(filepath.Ext(archivePath))

	var cmd *exec.Cmd
	switch {
	case ext == ".rar" || strings.HasPrefix(ext, ".r"):
		// Prefer unrar for RAR files (best compatibility with all RAR versions).
		// -o- means don't overwrite existing files.
		if _, err := exec.LookPath("unrar"); err == nil {
			cmd = exec.CommandContext(ctx, "unrar", "x", "-o-", archivePath, destDir)
		} else {
			cmd = exec.CommandContext(ctx, "7z", "x", "-aos", "-o"+destDir, archivePath)
		}
	case ext == ".zip":
		cmd = exec.CommandContext(ctx, "7z", "x", "-aos", "-o"+destDir, archivePath)
	default:
		return &PermanentError{Msg: fmt.Sprintf("unsupported archive type: %s", ext)}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("extraction failed",
			"job_id", job.ID,
			"archive", archivePath,
			"error", err,
			"output", string(output))

		// Check if the error is due to a missing tool
		if isExtractionToolMissing(err) {
			return &PermanentError{Msg: fmt.Sprintf("extraction tool not found: %v", err)}
		}

		// Most extraction errors are permanent (corrupt archive, bad format, etc.)
		return &PermanentError{Msg: fmt.Sprintf("extraction failed: %v: %s", err, string(output))}
	}

	slog.Info("extraction completed successfully", "job_id", job.ID, "archive", archivePath)

	// Clean up archive files if configured
	if r.config.GetExtraction().CleanupArchives {
		if err := cleanupArchiveFiles(job); err != nil {
			slog.Error("failed to cleanup archive files", "job_id", job.ID, "error", err)
			// Don't fail the job for cleanup errors — extraction succeeded
		}
	}

	return nil
}

// cleanupArchiveFiles deletes all archive files belonging to the same archive group
// from the local directory after successful extraction.
func cleanupArchiveFiles(job *models.Job) error {
	dir := job.LocalPath
	group := job.ArchiveGroup()
	if group == "" {
		return nil
	}

	groupBase := filepath.Base(group)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	var deleted int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !archive.IsArchive(name) {
			continue
		}
		// Check if this file belongs to the same archive group
		fileGroup := filepath.Base(archive.GroupKey(filepath.Join(dir, name)))
		if fileGroup != groupBase {
			continue
		}

		fullPath := filepath.Join(dir, name)
		if err := os.Remove(fullPath); err != nil {
			slog.Error("failed to delete archive file", "path", fullPath, "error", err)
			continue
		}
		deleted++
		slog.Debug("deleted archive file", "path", fullPath)
	}

	slog.Info("archive cleanup complete", "dir", dir, "deleted", deleted)
	return nil
}

func isExtractionToolMissing(err error) bool {
	if exitErr, ok := err.(*exec.Error); ok {
		return exitErr.Err == exec.ErrNotFound
	}
	return false
}
