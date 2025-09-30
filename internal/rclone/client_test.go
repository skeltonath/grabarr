package rclone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client := NewClient("http://localhost:5572")
	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:5572", client.baseURL)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, 30*time.Second, client.httpClient.Timeout)
}

func TestClient_Copy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/sync/copy", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req SyncCopyRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, "remote:/source", req.SrcFs)
		assert.Equal(t, "/dest", req.DstFs)
		assert.True(t, req.Async)
		assert.NotNil(t, req.Filter)

		resp := CopyResponse{JobID: 12345}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	filter := map[string]interface{}{
		"IncludeRule": []string{"*.mkv"},
	}

	resp, err := client.Copy(context.Background(), "remote:/source", "/dest", filter)
	require.NoError(t, err)
	assert.Equal(t, int64(12345), resp.JobID)
}

func TestClient_CopyWithIgnoreExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req SyncCopyRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, "remote:/source", req.SrcFs)
		assert.Equal(t, "/dest", req.DstFs)
		assert.True(t, req.Async)
		assert.NotNil(t, req.Config)

		// Verify IgnoreExisting is set
		ignoreExisting, ok := req.Config["IgnoreExisting"]
		assert.True(t, ok)
		assert.True(t, ignoreExisting.(bool))

		resp := CopyResponse{JobID: 54321}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	filter := map[string]interface{}{}

	resp, err := client.CopyWithIgnoreExisting(context.Background(), "remote:/source", "/dest", filter)
	require.NoError(t, err)
	assert.Equal(t, int64(54321), resp.JobID)
}

func TestClient_GetJobStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/job/status", r.URL.Path)
		assert.Equal(t, "12345", r.URL.Query().Get("jobid"))

		status := JobStatus{
			ID:       12345,
			Name:     "sync/copy",
			Group:    "sync",
			Finished: false,
			Success:  false,
			Duration: 123.45,
			Output: Output{
				Bytes:          1024 * 1024,
				TotalBytes:     1024 * 1024 * 10,
				Transfers:      5,
				TotalTransfers: 10,
				Speed:          1024 * 512,
			},
		}
		json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	status, err := client.GetJobStatus(context.Background(), 12345)
	require.NoError(t, err)
	assert.Equal(t, int64(12345), status.ID)
	assert.Equal(t, "sync/copy", status.Name)
	assert.False(t, status.Finished)
	assert.Equal(t, int64(1024*1024), status.Output.Bytes)
	assert.Equal(t, int64(1024*1024*10), status.Output.TotalBytes)
}

func TestClient_ListJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/job/list", r.URL.Path)

		resp := JobListResponse{
			JobIDs: []int64{1, 2, 3, 4, 5},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.ListJobs(context.Background())
	require.NoError(t, err)
	assert.Len(t, resp.JobIDs, 5)
	assert.Equal(t, []int64{1, 2, 3, 4, 5}, resp.JobIDs)
}

func TestClient_StopJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/job/stop", r.URL.Path)
		assert.Equal(t, "12345", r.URL.Query().Get("jobid"))

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.StopJob(context.Background(), 12345)
	require.NoError(t, err)
}

func TestClient_Ping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/core/pid", r.URL.Path)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]int{"pid": 1234})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.Ping(context.Background())
	require.NoError(t, err)
}

func TestClient_HTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"bad request", http.StatusBadRequest, "invalid request"},
		{"not found", http.StatusNotFound, "not found"},
		{"internal error", http.StatusInternalServerError, "server error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewClient(server.URL)
			_, err := client.Copy(context.Background(), "src", "dst", nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.body)
		})
	}
}

func TestClient_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.Copy(context.Background(), "src", "dst", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode response")
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Copy(ctx, "src", "dst", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context canceled")
}

func TestClient_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.Copy(ctx, "src", "dst", nil)
	require.Error(t, err)
}