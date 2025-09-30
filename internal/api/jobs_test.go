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
	"grabarr/internal/interfaces"
	"grabarr/internal/mocks"
	"grabarr/internal/models"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestCreateJob_Success(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)

	mockGatekeeper.EXPECT().
		CanStartJob(mock.AnythingOfType("int64")).
		Return(interfaces.GateDecision{Allowed: true, Reason: "All checks passed"}).
		Once()

	mockQueue.EXPECT().
		Enqueue(mock.AnythingOfType("*models.Job")).
		RunAndReturn(func(job *models.Job) error {
			job.ID = 123
			return nil
		}).
		Once()

	cfg := &config.Config{}
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	reqBody := `{"name":"test-job","remote_path":"/remote/path"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateJob(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, "Job created successfully", response.Message)

	jobData, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(123), jobData["id"])
}

func TestCreateJob_MissingName(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	reqBody := `{"remote_path":"/remote/path"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "job name is required", response.Error)
}

func TestCreateJob_MissingRemotePath(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	reqBody := `{"name":"test-job"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "remote_path is required", response.Error)
}

func TestCreateJob_InvalidJSON(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	reqBody := `{invalid json`
	req := httptest.NewRequest("POST", "/api/v1/jobs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "Invalid JSON payload", response.Error)
}

func TestCreateJob_CategoryNotAllowed(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	cfg := &config.Config{
		Downloads: config.DownloadsConfig{
			AllowedCategories: []string{"movies", "tv"},
		},
	}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	reqBody := `{"name":"test","remote_path":"/path","metadata":{"category":"music"}}`
	req := httptest.NewRequest("POST", "/api/v1/jobs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Contains(t, response.Error, "category 'music' not allowed")
}

func TestCreateJob_EnqueueError(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockQueue.EXPECT().
		Enqueue(mock.AnythingOfType("*models.Job")).
		Return(errors.New("queue is full")).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	reqBody := `{"name":"test","remote_path":"/path"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	handlers.CreateJob(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "Failed to enqueue job", response.Error)
}

func TestGetJobs_Success(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	testJobs := []*models.Job{
		{ID: 1, Name: "job1", Status: models.JobStatusQueued},
		{ID: 2, Name: "job2", Status: models.JobStatusRunning},
	}

	mockQueue.EXPECT().
		GetJobs(mock.MatchedBy(func(filter models.JobFilter) bool {
			return filter.Limit == 50 // Default limit
		})).
		Return(testJobs, nil).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	rec := httptest.NewRecorder()

	handlers.GetJobs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
}

func TestGetJobs_WithFilters(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	mockQueue.EXPECT().
		GetJobs(mock.MatchedBy(func(filter models.JobFilter) bool {
			return len(filter.Status) == 1 &&
				filter.Status[0] == models.JobStatusQueued &&
				filter.Category == "movies" &&
				filter.Limit == 10
		})).
		Return([]*models.Job{}, nil).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs?status=queued&category=movies&limit=10", nil)
	rec := httptest.NewRecorder()

	handlers.GetJobs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetJobs_WithPagination(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	mockQueue.EXPECT().
		GetJobs(mock.MatchedBy(func(filter models.JobFilter) bool {
			return filter.Limit == 25 && filter.Offset == 50
		})).
		Return([]*models.Job{}, nil).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs?limit=25&offset=50", nil)
	rec := httptest.NewRecorder()

	handlers.GetJobs(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestGetJobs_Error(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	mockQueue.EXPECT().
		GetJobs(mock.Anything).
		Return(nil, errors.New("database error")).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	rec := httptest.NewRecorder()

	handlers.GetJobs(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetJob_Success(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	testJob := &models.Job{
		ID:     123,
		Name:   "test-job",
		Status: models.JobStatusRunning,
		Progress: models.JobProgress{
			Percentage: 50.0,
			LastUpdateTime: time.Now(),
		},
	}

	mockQueue.EXPECT().
		GetJob(int64(123)).
		Return(testJob, nil).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs/123", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "123"})
	rec := httptest.NewRecorder()

	handlers.GetJob(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
}

func TestGetJob_InvalidID(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handlers.GetJob(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.False(t, response.Success)
	assert.Equal(t, "Invalid job ID", response.Error)
}

func TestGetJob_NotFound(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	mockQueue.EXPECT().
		GetJob(int64(999)).
		Return(nil, errors.New("job not found")).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs/999", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "999"})
	rec := httptest.NewRecorder()

	handlers.GetJob(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteJob_Success(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	mockQueue.EXPECT().
		CancelJob(int64(123)).
		Return(nil).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("DELETE", "/api/v1/jobs/123", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "123"})
	rec := httptest.NewRecorder()

	handlers.DeleteJob(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, "Job deleted successfully", response.Message)
}

func TestCancelJob_Success(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	mockQueue.EXPECT().
		CancelJob(int64(123)).
		Return(nil).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("POST", "/api/v1/jobs/123/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "123"})
	rec := httptest.NewRecorder()

	handlers.CancelJob(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, "Job cancelled successfully", response.Message)
}

func TestCancelJob_Error(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	mockQueue.EXPECT().
		CancelJob(int64(123)).
		Return(errors.New("cannot cancel completed job")).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("POST", "/api/v1/jobs/123/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "123"})
	rec := httptest.NewRecorder()

	handlers.CancelJob(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetJobSummary_Success(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

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

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs/summary", nil)
	rec := httptest.NewRecorder()

	handlers.GetJobSummary(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response APIResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)
	assert.True(t, response.Success)

	summaryData, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(100), summaryData["total_jobs"])
	assert.Equal(t, float64(10), summaryData["queued_jobs"])
}

func TestGetJobSummary_Error(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)

	mockQueue.EXPECT().
		GetSummary().
		Return(nil, errors.New("database error")).
		Once()

	cfg := &config.Config{}
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockGatekeeper.EXPECT().CanStartJob(mock.AnythingOfType("int64")).Return(interfaces.GateDecision{Allowed: true}).Maybe()
	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, nil)

	req := httptest.NewRequest("GET", "/api/v1/jobs/summary", nil)
	rec := httptest.NewRecorder()

	handlers.GetJobSummary(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  bool
	}{
		{"item exists", []string{"a", "b", "c"}, "b", true},
		{"item not exists", []string{"a", "b", "c"}, "d", false},
		{"empty slice", []string{}, "a", false},
		{"exact match", []string{"test"}, "test", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.slice, tt.item)
			assert.Equal(t, tt.want, got)
		})
	}
}