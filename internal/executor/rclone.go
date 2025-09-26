package executor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/models"
	"grabarr/internal/queue"
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

	// Prepare rclone command
	cmd, err := r.prepareCommand(job)
	if err != nil {
		return fmt.Errorf("failed to prepare rclone command: %w", err)
	}

	return r.executeCommand(ctx, cmd, job)
}

func (r *RCloneExecutor) executeCommand(ctx context.Context, cmd *exec.Cmd, job *models.Job) error {
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
		cmd.Process.Kill()
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("rclone execution failed: %w", err)
		}
		return nil
	}
}

func (r *RCloneExecutor) prepareCommand(job *models.Job) (*exec.Cmd, error) {
	rcloneConfig := r.config.GetRClone()

	// Build source path
	sourcePath := fmt.Sprintf("%s:%s", rcloneConfig.RemoteName, job.RemotePath)

	// Use the local path from the job
	basePath := job.LocalPath

	// Build destination path
	destPath := filepath.Join(basePath, filepath.Base(job.RemotePath))

	// Determine command based on whether source is directory or file
	var command string
	if r.isRemoteDirectory(job.RemotePath) {
		command = "copy"
	} else {
		command = "copyto"
	}

	// Build base rclone arguments
	args := []string{
		command,
		sourcePath,
		destPath,
		"--config", rcloneConfig.ConfigFile,
		"--progress",
		"--stats", "1s",
		"--stats-one-line",
		"--transfers", "4",
		"--ignore-existing",
	}

	// Add directory-specific flags
	if command == "copy" {
		args = append(args, "--create-empty-src-dirs")
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

	cmd := exec.Command("rclone", args...)

	slog.Info("prepared rclone command",
		"job_id", job.ID,
		"command", command,
		"source", sourcePath,
		"dest", destPath,
		"args", args)

	return cmd, nil
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
		slog.Info("rclone output", "job_id", job.ID, "line", line)
	}

	if err := scanner.Err(); err != nil {
		slog.Error("error reading rclone stderr", "job_id", job.ID, "error", err)
	}
}

func (r *RCloneExecutor) parseProgressLine(line string, transferredRe, speedRe, etaRe, filesRe *regexp.Regexp) *models.JobProgress {
	progress := &models.JobProgress{}
	hasProgress := false

	// Parse transferred bytes and percentage
	if matches := transferredRe.FindStringSubmatch(line); len(matches) >= 4 {
		transferred := r.parseBytes(matches[1])
		total := r.parseBytes(matches[2])
		percentage, _ := strconv.Atoi(matches[3])

		progress.TransferredBytes = transferred
		progress.TotalBytes = total
		progress.Percentage = float64(percentage)
		hasProgress = true
	}

	// Parse transfer speed
	if matches := speedRe.FindStringSubmatch(line); len(matches) >= 2 {
		speed := r.parseBytes(matches[1])
		progress.TransferSpeed = speed
		hasProgress = true
	}

	// Parse file counts
	if matches := filesRe.FindStringSubmatch(line); len(matches) >= 4 {
		completed, _ := strconv.Atoi(matches[1])
		total, _ := strconv.Atoi(matches[2])

		progress.FilesCompleted = completed
		progress.FilesTotal = total
		hasProgress = true
	}

	if hasProgress {
		return progress
	}
	return nil
}

func (r *RCloneExecutor) parseBytes(s string) int64 {
	s = strings.TrimSpace(s)

	// Handle different units
	multiplier := int64(1)
	if strings.HasSuffix(s, "KiB") || strings.HasSuffix(s, "KB") {
		multiplier = 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "KiB"), "KB")
	} else if strings.HasSuffix(s, "MiB") || strings.HasSuffix(s, "MB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "MiB"), "MB")
	} else if strings.HasSuffix(s, "GiB") || strings.HasSuffix(s, "GB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "GiB"), "GB")
	} else if strings.HasSuffix(s, "TiB") || strings.HasSuffix(s, "TB") {
		multiplier = 1024 * 1024 * 1024 * 1024
		s = strings.TrimSuffix(strings.TrimSuffix(s, "TiB"), "TB")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}

	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}

	return int64(value * float64(multiplier))
}

func (r *RCloneExecutor) GetProgressChannel() <-chan models.JobProgress {
	return r.progressChan
}

func (r *RCloneExecutor) CanExecute() bool {
	// Check if we have available resources
	return r.monitor.CanScheduleJob()
}

// isRemoteDirectory checks if the remote path is a directory using rclone lsf
func (r *RCloneExecutor) isRemoteDirectory(remotePath string) bool {
	rcloneConfig := r.config.GetRClone()

	// Build full remote path
	fullRemotePath := fmt.Sprintf("%s:%s", rcloneConfig.RemoteName, remotePath)

	slog.Info("checking if remote path is directory",
		"remote_path", remotePath,
		"full_remote_path", fullRemotePath)

	// Use rclone lsf with --dirs-only to check if path can be listed as a directory
	// This is more efficient and reliable than listing all contents
	cmd := exec.Command("rclone", "lsf", "--dirs-only", "--max-depth", "0",
		fullRemotePath, "--config", rcloneConfig.ConfigFile)

	// If the command succeeds, the path is a directory
	err := cmd.Run()
	isDirectory := err == nil

	slog.Info("directory check result",
		"full_remote_path", fullRemotePath,
		"is_directory", isDirectory,
		"method", "lsf --dirs-only --max-depth 0")

	return isDirectory
}