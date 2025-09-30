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

func TestHealthCheck_WithMonitor(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockMonitor := mocks.NewMockResourceMonitor(t)
	cfg := &config.Config{}

	resourceStatus := interfaces.ResourceStatus{
		BandwidthAvailable: true,
		BandwidthUsage:     45.5,
		DiskSpaceAvailable: true,
		CacheDiskFree:      1024 * 1024 * 1024 * 10, // 10GB
		ArrayDiskFree:      1024 * 1024 * 1024 * 100, // 100GB
	}

	mockMonitor.EXPECT().
		GetResourceStatus().
		Return(resourceStatus).
		Once()

	handlers := NewHandlers(mockQueue, mockMonitor, cfg, nil)

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

func TestHealthCheck_WithoutMonitor(t *testing.T) {
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
	mockMonitor := mocks.NewMockResourceMonitor(t)
	cfg := &config.Config{}

	metrics := map[string]interface{}{
		"cpu_percent":    45.5,
		"memory_percent": 60.2,
		"disk_percent":   75.0,
	}

	mockMonitor.EXPECT().
		GetMetrics().
		Return(metrics).
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

	handlers := NewHandlers(mockQueue, mockMonitor, cfg, nil)

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
	assert.Equal(t, 45.5, data["cpu_percent"])
	assert.NotNil(t, data["jobs"])
}

func TestGetMetrics_WithoutMonitor(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	cfg := &config.Config{}

	handlers := NewHandlers(mockQueue, nil, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()

	handlers.GetMetrics(rec, req)

	assert.Equal(t, 503, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.False(t, response.Success)
	assert.Equal(t, "Monitoring not available", response.Error)
}

func TestGetMetrics_JobSummaryError(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockMonitor := mocks.NewMockResourceMonitor(t)
	cfg := &config.Config{}

	metrics := map[string]interface{}{
		"cpu_percent": 45.5,
	}

	mockMonitor.EXPECT().
		GetMetrics().
		Return(metrics).
		Once()

	mockQueue.EXPECT().
		GetSummary().
		Return(nil, errors.New("database error")).
		Once()

	handlers := NewHandlers(mockQueue, mockMonitor, cfg, nil)

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
	mockMonitor := mocks.NewMockResourceMonitor(t)
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

	resourceStatus := interfaces.ResourceStatus{
		BandwidthAvailable: true,
		BandwidthUsage:     30.5,
		DiskSpaceAvailable: true,
		CacheDiskFree:      1024 * 1024 * 1024 * 5,  // 5GB
		ArrayDiskFree:      1024 * 1024 * 1024 * 50, // 50GB
	}

	mockMonitor.EXPECT().
		GetResourceStatus().
		Return(resourceStatus).
		Once()

	handlers := NewHandlers(mockQueue, mockMonitor, cfg, nil)

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

	handlers := NewHandlers(mockQueue, nil, cfg, nil)

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
	mockMonitor := mocks.NewMockResourceMonitor(t)
	cfg := &config.Config{}

	mockQueue.EXPECT().
		GetSummary().
		Return(nil, errors.New("database error")).
		Once()

	resourceStatus := interfaces.ResourceStatus{
		BandwidthAvailable: false,
		BandwidthUsage:     95.0,
		DiskSpaceAvailable: false,
	}

	mockMonitor.EXPECT().
		GetResourceStatus().
		Return(resourceStatus).
		Once()

	handlers := NewHandlers(mockQueue, mockMonitor, cfg, nil)

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