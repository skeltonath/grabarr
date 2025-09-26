package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/models"
	"grabarr/internal/queue"
	"grabarr/internal/sanitizer"
)

type RCloneExecutor struct {
	config       *config.Config
	monitor      queue.ResourceChecker
	progressChan chan models.JobProgress
}

func NewRCloneExecutor(cfg *config.Config, monitor queue.ResourceChecker) *RCloneExecutor {
	return &RCloneExecutor{
		config:       cfg,
		monitor:      monitor,
		progressChan: make(chan models.JobProgress, 100),
	}
}

func (r *RCloneExecutor) Execute(ctx context.Context, job *models.Job) error {
	slog.Info("starting rclone execution", "job_id", job.ID, "name", job.Name)

	// Prepare rclone command (may create symlink)
	cmd, symlinkPath, err := r.prepareCommand(job)
	if err != nil {
		return fmt.Errorf("failed to prepare rclone command: %w", err)
	}

	// Ensure symlink cleanup happens regardless of outcome
	defer func() {
		if symlinkPath != "" {
			r.removeSymlink(symlinkPath)
		}
	}()

	// Set up progress monitoring
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start rclone: %w", err)
	}

	// Monitor progress in separate goroutines
	progressCtx, progressCancel := context.WithCancel(ctx)
	defer progressCancel()

	go r.monitorProgress(progressCtx, stdout, job)
	go r.monitorErrors(progressCtx, stderr, job)

	// Wait for command to complete or context cancellation
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		slog.Info("rclone execution cancelled", "job_id", job.ID)
		// Kill the process
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("execution cancelled")
	case err := <-done:
		if err != nil {
			return fmt.Errorf("rclone execution failed: %w", err)
		}
	}

	slog.Info("rclone execution completed", "job_id", job.ID)
	return nil
}

func (r *RCloneExecutor) CanExecute() bool {
	return r.monitor.CanScheduleJob()
}

func (r *RCloneExecutor) prepareCommand(job *models.Job) (*exec.Cmd, string, error) {
	rcloneConfig := r.config.GetRClone()

	var symlinkPath string // Track symlink for cleanup
	actualRemotePath := job.RemotePath

	// Handle filename sanitization if enabled
	if rcloneConfig.FilenameSanitization && sanitizer.NeedsSanitization(job.RemotePath) {
		cleanPath, _ := sanitizer.SanitizeForSymlink(job.RemotePath)
		symlinkPath = cleanPath

		slog.Info("path needs sanitization",
			"job_id", job.ID,
			"original", job.RemotePath,
			"cleaned", cleanPath)

		// Create symlink on remote host
		originalPath := job.RemotePath
		if err := r.createSymlink(originalPath, symlinkPath); err != nil {
			return nil, "", fmt.Errorf("failed to create symlink: %w", err)
		}

		// Use the clean symlink path for download
		actualRemotePath = symlinkPath
	}

	// Build source path using the actual remote path (original or symlink)
	sourcePath := fmt.Sprintf("%s:%s%s", rcloneConfig.RemoteName, rcloneConfig.RemotePath, actualRemotePath)

	var destPath string
	if job.LocalPath != "" {
		destPath = job.LocalPath
	} else {
		// Use configured downloads local path + job name
		destPath = filepath.Join(r.config.GetDownloads().LocalPath, job.Name)
	}

	// Check if remote path is a directory and preserve structure
	if r.isRemoteDirectory(actualRemotePath) {
		dirName := filepath.Base(actualRemotePath)
		// If destPath doesn't already end with the directory name, add it
		if filepath.Base(destPath) != dirName {
			destPath = filepath.Join(destPath, dirName)
		}
		slog.Info("preserving directory structure",
			"job_id", job.ID,
			"remote_directory", actualRemotePath,
			"local_directory", destPath)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return nil, "", fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Build rclone arguments
	args := []string{
		"copy", // Use copy instead of sync for individual files/directories
		sourcePath,
		destPath,
		"--config", rcloneConfig.ConfigFile,
		"--progress", // Enable progress output
		"--stats", "1s", // Update stats every second
		"--stats-one-line", // One line stats for easier parsing
		"--create-empty-src-dirs", // Create empty source directories
		"--transfers", "4", // Use multiple transfers for better performance
	}

	// Add bandwidth limit if configured
	if rcloneConfig.BandwidthLimit != "" {
		args = append(args, "--bwlimit", rcloneConfig.BandwidthLimit)
	}

	// Add transfer timeout if configured
	if rcloneConfig.TransferTimeout > 0 {
		args = append(args, "--timeout", rcloneConfig.TransferTimeout.String())
	}

	// Add any additional arguments from job metadata
	if len(job.Metadata.RCloneArgs) > 0 {
		args = append(args, job.Metadata.RCloneArgs...)
	}

	// Add any additional arguments from config
	if len(rcloneConfig.AdditionalArgs) > 0 {
		args = append(args, rcloneConfig.AdditionalArgs...)
	}

	// Create command
	cmd := exec.Command("rclone", args...)
	cmd.Env = os.Environ()

	slog.Info("prepared rclone command",
		"job_id", job.ID,
		"source", sourcePath,
		"dest", destPath,
		"args", args,
		"symlink_path", symlinkPath,
	)

	return cmd, symlinkPath, nil
}

func (r *RCloneExecutor) monitorProgress(ctx context.Context, stdout io.ReadCloser, job *models.Job) {
	scanner := bufio.NewScanner(stdout)

	// Regular expressions for parsing rclone output
	// Match format: "46 MiB / 5.250 GiB, 1%, 6.346 MiB/s, ETA 13m59s"
	transferredRe := regexp.MustCompile(`([0-9.]+\s*[KMGT]?i?B)\s*/\s*([0-9.]+\s*[KMGT]?i?B),\s*([0-9]+)%`)
	speedRe := regexp.MustCompile(`([0-9.]+\s*[KMGT]?i?B/s)`)
	etaRe := regexp.MustCompile(`ETA\s+([0-9hms-]+)`)
	filesRe := regexp.MustCompile(`Transferred:\s+([0-9]+)\s*/\s*([0-9]+),\s*([0-9]+)%`)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		slog.Info("rclone output", "job_id", job.ID, "line", line)

		// Parse progress information
		progress := r.parseProgressLine(line, transferredRe, speedRe, etaRe, filesRe)
		if progress != nil {
			progress.LastUpdateTime = time.Now()

			// Update job progress
			job.UpdateProgress(*progress)

			// Send progress update (non-blocking)
			select {
			case r.progressChan <- *progress:
			default:
			}
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("error reading rclone stdout", "job_id", job.ID, "error", err)
	}
}

func (r *RCloneExecutor) monitorErrors(ctx context.Context, stderr io.ReadCloser, job *models.Job) {
	scanner := bufio.NewScanner(stderr)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		// Log errors and warnings
		if strings.Contains(line, "ERROR") {
			slog.Error("rclone error", "job_id", job.ID, "error", line)
		} else if strings.Contains(line, "WARN") {
			slog.Warn("rclone warning", "job_id", job.ID, "warning", line)
		} else {
			slog.Debug("rclone stderr", "job_id", job.ID, "line", line)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("error reading rclone stderr", "job_id", job.ID, "error", err)
	}
}

func (r *RCloneExecutor) parseProgressLine(line string, transferredRe, speedRe, etaRe, filesRe *regexp.Regexp) *models.JobProgress {
	progress := &models.JobProgress{}
	found := false

	// Parse transferred bytes and percentage
	if matches := transferredRe.FindStringSubmatch(line); matches != nil {
		transferred := r.parseSizeString(matches[1])
		total := r.parseSizeString(matches[2])
		percentage, _ := strconv.ParseFloat(matches[3], 64)

		progress.TransferredBytes = transferred
		progress.TotalBytes = total
		progress.Percentage = percentage
		found = true
	}

	// Parse transfer speed
	if matches := speedRe.FindStringSubmatch(line); matches != nil {
		speed := r.parseSpeedString(matches[1])
		progress.TransferSpeed = speed
		found = true
	}

	// Parse ETA
	if matches := etaRe.FindStringSubmatch(line); matches != nil {
		if eta := r.parseDurationString(matches[1]); eta > 0 {
			duration := models.Duration{Duration: eta}
			progress.ETA = &duration
			found = true
		}
	}

	// Parse file counts
	if matches := filesRe.FindStringSubmatch(line); matches != nil {
		completed, _ := strconv.Atoi(matches[1])
		total, _ := strconv.Atoi(matches[2])

		progress.FilesCompleted = completed
		progress.FilesTotal = total
		found = true
	}

	if found {
		return progress
	}

	return nil
}

func (r *RCloneExecutor) parseSizeString(sizeStr string) int64 {
	sizeStr = strings.TrimSpace(sizeStr)

	// Extract number and unit
	parts := strings.Fields(sizeStr)
	if len(parts) != 2 {
		// Try parsing as single string like "1.5GB" or "5.250GiB"
		re := regexp.MustCompile(`([0-9.]+)\s*([KMGT]?i?B)`)
		if matches := re.FindStringSubmatch(sizeStr); len(matches) == 3 {
			parts = []string{matches[1], matches[2]}
		} else {
			return 0
		}
	}

	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}

	unit := strings.ToUpper(parts[1])
	switch unit {
	case "B":
		return int64(value)
	case "KB", "KIB":
		return int64(value * 1024)
	case "MB", "MIB":
		return int64(value * 1024 * 1024)
	case "GB", "GIB":
		return int64(value * 1024 * 1024 * 1024)
	case "TB", "TIB":
		return int64(value * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(value) // Assume bytes
	}
}

func (r *RCloneExecutor) parseSpeedString(speedStr string) int64 {
	// Remove "/s" suffix and parse as size
	speedStr = strings.Replace(speedStr, "/s", "", 1)
	return r.parseSizeString(speedStr)
}

func (r *RCloneExecutor) parseDurationString(durationStr string) time.Duration {
	// Simple duration parser for formats like "1h23m45s", "23m45s", "45s"
	durationStr = strings.TrimSpace(durationStr)

	// Add zero values for missing units to make it parseable by time.ParseDuration
	if !strings.Contains(durationStr, "h") && !strings.Contains(durationStr, "m") && !strings.Contains(durationStr, "s") {
		// Assume seconds if no unit
		durationStr += "s"
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		slog.Debug("failed to parse duration", "duration", durationStr, "error", err)
		return 0
	}

	return duration
}

// GetProgressChannel returns a channel for receiving progress updates
func (r *RCloneExecutor) GetProgressChannel() <-chan models.JobProgress {
	return r.progressChan
}

// createSymlink creates a symlink on the remote host via SSH
func (r *RCloneExecutor) createSymlink(originalPath, symlinkPath string) error {
	// Get SSH credentials from environment
	host := os.Getenv("SEEDBOX_HOST")
	user := os.Getenv("SEEDBOX_USER")
	pass := os.Getenv("SEEDBOX_PASS")

	if host == "" || user == "" || pass == "" {
		return fmt.Errorf("missing SSH credentials for symlink creation")
	}

	// Create symlink command: ln -s "original path" "symlink path"
	sshCmd := fmt.Sprintf("ln -s %q %q", originalPath, symlinkPath)

	// Execute via sshpass
	cmd := exec.Command("sshpass", "-p", pass, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		fmt.Sprintf("%s@%s", user, host),
		sshCmd)

	slog.Debug("creating symlink", "original", originalPath, "symlink", symlinkPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create symlink: %w, output: %s", err, output)
	}

	slog.Info("symlink created successfully", "original", originalPath, "symlink", symlinkPath)
	return nil
}

// removeSymlink removes a symlink on the remote host via SSH
func (r *RCloneExecutor) removeSymlink(symlinkPath string) error {
	// Get SSH credentials from environment
	host := os.Getenv("SEEDBOX_HOST")
	user := os.Getenv("SEEDBOX_USER")
	pass := os.Getenv("SEEDBOX_PASS")

	if host == "" || user == "" || pass == "" {
		return fmt.Errorf("missing SSH credentials for symlink removal")
	}

	// Remove symlink command
	sshCmd := fmt.Sprintf("rm %q", symlinkPath)

	// Execute via sshpass
	cmd := exec.Command("sshpass", "-p", pass, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		fmt.Sprintf("%s@%s", user, host),
		sshCmd)

	slog.Debug("removing symlink", "path", symlinkPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("failed to remove symlink (non-critical)", "path", symlinkPath, "error", err, "output", string(output))
		// Don't return error - cleanup is best-effort
	} else {
		slog.Info("symlink removed successfully", "path", symlinkPath)
	}

	return nil
}

// isRemoteDirectory checks if the remote path is a directory using rclone lsf --dirs-only
func (r *RCloneExecutor) isRemoteDirectory(remotePath string) bool {
	rcloneConfig := r.config.GetRClone()

	// Build full remote path
	fullRemotePath := fmt.Sprintf("%s:%s%s", rcloneConfig.RemoteName, rcloneConfig.RemotePath, remotePath)

	// Use rclone lsf --dirs-only to check if path is a directory
	cmd := exec.Command("rclone", "lsf", "--dirs-only", fullRemotePath, "--config", rcloneConfig.ConfigFile)

	// If the command succeeds and returns output, it's a directory
	output, err := cmd.Output()
	if err != nil {
		// If lsf fails, assume it's a file
		return false
	}

	// If there's any output, the remote path contains directories (meaning it is a directory)
	return len(strings.TrimSpace(string(output))) > 0
}