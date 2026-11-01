package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/skeltonath/grabarr/internal/config"
	"github.com/skeltonath/grabarr/internal/mocks"
	"github.com/skeltonath/grabarr/internal/models"

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
		Remotes: []config.RemoteConfig{
			{
				Name:    "whatbox",
				SSHHost: "example.com",
				SSHUser: "testuser",
				WatchedPaths: []config.WatchedPath{
					{RemotePath: "/seedbox/dp/", LocalPath: ""},
				},
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

// Two watched paths on one remote used to show as two identical "whatbox"
// sources. Labels tell them apart, and sources keep their config order.
func TestGetRemoteFileTree_LabelsAndConfigOrder(t *testing.T) {
	repo := mocks.NewMockRemoteFileRepo(t)
	cfg := &config.Config{
		Remotes: []config.RemoteConfig{{
			Name: "whatbox",
			WatchedPaths: []config.WatchedPath{
				{Label: "Video", RemotePath: "/seedbox/dp/"},
				{Label: "Books", RemotePath: "/seedbox/books/"},
				{RemotePath: "/seedbox/other/"},
			},
		}},
	}
	h := NewHandlers(mocks.NewMockJobQueue(t), mocks.NewMockGatekeeper(t), cfg, repo, nil)

	repo.EXPECT().GetRemoteFiles(mock.AnythingOfType("models.RemoteFileFilter")).Return([]*models.RemoteFile{
		{ID: 1, RemotePath: "/seedbox/books/a.epub", Name: "a.epub", WatchedPath: "/seedbox/books/", Status: models.FileStatusOnSeedbox},
		{ID: 2, RemotePath: "/seedbox/other/c.mkv", Name: "c.mkv", WatchedPath: "/seedbox/other/", Status: models.FileStatusOnSeedbox},
		{ID: 3, RemotePath: "/old/gone.mkv", Name: "gone.mkv", WatchedPath: "/old/", Status: models.FileStatusOnSeedbox},
		{ID: 4, RemotePath: "/seedbox/dp/b.mkv", Name: "b.mkv", WatchedPath: "/seedbox/dp/", Status: models.FileStatusOnSeedbox},
	}, nil).Once()

	rec := httptest.NewRecorder()
	h.GetRemoteFileTree(rec, httptest.NewRequest("GET", "/api/v1/remote-files/tree", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data struct {
			ScannedAt *time.Time `json:"scanned_at"`
			Roots     []struct {
				WatchedPath string `json:"watched_path"`
				Name        string `json:"name"`
				Remote      string `json:"remote"`
			} `json:"roots"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Nil(t, resp.Data.ScannedAt, "no scanner, so no scan time")

	var got []string
	for _, r := range resp.Data.Roots {
		got = append(got, r.Name+"|"+r.Remote+"|"+r.WatchedPath)
	}
	assert.Equal(t, []string{
		"Video|whatbox|/seedbox/dp/",
		"Books|whatbox|/seedbox/books/",
		"whatbox|whatbox|/seedbox/other/", // no label: the remote's name
		"old||/old/",                      // no longer configured: last, named after its folder
	}, got)
}

// Queueing from the web UI, a single file or a whole folder, uses the watched
// path's priority, as the scanner does; files from a path no longer in the
// config get 0.
func TestQueue_UsesWatchedPathPriority(t *testing.T) {
	newHandlers := func(t *testing.T) (*Handlers, *mocks.MockRemoteFileRepo, *mocks.MockJobQueue) {
		repo := mocks.NewMockRemoteFileRepo(t)
		queue := mocks.NewMockJobQueue(t)
		cfg := &config.Config{
			Remotes: []config.RemoteConfig{{Name: "whatbox", WatchedPaths: []config.WatchedPath{
				{RemotePath: "/seedbox/dp/"},
				{RemotePath: "/seedbox/books/", Priority: 5},
			}}},
			Downloads: config.DownloadsConfig{LocalPath: "/downloads/"},
		}
		return NewHandlers(queue, mocks.NewMockGatekeeper(t), cfg, repo, nil), repo, queue
	}

	for _, tc := range []struct {
		watched string
		want    int
	}{
		{"/seedbox/books/", 5},
		{"/seedbox/dp/", 0},
		{"/seedbox/gone/", 0},
	} {
		t.Run("file "+tc.watched, func(t *testing.T) {
			h, repo, queue := newHandlers(t)
			repo.EXPECT().GetRemoteFile(int64(1)).Return(&models.RemoteFile{
				ID: 1, RemotePath: tc.watched + "x/a.epub", Name: "a.epub", Status: models.FileStatusOnSeedbox, WatchedPath: tc.watched,
			}, nil).Once()
			var got *models.Job
			queue.EXPECT().Enqueue(mock.AnythingOfType("*models.Job")).
				RunAndReturn(func(j *models.Job) error { got = j; j.ID = 9; return nil }).Once()
			repo.EXPECT().LinkRemoteFileToJob(int64(1), int64(9), models.FileStatusQueued).Return(nil).Once()

			rec := httptest.NewRecorder()
			h.QueueRemoteFile(rec, withURLParam(httptest.NewRequest("POST", "/api/v1/remote-files/1/queue", nil), "id", "1"))
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			assert.Equal(t, tc.want, got.Priority)
		})

		t.Run("folder "+tc.watched, func(t *testing.T) {
			h, repo, queue := newHandlers(t)
			repo.EXPECT().GetRemoteFilesByPathPrefix(tc.watched, "/x").Return([]*models.RemoteFile{
				{ID: 1, RemotePath: tc.watched + "x/a.epub", Name: "a.epub", Status: models.FileStatusOnSeedbox, WatchedPath: tc.watched},
			}, nil).Once()
			var got *models.Job
			queue.EXPECT().Enqueue(mock.AnythingOfType("*models.Job")).
				RunAndReturn(func(j *models.Job) error { got = j; j.ID = 9; return nil }).Once()
			repo.EXPECT().LinkRemoteFileToJob(int64(1), int64(9), models.FileStatusQueued).Return(nil).Once()

			body, _ := json.Marshal(map[string]string{"watched_path": tc.watched, "folder_path": "/x"})
			rec := httptest.NewRecorder()
			h.QueueFolder(rec, httptest.NewRequest("POST", "/api/v1/remote-files/queue-folder", bytes.NewReader(body)))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, tc.want, got.Priority)
		})
	}
}

// ---- Download now ----

func TestQueueRemoteFile_Now(t *testing.T) {
	setup := func(t *testing.T) (*Handlers, *mocks.MockJobQueue, **models.Job) {
		h, repo, queue := setupRemoteFileHandlers(t)
		repo.EXPECT().GetRemoteFile(int64(1)).Return(&models.RemoteFile{
			ID: 1, RemotePath: "/seedbox/dp/x/a.mkv", Name: "a.mkv", Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/",
		}, nil).Once()
		queue.EXPECT().TopPriority().Return(6, nil).Once()
		var got *models.Job
		queue.EXPECT().Enqueue(mock.AnythingOfType("*models.Job")).
			RunAndReturn(func(j *models.Job) error { got = j; j.ID = 9; return nil }).Once()
		repo.EXPECT().LinkRemoteFileToJob(int64(1), int64(9), models.FileStatusQueued).Return(nil).Once()
		return h, queue, &got
	}
	call := func(h *Handlers) map[string]interface{} {
		rec := httptest.NewRecorder()
		h.QueueRemoteFile(rec, withURLParam(httptest.NewRequest("POST", "/api/v1/remote-files/1/queue?now=true", nil), "id", "1"))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var resp APIResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		return resp.Data.(map[string]interface{})
	}

	t.Run("starts first in line", func(t *testing.T) {
		h, queue, got := setup(t)
		queue.EXPECT().StartNow(int64(9)).Return(nil).Once()
		data := call(h)
		assert.Equal(t, 6, (*got).Priority)
		assert.Equal(t, true, data["started"])
	})

	t.Run("stays first in line when it can't start", func(t *testing.T) {
		h, queue, got := setup(t)
		queue.EXPECT().StartNow(int64(9)).Return(errors.New("can't start now: cache disk usage too high")).Once()
		data := call(h)
		assert.Equal(t, 6, (*got).Priority)
		assert.Equal(t, false, data["started"])
		assert.Equal(t, "can't start now: cache disk usage too high", data["not_started"])
	})
}

func TestQueueFolder_Now(t *testing.T) {
	h, repo, queue := setupRemoteFileHandlers(t)
	repo.EXPECT().GetRemoteFilesByPathPrefix("/seedbox/dp/", "/ShowA").Return([]*models.RemoteFile{
		{ID: 1, RemotePath: "/seedbox/dp/ShowA/E01.mkv", Name: "E01.mkv", Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
		{ID: 2, RemotePath: "/seedbox/dp/ShowA/E02.mkv", Name: "E02.mkv", Status: models.FileStatusOnSeedbox, WatchedPath: "/seedbox/dp/"},
	}, nil).Once()
	queue.EXPECT().TopPriority().Return(3, nil).Once()
	var priorities []int
	nextID := int64(10)
	queue.EXPECT().Enqueue(mock.AnythingOfType("*models.Job")).
		RunAndReturn(func(j *models.Job) error {
			priorities = append(priorities, j.Priority)
			j.ID = nextID
			nextID++
			return nil
		}).Twice()
	repo.EXPECT().LinkRemoteFileToJob(mock.Anything, mock.Anything, models.FileStatusQueued).Return(nil).Twice()
	queue.EXPECT().StartNow(int64(10)).Return(nil).Once() // only the first

	body, _ := json.Marshal(map[string]any{"watched_path": "/seedbox/dp/", "folder_path": "/ShowA", "now": true})
	rec := httptest.NewRecorder()
	h.QueueFolder(rec, httptest.NewRequest("POST", "/api/v1/remote-files/queue-folder", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp APIResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	data := resp.Data.(map[string]interface{})
	assert.Equal(t, float64(2), data["queued"])
	assert.Equal(t, true, data["started"])
	assert.Equal(t, []int{3, 3}, priorities)
}
