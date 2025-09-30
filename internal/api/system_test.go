package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"
	"grabarr/internal/mocks"
	"grabarr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthCheck_WithGatekeeper(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	cfg := &config.Config{}

	resourceStatus := interfaces.GatekeeperResourceStatus{
		BandwidthUsageMbps: 250.5,
		BandwidthLimitMbps: 500,
		CacheUsagePercent:  45.2,
		CacheMaxPercent:    80,
		CacheFreeBytes:     1024 * 1024 * 1024 * 10,  // 10GB
		CacheTotalBytes:    1024 * 1024 * 1024 * 100, // 100GB
		ActiveSyncs:        0,
	}

	mockGatekeeper.EXPECT().
		GetResourceStatus().
		Return(resourceStatus).
		Once()

	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	handlers.HealthCheck(rec, req)

	assert.Equal(t, 200, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.Equal(t, "Service is healthy", response.Message)

	data, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "healthy", data["status"])
	assert.NotNil(t, data["timestamp"])
	assert.NotNil(t, data["uptime"])
	assert.Equal(t, "1.0.0", data["version"])
	assert.NotNil(t, data["resources"])
}

func TestHealthCheck_WithoutGatekeeper(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	cfg := &config.Config{}

	handlers := NewHandlers(mockQueue, nil, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	handlers.HealthCheck(rec, req)

	assert.Equal(t, 200, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)

	data, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "healthy", data["status"])
	assert.Nil(t, data["resources"]) // No monitor, no resources
}

func TestGetMetrics_Success(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}

	resourceStatus := interfaces.GatekeeperResourceStatus{
		BandwidthUsageMbps: 250.5,
		BandwidthLimitMbps: 500,
		CacheUsagePercent:  45.2,
		CacheMaxPercent:    80,
	}

	mockGatekeeper.EXPECT().
		GetResourceStatus().
		Return(resourceStatus).
		Once()

	summary := &models.JobSummary{
		TotalJobs:     100,
		QueuedJobs:    10,
		RunningJobs:   5,
		CompletedJobs: 80,
		FailedJobs:    3,
		CancelledJobs: 2,
	}

	mockQueue.EXPECT().
		GetSummary().
		Return(summary, nil).
		Once()

	syncSummary := &models.SyncSummary{
		TotalSyncs:     10,
		QueuedSyncs:    2,
		RunningSyncs:   1,
		CompletedSyncs: 6,
		FailedSyncs:    1,
	}

	mockSync.EXPECT().
		GetSyncSummary().
		Return(syncSummary, nil).
		Once()

	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()

	handlers.GetMetrics(rec, req)

	assert.Equal(t, 200, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)

	data, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, data["resources"])
	assert.NotNil(t, data["jobs"])
	assert.NotNil(t, data["syncs"])
}

func TestGetMetrics_JobSummaryError(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}

	resourceStatus := interfaces.GatekeeperResourceStatus{
		BandwidthUsageMbps: 250.5,
		BandwidthLimitMbps: 500,
	}

	mockGatekeeper.EXPECT().
		GetResourceStatus().
		Return(resourceStatus).
		Once()

	mockQueue.EXPECT().
		GetSummary().
		Return(nil, errors.New("database error")).
		Once()

	mockSync.EXPECT().
		GetSyncSummary().
		Return(nil, errors.New("database error")).
		Once()

	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()

	handlers.GetMetrics(rec, req)

	assert.Equal(t, 200, rec.Code) // Still succeeds without job metrics

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)

	data, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Nil(t, data["jobs"]) // Job summary not included
}

func TestGetStatus_Full(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}

	summary := &models.JobSummary{
		TotalJobs:     100,
		QueuedJobs:    10,
		RunningJobs:   5,
		CompletedJobs: 80,
		FailedJobs:    3,
		CancelledJobs: 2,
	}

	mockQueue.EXPECT().
		GetSummary().
		Return(summary, nil).
		Once()

	syncSummary := &models.SyncSummary{
		TotalSyncs:   5,
		QueuedSyncs:  1,
		RunningSyncs: 0,
	}

	mockSync.EXPECT().
		GetSyncSummary().
		Return(syncSummary, nil).
		Once()

	resourceStatus := interfaces.GatekeeperResourceStatus{
		BandwidthUsageMbps: 150.5,
		BandwidthLimitMbps: 500,
		CacheUsagePercent:  30.5,
		CacheMaxPercent:    80,
	}

	mockGatekeeper.EXPECT().
		GetResourceStatus().
		Return(resourceStatus).
		Once()

	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rec := httptest.NewRecorder()

	handlers.GetStatus(rec, req)

	assert.Equal(t, 200, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)

	data, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "grabarr", data["service"])
	assert.Equal(t, "1.0.0", data["version"])
	assert.NotNil(t, data["timestamp"])
	assert.NotNil(t, data["uptime"])
	assert.NotNil(t, data["jobs"])
	assert.NotNil(t, data["resources"])
}

func TestGetStatus_WithoutMonitor(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	cfg := &config.Config{}

	summary := &models.JobSummary{
		TotalJobs:  50,
		QueuedJobs: 5,
	}

	mockQueue.EXPECT().
		GetSummary().
		Return(summary, nil).
		Once()

	mockSync := mocks.NewMockSyncService(t)
	mockSync.EXPECT().
		GetSyncSummary().
		Return(&models.SyncSummary{}, nil).
		Once()

	handlers := NewHandlers(mockQueue, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rec := httptest.NewRecorder()

	handlers.GetStatus(rec, req)

	assert.Equal(t, 200, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)

	data, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, data["jobs"])
	assert.Nil(t, data["resources"]) // No monitor
}

func TestGetStatus_JobSummaryError(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}

	mockQueue.EXPECT().
		GetSummary().
		Return(nil, errors.New("database error")).
		Once()

	mockSync.EXPECT().
		GetSyncSummary().
		Return(nil, errors.New("database error")).
		Once()

	resourceStatus := interfaces.GatekeeperResourceStatus{
		BandwidthUsageMbps: 495.0,
		BandwidthLimitMbps: 500,
		CacheUsagePercent:  95.0,
		CacheMaxPercent:    80,
	}

	mockGatekeeper.EXPECT().
		GetResourceStatus().
		Return(resourceStatus).
		Once()

	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	rec := httptest.NewRecorder()

	handlers.GetStatus(rec, req)

	assert.Equal(t, 200, rec.Code) // Still succeeds without job summary

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)

	data, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Nil(t, data["jobs"]) // Job summary not included
	assert.NotNil(t, data["resources"])
}
