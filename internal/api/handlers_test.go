package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"grabarr/internal/config"
	"grabarr/internal/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestHandlers(t *testing.T) (*Handlers, *mocks.MockJobQueue, *mocks.MockGatekeeper, *mocks.MockSyncService) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}

	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, mockSync)
	return handlers, mockQueue, mockGatekeeper, mockSync
}

func TestNewHandlers(t *testing.T) {
	mockQueue := mocks.NewMockJobQueue(t)
	mockGatekeeper := mocks.NewMockGatekeeper(t)
	mockSync := mocks.NewMockSyncService(t)
	cfg := &config.Config{}

	handlers := NewHandlers(mockQueue, mockGatekeeper, cfg, mockSync)

	assert.NotNil(t, handlers)
	assert.Equal(t, mockQueue, handlers.queue)
	assert.Equal(t, mockGatekeeper, handlers.gatekeeper)
	assert.Equal(t, cfg, handlers.config)
	assert.Equal(t, mockSync, handlers.syncService)
}

func TestWriteSuccess(t *testing.T) {
	h, _, _, _ := setupTestHandlers(t)
	w := httptest.NewRecorder()

	data := map[string]string{"key": "value"}
	h.writeSuccess(w, 200, data, "Operation successful")

	assert.Equal(t, 200, w.Code)

	var response APIResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.Equal(t, "Operation successful", response.Message)
	assert.NotNil(t, response.Data)

	// Verify data structure
	dataMap, ok := response.Data.(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "value", dataMap["key"])
}

func TestWriteSuccess_NilData(t *testing.T) {
	h, _, _, _ := setupTestHandlers(t)
	w := httptest.NewRecorder()

	h.writeSuccess(w, 204, nil, "")

	assert.Equal(t, 204, w.Code)

	var response APIResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response.Success)
	assert.Nil(t, response.Data)
}

func TestWriteError_WithError(t *testing.T) {
	h, _, _, _ := setupTestHandlers(t)
	w := httptest.NewRecorder()

	err := errors.New("something went wrong")
	h.writeError(w, 500, "Internal server error", err)

	assert.Equal(t, 500, w.Code)

	var response APIResponse
	decodeErr := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, decodeErr)

	assert.False(t, response.Success)
	assert.Equal(t, "Internal server error", response.Error)
}

func TestWriteError_WithoutError(t *testing.T) {
	h, _, _, _ := setupTestHandlers(t)
	w := httptest.NewRecorder()

	h.writeError(w, 400, "Bad request", nil)

	assert.Equal(t, 400, w.Code)

	var response APIResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.False(t, response.Success)
	assert.Equal(t, "Bad request", response.Error)
}

func TestAPIResponse_JSONFormat(t *testing.T) {
	tests := []struct {
		name     string
		response APIResponse
		wantJSON string
	}{
		{
			name: "success with data",
			response: APIResponse{
				Success: true,
				Data:    map[string]string{"test": "value"},
				Message: "ok",
			},
			wantJSON: `{"success":true,"data":{"test":"value"},"message":"ok"}`,
		},
		{
			name: "error response",
			response: APIResponse{
				Success: false,
				Error:   "error message",
			},
			wantJSON: `{"success":false,"error":"error message"}`,
		},
		{
			name: "success without message",
			response: APIResponse{
				Success: true,
				Data:    nil,
			},
			wantJSON: `{"success":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tt.response)
			require.NoError(t, err)
			assert.JSONEq(t, tt.wantJSON, string(jsonBytes))
		})
	}
}