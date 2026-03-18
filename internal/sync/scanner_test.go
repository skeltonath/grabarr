package sync

import (
	"regexp"
	"testing"
	"time"

	"grabarr/internal/interfaces"
	"grabarr/internal/mocks"
	"grabarr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stubScannerRepo is a minimal in-memory ScannerRepo for unit tests.
type stubScannerRepo struct {
	staleWithJobs []*models.RemoteFile
	staleErr      error
}

func (s *stubScannerRepo) UpsertRemoteFile(_ *models.RemoteFile) error { return nil }
func (s *stubScannerRepo) GetRemoteFilesLinkedToJobs() ([]*models.RemoteFile, error) {
	return nil, nil
}
func (s *stubScannerRepo) GetRemoteFileByPath(_ string) (*models.RemoteFile, error)  { return nil, nil }
func (s *stubScannerRepo) UpdateRemoteFileStatus(_ int64, _ models.FileStatus) error { return nil }
func (s *stubScannerRepo) LinkRemoteFileToJob(_ int64, _ int64, _ models.FileStatus) error {
	return nil
}
func (s *stubScannerRepo) GetStaleRemoteFilesWithJobs(_ string, _ time.Time) ([]*models.RemoteFile, error) {
	return s.staleWithJobs, s.staleErr
}
func (s *stubScannerRepo) DeleteStaleRemoteFiles(_ string, _ time.Time) error { return nil }

// compile-time check that stubScannerRepo satisfies ScannerRepo
var _ ScannerRepo = (*stubScannerRepo)(nil)

// compile-time check that mocks.MockJobQueue satisfies interfaces.JobQueue
var _ interfaces.JobQueue = (*mocks.MockJobQueue)(nil)

func TestParseSSHFindOutput(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		watchedPath string
		wantCount   int
		wantFiles   []struct {
			name      string
			size      int64
			extension string
			path      string
		}
	}{
		{
			name:        "single file",
			output:      "/home/user/downloads/movie.mkv\t4294967296\n",
			watchedPath: "/home/user/downloads/",
			wantCount:   1,
			wantFiles: []struct {
				name      string
				size      int64
				extension string
				path      string
			}{
				{name: "movie.mkv", size: 4294967296, extension: "mkv", path: "/home/user/downloads/movie.mkv"},
			},
		},
		{
			name: "multiple files",
			output: "/home/user/downloads/movie.mkv\t4294967296\n" +
				"/home/user/downloads/subtitle.srt\t12345\n",
			watchedPath: "/home/user/downloads/",
			wantCount:   2,
		},
		{
			name:        "empty output",
			output:      "",
			watchedPath: "/home/user/downloads/",
			wantCount:   0,
		},
		{
			name:        "whitespace only",
			output:      "   \n\n  ",
			watchedPath: "/home/user/downloads/",
			wantCount:   0,
		},
		{
			name:        "malformed line skipped",
			output:      "/home/user/downloads/movie.mkv\t4294967296\nbad_line_no_tab\n",
			watchedPath: "/home/user/downloads/",
			wantCount:   1,
		},
		{
			name:        "file with no extension",
			output:      "/home/user/downloads/noext\t1024\n",
			watchedPath: "/home/user/downloads/",
			wantCount:   1,
			wantFiles: []struct {
				name      string
				size      int64
				extension string
				path      string
			}{
				{name: "noext", size: 1024, extension: "", path: "/home/user/downloads/noext"},
			},
		},
		{
			name:        "invalid size defaults to zero",
			output:      "/home/user/downloads/movie.mkv\tbadsize\n",
			watchedPath: "/home/user/downloads/",
			wantCount:   1,
			wantFiles: []struct {
				name      string
				size      int64
				extension string
				path      string
			}{
				{name: "movie.mkv", size: 0, extension: "mkv", path: "/home/user/downloads/movie.mkv"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := parseSSHFindOutput(tt.output, tt.watchedPath, nil)
			require.Len(t, files, tt.wantCount)

			for i, want := range tt.wantFiles {
				assert.Equal(t, want.name, files[i].Name)
				assert.Equal(t, want.size, files[i].Size)
				assert.Equal(t, want.extension, files[i].Extension)
				assert.Equal(t, want.path, files[i].RemotePath)
				assert.Equal(t, tt.watchedPath, files[i].WatchedPath)
				assert.Equal(t, models.FileStatusOnSeedbox, files[i].Status)
			}
		})
	}
}

func TestParseSSHFindOutput_ExcludePatterns(t *testing.T) {
	output := "/home/user/downloads/movie.mkv\t1073741824\n" +
		"/home/user/downloads/movie.sample.mkv\t5242880\n" +
		"/home/user/downloads/Sample.mkv\t5242880\n" +
		"/home/user/downloads/subtitle.srt\t12345\n"

	res := []*regexp.Regexp{regexp.MustCompile(`(?i)\.sample\.`), regexp.MustCompile(`(?i)^sample\.`)}
	files := parseSSHFindOutput(output, "/home/user/downloads/", res)

	require.Len(t, files, 2)
	assert.Equal(t, "movie.mkv", files[0].Name)
	assert.Equal(t, "subtitle.srt", files[1].Name)
}

func TestCompilePatterns(t *testing.T) {
	res, err := compilePatterns([]string{`(?i)\.sample\.`, `(?i)^sample\.`})
	require.NoError(t, err)
	assert.Len(t, res, 2)

	_, err = compilePatterns([]string{`[invalid`})
	assert.Error(t, err)
}

func TestMatchesAny(t *testing.T) {
	res := []*regexp.Regexp{regexp.MustCompile(`(?i)\.sample\.`)}

	assert.True(t, matchesAny("movie.sample.mkv", res))
	assert.True(t, matchesAny("movie.SAMPLE.mkv", res))
	assert.False(t, matchesAny("movie.mkv", res))
	assert.False(t, matchesAny("movie.mkv", nil))
}

func TestRemoteFileStatusFromJob(t *testing.T) {
	tests := []struct {
		jobStatus  models.JobStatus
		wantStatus models.FileStatus
	}{
		{models.JobStatusQueued, models.FileStatusQueued},
		{models.JobStatusPending, models.FileStatusQueued},
		{models.JobStatusRunning, models.FileStatusDownloading},
		{models.JobStatusCompleted, models.FileStatusDownloaded},
		{models.JobStatusFailed, models.FileStatusOnSeedbox},
		{models.JobStatusCancelled, models.FileStatusOnSeedbox},
	}

	for _, tt := range tests {
		t.Run(string(tt.jobStatus), func(t *testing.T) {
			got := remoteFileStatusFromJob(tt.jobStatus)
			assert.Equal(t, tt.wantStatus, got)
		})
	}
}

func TestParseSSHFindOutput_LastSeenAt(t *testing.T) {
	before := time.Now()
	files := parseSSHFindOutput("/home/user/file.mkv\t1024\n", "/home/user/", nil)
	after := time.Now()

	require.Len(t, files, 1)
	assert.True(t, files[0].LastSeenAt.After(before) || files[0].LastSeenAt.Equal(before))
	assert.True(t, files[0].LastSeenAt.Before(after) || files[0].LastSeenAt.Equal(after))
}

func TestCancelJobsForStaleFiles(t *testing.T) {
	jobID := int64(42)

	t.Run("queued job is cancelled", func(t *testing.T) {
		rf := &models.RemoteFile{
			ID:         1,
			RemotePath: "/seedbox/movie.mkv",
			JobID:      &jobID,
		}
		repo := &stubScannerRepo{staleWithJobs: []*models.RemoteFile{rf}}
		q := mocks.NewMockJobQueue(t)
		q.On("GetJob", jobID).Return(&models.Job{ID: jobID, Status: models.JobStatusQueued}, nil)
		q.On("CancelJob", jobID).Return(nil)

		s := &Scanner{repo: repo, queue: q}
		s.cancelJobsForStaleFiles("/seedbox/", time.Now())

		q.AssertCalled(t, "CancelJob", jobID)
	})

	t.Run("completed job is left alone", func(t *testing.T) {
		rf := &models.RemoteFile{
			ID:         2,
			RemotePath: "/seedbox/movie.mkv",
			JobID:      &jobID,
		}
		repo := &stubScannerRepo{staleWithJobs: []*models.RemoteFile{rf}}
		q := mocks.NewMockJobQueue(t)
		q.On("GetJob", jobID).Return(&models.Job{ID: jobID, Status: models.JobStatusCompleted}, nil)

		s := &Scanner{repo: repo, queue: q}
		s.cancelJobsForStaleFiles("/seedbox/", time.Now())

		q.AssertNotCalled(t, "CancelJob", mock.Anything)
	})

	t.Run("no linked job proceeds without error", func(t *testing.T) {
		repo := &stubScannerRepo{staleWithJobs: nil}
		q := mocks.NewMockJobQueue(t)

		s := &Scanner{repo: repo, queue: q}
		s.cancelJobsForStaleFiles("/seedbox/", time.Now())

		q.AssertNotCalled(t, "CancelJob", mock.Anything)
	})
}
