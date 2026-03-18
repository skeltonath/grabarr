package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
	"grabarr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ---- helpers ----

func int64Ptr(v int64) *int64 { return &v }

func setupRemoteFileHandlers(t *testing.T) (*Handlers, *mocks.MockRemoteFileRepo, *mocks.MockJobQueue) {
	t.Helper()
	repo := mocks.NewMockRemoteFileRepo(t)
	queue := mocks.NewMockJobQueue(t)
	gk := mocks.NewMockGatekeeper(t)
	cfg := &config.Config{
		Sync: config.SyncConfig{
			WatchedPaths: []config.WatchedPath{
				{RemotePath: "/seedbox/dp/", LocalPath: ""},
			},
		},
		Downloads: config.DownloadsConfig{LocalPath: "/downloads/"},
		Jobs:      config.JobsConfig{MaxRetries: 3},
	}
	h := NewHandlers(queue, gk, cfg, repo, nil)
	return h, repo, queue
}

// ---- GetRemoteFileTree tests ----

func TestGetRemoteFileTree_Empty(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)
	_ = queue

	repo.EXPECT().GetRemoteFiles(mock.AnythingOfType("models.RemoteFileFilter")).
		Return([]*models.RemoteFile{}, nil).Once()

	req := httptest.NewRequest("GET", "/api/v1/remote-files/tree", nil)
	rec := httptest.NewRecorder()
	h.GetRemoteFileTree(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Success)

	data := resp.Data.(map[string]interface{})
	roots := data["roots"].([]interface{})
	assert.Empty(t, roots)
}

func TestGetRemoteFileTree_SingleFile(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)

	file := &models.RemoteFile{
		ID:          1,
		RemotePath:  "/seedbox/dp/movie.mkv",
		Name:        "movie.mkv",
		Size:        1073741824, // 1 GB
		Status:      models.FileStatusOnSeedbox,
		WatchedPath: "/seedbox/dp/",
	}

	repo.EXPECT().GetRemoteFiles(mock.AnythingOfType("models.RemoteFileFilter")).
		Return([]*models.RemoteFile{file}, nil).Once()
	// No job fetching needed (JobID is nil)
	_ = queue

	req := httptest.NewRequest("GET", "/api/v1/remote-files/tree", nil)
	rec := httptest.NewRecorder()
	h.GetRemoteFileTree(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Success)

	data := resp.Data.(map[string]interface{})
	roots := data["roots"].([]interface{})
	require.Len(t, roots, 1)

	root := roots[0].(map[string]interface{})
	assert.Equal(t, "/seedbox/dp/", root["watched_path"])
	assert.Equal(t, "folder", root["type"])

	stats := root["stats"].(map[string]interface{})
	assert.Equal(t, float64(1), stats["file_count"])
	assert.Equal(t, float64(1073741824), stats["total_bytes"])
	assert.Equal(t, float64(1), stats["on_seedbox"])
	assert.Equal(t, float64(0), stats["downloaded"])

	// The file should appear as direct child of root
	children := root["children"].([]interface{})
	require.Len(t, children, 1)
	child := children[0].(map[string]interface{})
	assert.Equal(t, "file", child["type"])
	assert.Equal(t, "movie.mkv", child["name"])
}

func TestGetRemoteFileTree_NestedFolders(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)
	_ = queue

	files := []*models.RemoteFile{
		{ID: 1, RemotePath: "/seedbox/dp/ShowA/S01/E01.mkv", Name: "E01.mkv", Size: 1000, Status: models.FileStatusDownloaded, WatchedPath: "/seedbox/dp/"},
		{ID: 2, RemotePath: "/seedbox/dp/ShowA/S01/E02.mkv", Name: "E02.mkv", Size: 1000, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
		{ID: 3, RemotePath: "/seedbox/dp/ShowA/S02/E01.mkv", Name: "E01.mkv", Size: 2000, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
	}

	repo.EXPECT().GetRemoteFiles(mock.AnythingOfType("models.RemoteFileFilter")).
		Return(files, nil).Once()

	req := httptest.NewRequest("GET", "/api/v1/remote-files/tree", nil)
	rec := httptest.NewRecorder()
	h.GetRemoteFileTree(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Success)

	data := resp.Data.(map[string]interface{})
	roots := data["roots"].([]interface{})
	require.Len(t, roots, 1)
	root := roots[0].(map[string]interface{})

	// Root stats should aggregate all 3 files (including grandchildren)
	stats := root["stats"].(map[string]interface{})
	assert.Equal(t, float64(3), stats["file_count"])
	assert.Equal(t, float64(4000), stats["total_bytes"])
	assert.Equal(t, float64(1), stats["downloaded"])
	assert.Equal(t, float64(2), stats["on_seedbox"])

	// Root should have one child folder: ShowA
	children := root["children"].([]interface{})
	require.Len(t, children, 1)
	showA := children[0].(map[string]interface{})
	assert.Equal(t, "ShowA", showA["name"])
	assert.Equal(t, "folder", showA["type"])

	// ShowA stats should also aggregate grandchildren
	showAStats := showA["stats"].(map[string]interface{})
	assert.Equal(t, float64(3), showAStats["file_count"])
}

func TestGetRemoteFileTree_MixedStatuses(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)

	jobID := int64(99)
	files := []*models.RemoteFile{
		{ID: 1, RemotePath: "/seedbox/dp/a.mkv", Name: "a.mkv", Size: 100, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
		{ID: 2, RemotePath: "/seedbox/dp/b.mkv", Name: "b.mkv", Size: 200, Status: models.FileStatusQueued, WatchedPath: "/seedbox/dp/"},
		{ID: 3, RemotePath: "/seedbox/dp/c.mkv", Name: "c.mkv", Size: 300, Status: models.FileStatusDownloading, JobID: &jobID, WatchedPath: "/seedbox/dp/"},
		{ID: 4, RemotePath: "/seedbox/dp/d.mkv", Name: "d.mkv", Size: 400, Status: models.FileStatusDownloaded, WatchedPath: "/seedbox/dp/"},
		{ID: 5, RemotePath: "/seedbox/dp/e.mkv", Name: "e.mkv", Size: 500, Status: models.FileStatusIgnored, WatchedPath: "/seedbox/dp/"},
	}

	repo.EXPECT().GetRemoteFiles(mock.AnythingOfType("models.RemoteFileFilter")).
		Return(files, nil).Once()

	queue.EXPECT().GetJob(jobID).Return(&models.Job{
		ID:     jobID,
		Status: models.JobStatusRunning,
		Progress: models.JobProgress{
			Percentage:    55.0,
			TransferSpeed: 1024 * 1024 * 10,
		},
	}, nil).Once()

	req := httptest.NewRequest("GET", "/api/v1/remote-files/tree", nil)
	rec := httptest.NewRecorder()
	h.GetRemoteFileTree(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Success)

	data := resp.Data.(map[string]interface{})
	roots := data["roots"].([]interface{})
	root := roots[0].(map[string]interface{})
	stats := root["stats"].(map[string]interface{})

	assert.Equal(t, float64(5), stats["file_count"])
	assert.Equal(t, float64(1), stats["on_seedbox"])
	assert.Equal(t, float64(1), stats["queued"])
	assert.Equal(t, float64(1), stats["downloading"])
	assert.Equal(t, float64(1), stats["downloaded"])
	assert.Equal(t, float64(1), stats["ignored"])
	assert.Equal(t, float64(0), stats["failed"])
}

func TestGetRemoteFileTree_FailedJobCountedAsFailed(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)

	jobID := int64(55)
	// File status is on_seedbox (scanner reverted it after failure), but job is failed
	file := &models.RemoteFile{
		ID: 1, RemotePath: "/seedbox/dp/a.mkv", Name: "a.mkv", Size: 100,
		Status: models.FileStatusOnSeedbox, JobID: &jobID, WatchedPath: "/seedbox/dp/",
	}

	repo.EXPECT().GetRemoteFiles(mock.AnythingOfType("models.RemoteFileFilter")).
		Return([]*models.RemoteFile{file}, nil).Once()
	queue.EXPECT().GetJob(jobID).Return(&models.Job{
		ID:           jobID,
		Status:       models.JobStatusFailed,
		ErrorMessage: "connection timeout",
	}, nil).Once()

	req := httptest.NewRequest("GET", "/api/v1/remote-files/tree", nil)
	rec := httptest.NewRecorder()
	h.GetRemoteFileTree(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	data := resp.Data.(map[string]interface{})
	roots := data["roots"].([]interface{})
	root := roots[0].(map[string]interface{})
	stats := root["stats"].(map[string]interface{})

	assert.Equal(t, float64(1), stats["failed"])
	assert.Equal(t, float64(0), stats["on_seedbox"])
}

func TestGetRemoteFileTree_MultipleWatchedPaths(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)
	_ = queue

	files := []*models.RemoteFile{
		{ID: 1, RemotePath: "/seedbox/dp/a.mkv", Name: "a.mkv", Size: 100, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
		{ID: 2, RemotePath: "/seedbox/other/b.mkv", Name: "b.mkv", Size: 200, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/other/"},
	}

	repo.EXPECT().GetRemoteFiles(mock.AnythingOfType("models.RemoteFileFilter")).
		Return(files, nil).Once()

	req := httptest.NewRequest("GET", "/api/v1/remote-files/tree", nil)
	rec := httptest.NewRecorder()
	h.GetRemoteFileTree(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))

	data := resp.Data.(map[string]interface{})
	roots := data["roots"].([]interface{})
	assert.Len(t, roots, 2)

	watched := []string{
		roots[0].(map[string]interface{})["watched_path"].(string),
		roots[1].(map[string]interface{})["watched_path"].(string),
	}
	assert.Contains(t, watched, "/seedbox/dp/")
	assert.Contains(t, watched, "/seedbox/other/")
}

// ---- QueueFolder tests ----

func TestQueueFolder_Success(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)

	now := time.Now()
	files := []*models.RemoteFile{
		{ID: 1, RemotePath: "/seedbox/dp/ShowA/E01.mkv", Name: "E01.mkv", Size: 1000, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
		{ID: 2, RemotePath: "/seedbox/dp/ShowA/E02.mkv", Name: "E02.mkv", Size: 2000, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
	}
	_ = now

	repo.EXPECT().GetRemoteFilesByPathPrefix("/seedbox/dp/", "/ShowA").
		Return(files, nil).Once()

	queue.EXPECT().Enqueue(mock.AnythingOfType("*models.Job")).
		RunAndReturn(func(job *models.Job) error { job.ID = 10; return nil }).Once()
	repo.EXPECT().LinkRemoteFileToJob(int64(1), int64(10), models.FileStatusQueued).
		Return(nil).Once()

	queue.EXPECT().Enqueue(mock.AnythingOfType("*models.Job")).
		RunAndReturn(func(job *models.Job) error { job.ID = 11; return nil }).Once()
	repo.EXPECT().LinkRemoteFileToJob(int64(2), int64(11), models.FileStatusQueued).
		Return(nil).Once()

	body, _ := json.Marshal(map[string]string{"watched_path": "/seedbox/dp/", "folder_path": "/ShowA"})
	req := httptest.NewRequest("POST", "/api/v1/remote-files/queue-folder", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.QueueFolder(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Success)

	data := resp.Data.(map[string]interface{})
	assert.Equal(t, float64(2), data["queued"])
	assert.Equal(t, float64(0), data["failed"])
}

func TestQueueFolder_SkipsAlreadyQueued(t *testing.T) {
	// GetRemoteFilesByPathPrefix only returns on_seedbox files, so no skipping needed —
	// this test verifies an empty result returns success with 0 queued.
	h, repo, _ := setupRemoteFileHandlers(t)

	repo.EXPECT().GetRemoteFilesByPathPrefix("/seedbox/dp/", "/ShowA").
		Return([]*models.RemoteFile{}, nil).Once()

	body, _ := json.Marshal(map[string]string{"watched_path": "/seedbox/dp/", "folder_path": "/ShowA"})
	req := httptest.NewRequest("POST", "/api/v1/remote-files/queue-folder", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.QueueFolder(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Success)
	data := resp.Data.(map[string]interface{})
	assert.Equal(t, float64(0), data["queued"])
}

func TestQueueFolder_PathTraversalFolderPath(t *testing.T) {
	h, _, _ := setupRemoteFileHandlers(t)

	body, _ := json.Marshal(map[string]string{"watched_path": "/seedbox/dp/", "folder_path": "/../etc"})
	req := httptest.NewRequest("POST", "/api/v1/remote-files/queue-folder", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.QueueFolder(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.False(t, resp.Success)
}

func TestQueueFolder_PathTraversalWatchedPath(t *testing.T) {
	h, _, _ := setupRemoteFileHandlers(t)

	body, _ := json.Marshal(map[string]string{"watched_path": "/seedbox/../etc/", "folder_path": "/foo"})
	req := httptest.NewRequest("POST", "/api/v1/remote-files/queue-folder", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.QueueFolder(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestQueueFolder_MissingFields(t *testing.T) {
	h, _, _ := setupRemoteFileHandlers(t)

	body, _ := json.Marshal(map[string]string{"watched_path": "/seedbox/dp/"})
	req := httptest.NewRequest("POST", "/api/v1/remote-files/queue-folder", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.QueueFolder(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestQueueFolder_PartialFailure(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)

	files := []*models.RemoteFile{
		{ID: 1, RemotePath: "/seedbox/dp/ShowA/E01.mkv", Name: "E01.mkv", Size: 1000, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
		{ID: 2, RemotePath: "/seedbox/dp/ShowA/E02.mkv", Name: "E02.mkv", Size: 2000, Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
	}

	repo.EXPECT().GetRemoteFilesByPathPrefix("/seedbox/dp/", "/ShowA").
		Return(files, nil).Once()

	// First file succeeds
	queue.EXPECT().Enqueue(mock.AnythingOfType("*models.Job")).
		RunAndReturn(func(job *models.Job) error { job.ID = 10; return nil }).Once()
	repo.EXPECT().LinkRemoteFileToJob(int64(1), int64(10), models.FileStatusQueued).
		Return(nil).Once()

	// Second file fails to enqueue
	queue.EXPECT().Enqueue(mock.AnythingOfType("*models.Job")).
		Return(errors.New("queue full")).Once()

	body, _ := json.Marshal(map[string]string{"watched_path": "/seedbox/dp/", "folder_path": "/ShowA"})
	req := httptest.NewRequest("POST", "/api/v1/remote-files/queue-folder", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.QueueFolder(rec, req)

	// 1 queued, 1 failed → still 200 (partial success)
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Success)
	data := resp.Data.(map[string]interface{})
	assert.Equal(t, float64(1), data["queued"])
	assert.Equal(t, float64(1), data["failed"])
}
