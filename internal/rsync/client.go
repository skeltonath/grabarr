package rsync

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"grabarr/internal/models"
)

// Client represents an rsync client for transferring files via SSH
type Client struct {
	sshHost    string
	sshUser    string
	sshKeyFile string
}

// NewClient creates a new rsync client
func NewClient(sshHost, sshUser, sshKeyFile string) *Client {
	return &Client{
		sshHost:    sshHost,
		sshUser:    sshUser,
		sshKeyFile: sshKeyFile,
	}
}

// Transfer represents a running rsync transfer
type Transfer struct {
	cmd          *exec.Cmd
	progressChan chan *models.JobProgress
	doneChan     chan error
	cancel       context.CancelFunc
}

// Copy starts an rsync transfer in the background
func (c *Client) Copy(ctx context.Context, remotePath, localPath string) (*Transfer, error) {
	// Build rsync command
	// rsync -avz --progress -e 'ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 -o ServerAliveInterval=60 -i /key' user@host:/remote /local
	sshCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 -o ServerAliveInterval=60 -i %s", c.sshKeyFile)
	remoteSource := fmt.Sprintf("%s@%s:%s", c.sshUser, c.sshHost, remotePath)

	cmdCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdCtx, "rsync", "-avz", "--progress", "-e", sshCmd, remoteSource, localPath)

	// Get stdout pipe for progress parsing
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// Get stderr pipe for error messages
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start rsync: %w", err)
	}

	transfer := &Transfer{
		cmd:          cmd,
		progressChan: make(chan *models.JobProgress, 10),
		doneChan:     make(chan error, 1),
		cancel:       cancel,
	}

	// Start goroutine to parse progress
	go transfer.parseProgress(stdout, stderr)

	// Start goroutine to wait for completion
	go func() {
		err := cmd.Wait()
		transfer.doneChan <- err
		close(transfer.progressChan)
		close(transfer.doneChan)
	}()

	return transfer, nil
}

// ProgressChan returns the channel for receiving progress updates
func (t *Transfer) ProgressChan() <-chan *models.JobProgress {
	return t.progressChan
}

// Done returns a channel that will receive the final error (or nil on success)
func (t *Transfer) Done() <-chan error {
	return t.doneChan
}

// Stop cancels the transfer
func (t *Transfer) Stop() {
	t.cancel()
}

// parseProgress parses rsync progress output and sends updates to the progress channel
func (t *Transfer) parseProgress(stdout, stderr io.Reader) {
	// Regex to parse rsync progress line
	// Example: "  8,745,341,265  21%   10.26MB/s    0:51:13"
	// Note: rsync uses variable whitespace (2+ spaces between fields)
	progressRegex := regexp.MustCompile(`([\d,]+)\s+(\d+)%\s+([\d.]+)([KMG])B/s\s+(\d+):(\d+):(\d+)`)

	// rsync uses \r (carriage return) to update progress on the same line
	// We need to split on both \r and \n
	scanner := bufio.NewScanner(stdout)
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		// Look for \r or \n
		if i := strings.IndexAny(string(data), "\r\n"); i >= 0 {
			// Skip the delimiter
			return i + 1, data[0:i], nil
		}
		// If we're at EOF, return what we have
		if atEOF {
			return len(data), data, nil
		}
		// Request more data
		return 0, nil, nil
	})

	var lastProgress *models.JobProgress

	for scanner.Scan() {
		line := scanner.Text()

		// Try to parse progress line
		matches := progressRegex.FindStringSubmatch(line)
		if len(matches) == 8 {
			// Parse transferred bytes
			bytesStr := strings.ReplaceAll(matches[1], ",", "")
			bytes, err := strconv.ParseInt(bytesStr, 10, 64)
			if err != nil {
				continue
			}

			// Parse percentage
			percentage, err := strconv.Atoi(matches[2])
			if err != nil {
				continue
			}

			// Parse speed
			speedVal, err := strconv.ParseFloat(matches[3], 64)
			if err != nil {
				continue
			}

			// Convert speed to bytes/sec
			speedUnit := matches[4]
			var speed float64
			switch speedUnit {
			case "K":
				speed = speedVal * 1024
			case "M":
				speed = speedVal * 1024 * 1024
			case "G":
				speed = speedVal * 1024 * 1024 * 1024
			}

			// Parse ETA
			hours, _ := strconv.Atoi(matches[5])
			minutes, _ := strconv.Atoi(matches[6])
			seconds, _ := strconv.Atoi(matches[7])
			etaDuration := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second
			eta := time.Now().Add(etaDuration)

			progress := &models.JobProgress{
				Percentage:       float64(percentage),
				TransferredBytes: bytes,
				TransferSpeed:    int64(speed),
				ETA:              &eta,
				LastUpdateTime:   time.Now(),
			}

			lastProgress = progress

			// Send progress update (non-blocking)
			select {
			case t.progressChan <- progress:
			default:
				// Channel full, skip this update
			}
		}
	}

	// If we have progress, send final 100% update
	if lastProgress != nil && lastProgress.Percentage < 100 {
		finalProgress := &models.JobProgress{
			Percentage:       100,
			TransferredBytes: lastProgress.TransferredBytes,
			TransferSpeed:    0,
			LastUpdateTime:   time.Now(),
		}
		select {
		case t.progressChan <- finalProgress:
		default:
		}
	}

	// Read any stderr output for errors
	stderrScanner := bufio.NewScanner(stderr)
	for stderrScanner.Scan() {
		// Log errors but don't send as progress
		// Errors will be handled via cmd.Wait() in the done channel
		_ = stderrScanner.Text()
	}
}
