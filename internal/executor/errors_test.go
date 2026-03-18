package executor

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPermanent(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "PermanentError direct",
			err:      &PermanentError{Msg: "bad path", Cause: errors.New("cause")},
			expected: true,
		},
		{
			name:     "plain error",
			err:      errors.New("some transient error"),
			expected: false,
		},
		{
			name:     "wrapped PermanentError",
			err:      fmt.Errorf("outer: %w", &PermanentError{Msg: "inner", Cause: errors.New("cause")}),
			expected: true,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsPermanent(tt.err))
		})
	}
}

func TestPermanentError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("the cause")
	pe := &PermanentError{Msg: "bad thing happened", Cause: cause}

	assert.Contains(t, pe.Error(), "bad thing happened")
	assert.Contains(t, pe.Error(), "the cause")
	assert.Equal(t, cause, pe.Unwrap())
}

// fakeExitError creates an *exec.ExitError with the given exit code via exec.Command.
// We use a helper that always exits with the code we want.
func makeExitError(t *testing.T, code int) error {
	t.Helper()
	cmd := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code))
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr
	}
	t.Fatalf("expected ExitError for code %d, got %v", code, err)
	return nil
}

func TestClassifyRsyncError(t *testing.T) {
	tests := []struct {
		name      string
		exitCode  int
		permanent bool
	}{
		{"exit 1 syntax error", 1, true},
		{"exit 2 protocol", 2, true},
		{"exit 3 source not found", 3, true},
		{"exit 4 unsupported", 4, true},
		{"exit 5 protocol start", 5, true},
		{"exit 6 log append", 6, true},
		{"exit 24 vanished", 24, true},
		{"exit 10 socket IO", 10, false},
		{"exit 11 file IO", 11, false},
		{"exit 12 protocol stream", 12, false},
		{"exit 23 partial transfer", 23, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawErr := makeExitError(t, tt.exitCode)
			// Wrap as rsync does
			wrapped := fmt.Errorf("rsync transfer failed: %w", rawErr)
			result := classifyRsyncError(wrapped)
			assert.Equal(t, tt.permanent, IsPermanent(result),
				"exit code %d: expected permanent=%v", tt.exitCode, tt.permanent)
		})
	}
}

func TestClassifyRsyncError_NonExitError(t *testing.T) {
	err := errors.New("connection reset by peer")
	result := classifyRsyncError(err)
	assert.False(t, IsPermanent(result))
	assert.Equal(t, err, result)
}

func TestClassifyRcloneError(t *testing.T) {
	tests := []struct {
		name      string
		errMsg    string
		permanent bool
	}{
		{"not found", "object not found", true},
		{"no such file", "no such file or directory", true},
		{"no such directory", "no such directory: /remote/path", true},
		{"permission denied", "permission denied", true},
		{"access denied", "Access Denied", true},
		{"401", "server returned error 401", true},
		{"403", "403 Forbidden", true},
		{"404", "404 not found", true},
		{"connection reset", "connection reset by peer", false},
		{"timeout", "dial tcp: timeout", false},
		{"500", "server returned error 500", false},
		{"generic", "unexpected error occurred", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyRcloneError(tt.errMsg)
			assert.Equal(t, tt.permanent, IsPermanent(result),
				"errMsg=%q: expected permanent=%v", tt.errMsg, tt.permanent)
		})
	}
}

func TestClassifyRcloneError_NonPermanentFormat(t *testing.T) {
	result := classifyRcloneError("connection reset by peer")
	assert.Contains(t, result.Error(), "rclone job failed")
	assert.Contains(t, result.Error(), "connection reset by peer")
}
