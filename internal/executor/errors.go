package executor

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/skeltonath/grabarr/internal/rsync"
)

// PermanentError signals that retrying will not help — the job should fail immediately.
type PermanentError struct {
	Cause error
	Msg   string
	// SourceGone means the file left the seedbox mid-copy (the torrent was
	// removed). That's expected housekeeping, not a failure worth an alert.
	SourceGone bool
}

func (e *PermanentError) Error() string { return fmt.Sprintf("%s: %v", e.Msg, e.Cause) }
func (e *PermanentError) Unwrap() error { return e.Cause }

// IsPermanent reports whether err (or any error in its chain) is a PermanentError.
func IsPermanent(err error) bool {
	var p *PermanentError
	return errors.As(err, &p)
}

// IsSourceGone reports whether err says the source file was removed from the
// seedbox before the copy finished.
func IsSourceGone(err error) bool {
	var p *PermanentError
	return errors.As(err, &p) && p.SourceGone
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
		2, // protocol incompatibility
		3, // file selection error (source not found)
		4, // unsupported action
		5, // error starting client/server protocol
		6: // daemon unable to append to log
		return &PermanentError{Cause: err, Msg: fmt.Sprintf("rsync permanent failure (exit %d)", exitErr.ExitCode())}
	case 24: // source files vanished during transfer
		return &PermanentError{Cause: err, Msg: "removed from the seedbox before the copy finished", SourceGone: true}
	case 23: // partial transfer — retryable unless stderr says a file doesn't exist
		var te *rsync.TransferError
		if errors.As(err, &te) && strings.Contains(strings.ToLower(te.Stderr), "no such file or directory") {
			// "[sender]" lines are about the seedbox side: the source is gone.
			// Anything else (say, a missing local folder) is a real failure.
			if strings.Contains(te.Stderr, "[sender]") {
				return &PermanentError{Cause: err, Msg: "removed from the seedbox before the copy finished", SourceGone: true}
			}
			return &PermanentError{Cause: err, Msg: fmt.Sprintf("rsync permanent failure (exit %d)", exitErr.ExitCode())}
		}
		return err
	default:
		// exit 10 (socket I/O), 11 (file I/O), 12 (protocol stream),
		// 14 (IPC crash), 20 (SIGINT), 255 (SSH error) → retryable
		return err
	}
}
