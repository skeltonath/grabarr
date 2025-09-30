package notifications

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func createTestConfig(enabled bool) *config.Config {
	return &config.Config{
		Notifications: config.NotificationsConfig{
			Pushover: config.PushoverConfig{
				Enabled:       enabled,
				Token:         "test-token",
				User:          "test-user",
				Priority:      0,
				RetryInterval: 30 * time.Second,
				ExpireTime:    300 * time.Second,
			},
		},
	}
}

func createMockPushoverServer(t *testing.T, expectedToken, expectedUser string, statusCode int, response pushoverResponse) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify HTTP method
		assert.Equal(t, "POST", r.Method)

		// Verify headers
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "grabarr/1.0", r.Header.Get("User-Agent"))

		// Parse request body
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req pushoverRequest
		err = json.Unmarshal(body, &req)
		require.NoError(t, err)

		// Verify credentials
		assert.Equal(t, expectedToken, req.Token)
		assert.Equal(t, expectedUser, req.User)

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(response)
	}))
}

// Constructor & Configuration Tests

func TestNewPushoverNotifier(t *testing.T) {
	cfg := createTestConfig(true)

	notifier := NewPushoverNotifier(cfg)

	assert.NotNil(t, notifier)
	assert.Equal(t, cfg, notifier.config)
	assert.NotNil(t, notifier.httpClient)
	assert.True(t, notifier.enabled)
	assert.Equal(t, pushoverAPIURL, notifier.apiURL)
	assert.Equal(t, 30*time.Second, notifier.httpClient.Timeout)
}

func TestIsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enabled",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disabled",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createTestConfig(tt.enabled)
			notifier := NewPushoverNotifier(cfg)

			assert.Equal(t, tt.expected, notifier.IsEnabled())
		})
	}
}

// NotifyJobFailed Tests

func TestNotifyJobFailed_Success(t *testing.T) {
	cfg := createTestConfig(true)

	mockServer := createMockPushoverServer(t, "test-token", "test-user", http.StatusOK, pushoverResponse{
		Status:  1,
		Request: "test-request-id",
	})
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	job := &models.Job{
		ID:           123,
		Name:         "test-job",
		RemotePath:   "/remote/path/test.mkv",
		Status:       models.JobStatusFailed,
		Retries:      1,
		MaxRetries:   3,
		ErrorMessage: "test error",
	}

	err := notifier.NotifyJobFailed(job)

	assert.NoError(t, err)
}

func TestNotifyJobFailed_Disabled(t *testing.T) {
	cfg := createTestConfig(false)
	notifier := NewPushoverNotifier(cfg)

	job := &models.Job{
		ID:   123,
		Name: "test-job",
	}

	err := notifier.NotifyJobFailed(job)

	assert.NoError(t, err)
}

func TestNotifyJobFailed_MaxRetriesReached(t *testing.T) {
	cfg := createTestConfig(true)

	var capturedReq pushoverRequest
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(pushoverResponse{Status: 1})
	}))
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	job := &models.Job{
		ID:         123,
		Name:       "test-job",
		Retries:    3,
		MaxRetries: 3,
	}

	err := notifier.NotifyJobFailed(job)

	assert.NoError(t, err)
	assert.Equal(t, 1, capturedReq.Priority) // High priority
	assert.Equal(t, "siren", capturedReq.Sound)
}

func TestNotifyJobFailed_EmergencyPriority(t *testing.T) {
	cfg := createTestConfig(true)
	cfg.Notifications.Pushover.Priority = 2 // Emergency

	var capturedReq pushoverRequest
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedReq)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(pushoverResponse{Status: 1})
	}))
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	job := &models.Job{
		ID:         123,
		Name:       "test-job",
		Retries:    1,
		MaxRetries: 3,
	}

	err := notifier.NotifyJobFailed(job)

	assert.NoError(t, err)
	assert.Equal(t, 2, capturedReq.Priority)
	assert.Equal(t, 30, capturedReq.Retry)   // From config
	assert.Equal(t, 300, capturedReq.Expire) // From config
}

func TestNotifyJobFailed_WithProgress(t *testing.T) {
	cfg := createTestConfig(true)

	mockServer := createMockPushoverServer(t, "test-token", "test-user", http.StatusOK, pushoverResponse{
		Status:  1,
		Request: "test-request-id",
	})
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	startTime := time.Now().Add(-5 * time.Minute)
	job := &models.Job{
		ID:         123,
		Name:       "test-job",
		RemotePath: "/remote/path",
		StartedAt:  &startTime,
		Progress: models.JobProgress{
			TotalBytes:       1024 * 1024 * 100, // 100 MB
			TransferredBytes: 1024 * 1024 * 50,  // 50 MB
			Percentage:       50.0,
		},
		Metadata: models.JobMetadata{
			Category: "movies",
		},
	}

	err := notifier.NotifyJobFailed(job)

	assert.NoError(t, err)
}

// NotifyJobCompleted Tests

func TestNotifyJobCompleted_Success(t *testing.T) {
	cfg := createTestConfig(true)

	mockServer := createMockPushoverServer(t, "test-token", "test-user", http.StatusOK, pushoverResponse{
		Status:  1,
		Request: "test-request-id",
	})
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	startTime := time.Now().Add(-10 * time.Minute)
	completedTime := time.Now()
	job := &models.Job{
		ID:          123,
		Name:        "test-job",
		Priority:    5, // High enough to trigger notification
		RemotePath:  "/remote/path",
		StartedAt:   &startTime,
		CompletedAt: &completedTime,
		Progress: models.JobProgress{
			TotalBytes:    1024 * 1024 * 100,
			TransferSpeed: 1024 * 1024,
		},
		Metadata: models.JobMetadata{
			Category: "movies",
		},
	}

	err := notifier.NotifyJobCompleted(job)

	assert.NoError(t, err)
}

func TestNotifyJobCompleted_Disabled(t *testing.T) {
	cfg := createTestConfig(false)
	notifier := NewPushoverNotifier(cfg)

	job := &models.Job{
		ID:       123,
		Name:     "test-job",
		Priority: 5,
	}

	err := notifier.NotifyJobCompleted(job)

	assert.NoError(t, err)
}

func TestNotifyJobCompleted_LowPriority(t *testing.T) {
	cfg := createTestConfig(true)
	notifier := NewPushoverNotifier(cfg)

	job := &models.Job{
		ID:       123,
		Name:     "test-job",
		Priority: 3, // Less than 5
	}

	err := notifier.NotifyJobCompleted(job)

	// Should not send notification
	assert.NoError(t, err)
}

// NotifySystemAlert Tests

func TestNotifySystemAlert_Success(t *testing.T) {
	cfg := createTestConfig(true)

	mockServer := createMockPushoverServer(t, "test-token", "test-user", http.StatusOK, pushoverResponse{
		Status:  1,
		Request: "test-request-id",
	})
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	err := notifier.NotifySystemAlert("Test Alert", "This is a test message", 0)

	assert.NoError(t, err)
}

func TestNotifySystemAlert_Disabled(t *testing.T) {
	cfg := createTestConfig(false)
	notifier := NewPushoverNotifier(cfg)

	err := notifier.NotifySystemAlert("Test Alert", "Test message", 0)

	assert.NoError(t, err)
}

func TestNotifySystemAlert_Priorities(t *testing.T) {
	tests := []struct {
		name          string
		priority      int
		expectedSound string
		hasRetry      bool
	}{
		{
			name:          "lowest priority",
			priority:      -2,
			expectedSound: "none",
			hasRetry:      false,
		},
		{
			name:          "low priority",
			priority:      -1,
			expectedSound: "none",
			hasRetry:      false,
		},
		{
			name:          "normal priority",
			priority:      0,
			expectedSound: "pushover",
			hasRetry:      false,
		},
		{
			name:          "high priority",
			priority:      1,
			expectedSound: "persistent",
			hasRetry:      false,
		},
		{
			name:          "emergency priority",
			priority:      2,
			expectedSound: "siren",
			hasRetry:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createTestConfig(true)

			var capturedReq pushoverRequest
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &capturedReq)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(pushoverResponse{Status: 1})
			}))
			defer mockServer.Close()

			notifier := NewPushoverNotifier(cfg)
			notifier.apiURL = mockServer.URL

			err := notifier.NotifySystemAlert("Test", "Test message", tt.priority)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedSound, capturedReq.Sound)
			assert.Equal(t, tt.priority, capturedReq.Priority)

			if tt.hasRetry {
				assert.Equal(t, 30, capturedReq.Retry)
				assert.Equal(t, 300, capturedReq.Expire)
			} else {
				assert.Equal(t, 0, capturedReq.Retry)
				assert.Equal(t, 0, capturedReq.Expire)
			}
		})
	}
}

// sendNotification Tests

func TestSendNotification_Success(t *testing.T) {
	cfg := createTestConfig(true)

	mockServer := createMockPushoverServer(t, "test-token", "test-user", http.StatusOK, pushoverResponse{
		Status:  1,
		Request: "test-request-id",
		Receipt: "test-receipt",
	})
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	req := pushoverRequest{
		Token:    "test-token",
		User:     "test-user",
		Message:  "Test message",
		Title:    "Test Title",
		Priority: 0,
	}

	err := notifier.sendNotification(req)

	assert.NoError(t, err)
}

func TestSendNotification_APIError(t *testing.T) {
	cfg := createTestConfig(true)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(pushoverResponse{
			Status: 0,
			Errors: []string{"invalid token", "user not found"},
		})
	}))
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	req := pushoverRequest{
		Token:   "invalid-token",
		User:    "invalid-user",
		Message: "Test",
	}

	err := notifier.sendNotification(req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pushover API error")
	assert.Contains(t, err.Error(), "invalid token")
}

func TestSendNotification_HTTPError(t *testing.T) {
	cfg := createTestConfig(true)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	req := pushoverRequest{
		Token:   "test-token",
		User:    "test-user",
		Message: "Test",
	}

	err := notifier.sendNotification(req)

	assert.Error(t, err)
}

func TestSendNotification_InvalidJSON(t *testing.T) {
	cfg := createTestConfig(true)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	req := pushoverRequest{
		Token:   "test-token",
		User:    "test-user",
		Message: "Test",
	}

	err := notifier.sendNotification(req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode pushover response")
}

func TestSendNotification_InvalidURL(t *testing.T) {
	cfg := createTestConfig(true)
	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = "://invalid-url"

	req := pushoverRequest{
		Token:   "test-token",
		User:    "test-user",
		Message: "Test",
	}

	err := notifier.sendNotification(req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create HTTP request")
}

// Message Building Tests

func TestBuildJobFailedMessage(t *testing.T) {
	cfg := createTestConfig(true)
	notifier := NewPushoverNotifier(cfg)

	startTime := time.Now().Add(-5 * time.Minute)
	job := &models.Job{
		ID:           123,
		Name:         "test-job",
		RemotePath:   "/remote/path/test.mkv",
		Status:       models.JobStatusFailed,
		Retries:      2,
		MaxRetries:   3,
		ErrorMessage: "connection timeout",
		StartedAt:    &startTime,
		Progress: models.JobProgress{
			TotalBytes:       1024 * 1024 * 100, // 100 MB
			TransferredBytes: 1024 * 1024 * 25,  // 25 MB
			Percentage:       25.0,
		},
		Metadata: models.JobMetadata{
			Category: "movies",
		},
	}

	message := notifier.buildJobFailedMessage(job)

	assert.Contains(t, message, "test-job")
	assert.Contains(t, message, "/remote/path/test.mkv")
	assert.Contains(t, message, "failed")
	assert.Contains(t, message, "2/3")
	assert.Contains(t, message, "connection timeout")
	assert.Contains(t, message, "25.0%")
	assert.Contains(t, message, "movies")
	assert.Contains(t, message, "123")
}

func TestBuildJobCompletedMessage(t *testing.T) {
	cfg := createTestConfig(true)
	notifier := NewPushoverNotifier(cfg)

	startTime := time.Now().Add(-10 * time.Minute)
	completedTime := time.Now()
	job := &models.Job{
		ID:          456,
		Name:        "completed-job",
		RemotePath:  "/remote/path/complete.mkv",
		StartedAt:   &startTime,
		CompletedAt: &completedTime,
		Progress: models.JobProgress{
			TotalBytes:    1024 * 1024 * 500, // 500 MB
			TransferSpeed: 1024 * 1024 * 10,  // 10 MB/s
		},
		Metadata: models.JobMetadata{
			Category: "tv",
		},
	}

	message := notifier.buildJobCompletedMessage(job)

	assert.Contains(t, message, "completed-job")
	assert.Contains(t, message, "/remote/path/complete.mkv")
	assert.Contains(t, message, "500.0 MB")
	assert.Contains(t, message, "10.0 MB/s")
	assert.Contains(t, message, "tv")
	assert.Contains(t, message, "456")
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{
			name:     "zero bytes",
			bytes:    0,
			expected: "0 B",
		},
		{
			name:     "bytes",
			bytes:    512,
			expected: "512 B",
		},
		{
			name:     "kilobytes",
			bytes:    1024 * 5,
			expected: "5.0 KB",
		},
		{
			name:     "megabytes",
			bytes:    1024 * 1024 * 100,
			expected: "100.0 MB",
		},
		{
			name:     "gigabytes",
			bytes:    1024 * 1024 * 1024 * 5,
			expected: "5.0 GB",
		},
		{
			name:     "terabytes",
			bytes:    1024 * 1024 * 1024 * 1024 * 2,
			expected: "2.0 TB",
		},
		{
			name:     "fractional",
			bytes:    1024*1024*100 + 1024*512, // 100.5 MB
			expected: "100.5 MB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// NotifySyncFailed Tests

func TestNotifySyncFailed_Success(t *testing.T) {
	cfg := createTestConfig(true)
	mockServer := createMockPushoverServer(t, "test-token", "test-user", http.StatusOK, pushoverResponse{
		Status:  1,
		Request: "test-request-id",
	})
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	now := time.Now()
	syncJob := &models.SyncJob{
		ID:           1,
		RemotePath:   "/remote/path",
		LocalPath:    "/local/path",
		Status:       models.SyncStatusFailed,
		ErrorMessage: "Test error",
		StartedAt:    &now,
	}

	err := notifier.NotifySyncFailed(syncJob)
	assert.NoError(t, err)
}

func TestNotifySyncFailed_Disabled(t *testing.T) {
	cfg := createTestConfig(false)
	notifier := NewPushoverNotifier(cfg)

	syncJob := &models.SyncJob{ID: 1}

	err := notifier.NotifySyncFailed(syncJob)
	assert.NoError(t, err)
}

// NotifySyncCompleted Tests

func TestNotifySyncCompleted_Success(t *testing.T) {
	cfg := createTestConfig(true)
	mockServer := createMockPushoverServer(t, "test-token", "test-user", http.StatusOK, pushoverResponse{
		Status:  1,
		Request: "test-request-id",
	})
	defer mockServer.Close()

	notifier := NewPushoverNotifier(cfg)
	notifier.apiURL = mockServer.URL

	now := time.Now()
	completed := now.Add(5 * time.Minute)
	syncJob := &models.SyncJob{
		ID:          1,
		RemotePath:  "/remote/path",
		LocalPath:   "/local/path",
		Status:      models.SyncStatusCompleted,
		StartedAt:   &now,
		CompletedAt: &completed,
		Stats: models.SyncStats{
			FilesTransferred: 100,
			TotalBytes:       1024000,
		},
	}

	err := notifier.NotifySyncCompleted(syncJob)
	assert.NoError(t, err)
}

func TestNotifySyncCompleted_Disabled(t *testing.T) {
	cfg := createTestConfig(false)
	notifier := NewPushoverNotifier(cfg)

	syncJob := &models.SyncJob{ID: 1}

	err := notifier.NotifySyncCompleted(syncJob)
	assert.NoError(t, err)
}

