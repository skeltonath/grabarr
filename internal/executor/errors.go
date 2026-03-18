package executor

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// PermanentError signals that retrying will not help — the job should fail immediately.
type PermanentError struct {
	Cause error
	Msg   string
}

func (e *PermanentError) Error() string { return fmt.Sprintf("%s: %v", e.Msg, e.Cause) }
func (e *PermanentError) Unwrap() error { return e.Cause }

// IsPermanent reports whether err (or any error in its chain) is a PermanentError.
func IsPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}

// classifyRsyncError wraps an rsync exit error as PermanentError when the exit code
// indicates a condition that won't be fixed by retrying.
func classifyRsyncError(err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err // not an exit error — treat as retryable
	}
	switch exitErr.ExitCode() {
	case 1, // syntax/usage error
		2,  // protocol incompatibility
		3,  // file selection error (source not found)
		4,  // unsupported action
		5,  // error starting client/server protocol
		6,  // daemon unable to append to log
		24: // source files vanished during transfer
		return &PermanentError{Cause: err, Msg: fmt.Sprintf("rsync permanent failure (exit %d)", exitErr.ExitCode())}
	default:
		// exit 10 (socket I/O), 11 (file I/O), 12 (protocol stream),
		// 14 (IPC crash), 20 (SIGINT), 23 (partial transfer), 255 (SSH error) → retryable
		return err
	}
}

// classifyRcloneError inspects the rclone daemon error message string and returns a
// PermanentError for conditions that cannot be fixed by retrying.
func classifyRcloneError(errMsg string) error {
	lower := strings.ToLower(errMsg)
	permanentPatterns := []string{
		"not found", "no such file", "no such directory",
		"permission denied", "access denied",
		"401", "403", "404",
	}
	for _, p := range permanentPatterns {
		if strings.Contains(lower, p) {
			return &PermanentError{
				Cause: fmt.Errorf("%s", errMsg),
				Msg:   "rclone permanent failure",
			}
		}
	}
	return fmt.Errorf("rclone job failed: %s", errMsg)
}
