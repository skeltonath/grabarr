package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
	"grabarr/internal/models"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCreateSync_Success(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	expectedSyncJob := &models.SyncJob{
		ID:         1,
		RemotePath: "/remote/path",
		Status:     models.SyncStatusQueued,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	mockSync.EXPECT().
		StartSync(mock.Anything, "/remote/path").
		Return(expectedSyncJob, nil).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	reqBody := `{"remote_path":"/remote/path"}`
	req := httptest.NewRequest("POST", "/api/v1/syncs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateSync(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, "Sync started successfully", response.Message)
}

func TestCreateSync_MissingRemotePath(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	reqBody := `{}`
	req := httptest.NewRequest("POST", "/api/v1/syncs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateSync(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "remote_path is required", response.Error)
}

func TestCreateSync_InvalidJSON(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	reqBody := `{invalid json`
	req := httptest.NewRequest("POST", "/api/v1/syncs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateSync(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "Invalid JSON payload", response.Error)
}

func TestCreateSync_MaxConcurrentReached(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		StartSync(mock.Anything, "/remote/path").
		Return(nil, errors.New("maximum concurrent syncs (1) reached, please wait for existing sync to complete")).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	reqBody := `{"remote_path":"/remote/path"}`
	req := httptest.NewRequest("POST", "/api/v1/syncs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateSync(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Contains(t, response.Error, "maximum concurrent syncs")
}

func TestCreateSync_StartError(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		StartSync(mock.Anything, "/remote/path").
		Return(nil, errors.New("internal error")).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	reqBody := `{"remote_path":"/remote/path"}`
	req := httptest.NewRequest("POST", "/api/v1/syncs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateSync(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "Failed to start sync", response.Error)
}

func TestGetSyncs_Success(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	testSyncs := []*models.SyncJob{
		{ID: 1, RemotePath: "/path1", Status: models.SyncStatusQueued},
		{ID: 2, RemotePath: "/path2", Status: models.SyncStatusRunning},
	}

	mockSync.EXPECT().
		GetSyncJobs(mock.MatchedBy(func(filter models.SyncFilter) bool {
			return filter.Limit == 50 // Default limit
		})).
		Return(testSyncs, nil).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs", nil)
	rec := httptest.NewRecorder()

	handlers.GetSyncs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
}

func TestGetSyncs_WithFilters(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		GetSyncJobs(mock.MatchedBy(func(filter models.SyncFilter) bool {
			return len(filter.Status) == 1 &&
				filter.Status[0] == models.SyncStatusRunning &&
				filter.Limit == 10
		})).
		Return([]*models.SyncJob{}, nil).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs?status=running&limit=10", nil)
	rec := httptest.NewRecorder()

	handlers.GetSyncs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetSyncs_WithPagination(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		GetSyncJobs(mock.MatchedBy(func(filter models.SyncFilter) bool {
			return filter.Limit == 25 && filter.Offset == 50
		})).
		Return([]*models.SyncJob{}, nil).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs?limit=25&offset=50", nil)
	rec := httptest.NewRecorder()

	handlers.GetSyncs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetSyncs_WithSorting(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		GetSyncJobs(mock.MatchedBy(func(filter models.SyncFilter) bool {
			return filter.SortBy == "created_at" && filter.SortOrder == "desc"
		})).
		Return([]*models.SyncJob{}, nil).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs?sort_by=created_at&sort_order=desc", nil)
	rec := httptest.NewRecorder()

	handlers.GetSyncs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetSyncs_Error(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		GetSyncJobs(mock.Anything).
		Return(nil, errors.New("database error")).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs", nil)
	rec := httptest.NewRecorder()

	handlers.GetSyncs(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetSync_Success(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	testSync := &models.SyncJob{
		ID:         123,
		RemotePath: "/remote/path",
		Status:     models.SyncStatusRunning,
		Progress: models.SyncProgress{
			Percentage:     50.0,
			LastUpdateTime: time.Now(),
		},
	}

	mockSync.EXPECT().
		GetSyncJob(int64(123)).
		Return(testSync, nil).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs/123", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "123"})
	rec := httptest.NewRecorder()

	handlers.GetSync(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
}

func TestGetSync_InvalidID(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handlers.GetSync(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "Invalid sync ID", response.Error)
}

func TestGetSync_NotFound(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		GetSyncJob(int64(999)).
		Return(nil, errors.New("sync not found")).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs/999", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "999"})
	rec := httptest.NewRecorder()

	handlers.GetSync(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCancelSync_Success(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		CancelSync(mock.Anything, int64(123)).
		Return(nil).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("POST", "/api/v1/syncs/123/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "123"})
	rec := httptest.NewRecorder()

	handlers.CancelSync(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, "Sync cancelled successfully", response.Message)
}

func TestCancelSync_InvalidID(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("POST", "/api/v1/syncs/invalid/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handlers.CancelSync(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "Invalid sync ID", response.Error)
}

func TestCancelSync_NotActive(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		CancelSync(mock.Anything, int64(123)).
		Return(errors.New("sync job is not active")).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("POST", "/api/v1/syncs/123/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "123"})
	rec := httptest.NewRecorder()

	handlers.CancelSync(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "sync job is not active", response.Error)
}

func TestCancelSync_NotFound(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		CancelSync(mock.Anything, int64(999)).
		Return(errors.New("sync job not found")).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("POST", "/api/v1/syncs/999/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "999"})
	rec := httptest.NewRecorder()

	handlers.CancelSync(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "sync job not found", response.Error)
}

func TestCancelSync_Error(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		CancelSync(mock.Anything, int64(123)).
		Return(errors.New("internal error")).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("POST", "/api/v1/syncs/123/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "123"})
	rec := httptest.NewRecorder()

	handlers.CancelSync(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetSyncSummary_Success(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	summary := &models.SyncSummary{
		TotalSyncs:     50,
		QueuedSyncs:    5,
		RunningSyncs:   2,
		CompletedSyncs: 40,
		FailedSyncs:    2,
		CancelledSyncs: 1,
	}

	mockSync.EXPECT().
		GetSyncSummary().
		Return(summary, nil).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs/summary", nil)
	rec := httptest.NewRecorder()

	handlers.GetSyncSummary(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)

	summaryData, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(50), summaryData["total_syncs"])
	assert.Equal(t, float64(5), summaryData["queued_syncs"])
}

func TestGetSyncSummary_Error(t *testing.T) {
	mockSync := mocks.NewMockSyncService(t)

	mockSync.EXPECT().
		GetSyncSummary().
		Return(nil, errors.New("database error")).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(nil, nil, cfg, mockSync)

	req := httptest.NewRequest("GET", "/api/v1/syncs/summary", nil)
	rec := httptest.NewRecorder()

	handlers.GetSyncSummary(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
