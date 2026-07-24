package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"grabarr/internal/archive"
	"grabarr/internal/models"
)

// extractRunner runs the extraction tool for archivePath, writing output into
// destDir. It is a field on RsyncExecutor so tests can exercise the staging and
// promotion logic without a real unrar binary.
type extractRunner func(ctx context.Context, archivePath, destDir string) ([]byte, error)

// executeExtraction handles extraction jobs by running unrar/unzip on the archive
// and optionally cleaning up archive files afterward.
//
// Extraction goes into a staging directory that is promoted only on success.
// unrar refuses to overwrite an existing file, so a failed attempt that wrote a
// truncated payload straight into the destination would be silently kept by the
// retry — staging keeps every attempt starting from a clean slate.
func (r *RsyncExecutor) executeExtraction(ctx context.Context, job *models.Job) error {
	archivePath := job.RemotePath // reused field: stores the local path to the first-part archive
	destDir := job.LocalPath

	slog.Info("starting archive extraction",
		"job_id", job.ID,
		"archive", archivePath,
		"dest", destDir)

	ext := strings.ToLower(filepath.Ext(archivePath))
	if ext != ".rar" && ext != ".zip" && !rNNExtRegex.MatchString(ext) {
		return &PermanentError{Msg: fmt.Sprintf("unsupported archive type: %s", ext)}
	}

	stagingDir := filepath.Join(destDir, fmt.Sprintf(".grabarr-extract-%d", job.ID))
	if err := os.RemoveAll(stagingDir); err != nil {
		return fmt.Errorf("clear staging dir %s: %w", stagingDir, err)
	}
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return fmt.Errorf("create staging dir %s: %w", stagingDir, err)
	}
	// Removed on every path: on failure it discards partial output, on success
	// it is already empty because the contents were promoted.
	defer func() {
		if err := os.RemoveAll(stagingDir); err != nil {
			slog.Error("failed to remove staging dir", "job_id", job.ID, "dir", stagingDir, "error", err)
		}
	}()

	run := r.extractFn
	if run == nil {
		run = runExtractCommand
	}

	output, err := run(ctx, archivePath, stagingDir)
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

		// A missing volume almost always means the remaining parts are still
		// in flight rather than that the archive is bad, so let it retry.
		if isRetryableExtractionOutput(string(output)) {
			return fmt.Errorf("extraction failed (retryable): %w: %s", err, string(output))
		}

		// Everything else (corrupt archive, bad format, wrong password) is permanent.
		return &PermanentError{Msg: fmt.Sprintf("extraction failed: %v: %s", err, string(output))}
	}

	if err := promoteExtracted(stagingDir, destDir); err != nil {
		return fmt.Errorf("promote extracted files: %w", err)
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

// rNNExtRegex matches old-style multi-part RAR extensions (.r00-.r99), with the
// leading dot as returned by filepath.Ext.
var rNNExtRegex = regexp.MustCompile(`(?i)^\.r\d{2}$`)

// runExtractCommand is the production extractRunner.
func runExtractCommand(ctx context.Context, archivePath, destDir string) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(archivePath))

	var cmd *exec.Cmd
	switch {
	case ext == ".rar" || rNNExtRegex.MatchString(ext):
		// Prefer unrar for RAR files (best compatibility with all RAR versions).
		// -o+ overwrites: the destination is a fresh staging dir, and a retry
		// must be free to rewrite whatever a failed attempt left there.
		if _, err := exec.LookPath("unrar"); err == nil {
			cmd = exec.CommandContext(ctx, "unrar", "x", "-o+", archivePath, destDir)
		} else {
			cmd = exec.CommandContext(ctx, "7z", "x", "-aoa", "-o"+destDir, archivePath)
		}
	case ext == ".zip":
		cmd = exec.CommandContext(ctx, "7z", "x", "-aoa", "-o"+destDir, archivePath)
	default:
		return nil, &PermanentError{Msg: fmt.Sprintf("unsupported archive type: %s", ext)}
	}

	return cmd.CombinedOutput()
}

// promoteExtracted moves everything from stagingDir into destDir.
func promoteExtracted(stagingDir, destDir string) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return fmt.Errorf("read staging dir %s: %w", stagingDir, err)
	}

	for _, entry := range entries {
		src := filepath.Join(stagingDir, entry.Name())
		dst := filepath.Join(destDir, entry.Name())

		// Same filesystem, so this is a rename rather than a copy even for a
		// multi-gigabyte payload.
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s to %s: %w", src, dst, err)
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
