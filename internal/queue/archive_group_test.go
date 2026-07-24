package queue

import (
	"context"
	"errors"
	"testing"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
	"grabarr/internal/models"
	"grabarr/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newArchiveTestQueue builds a queue with extraction enabled.
func newArchiveTestQueue(t *testing.T) *queue {
	t.Helper()

	repo := testutil.SetupTestDB(t)
	cfg := &config.Config{
		Extraction: config.ExtractionConfig{Enabled: true},
		Jobs:       config.JobsConfig{MaxRetries: 5},
	}

	q := New(repo, cfg, mocks.NewMockGatekeeper(t), mocks.NewMockNotifier(t))
	return q.(*queue)
}

// addCompletedArchiveJob inserts a completed download job belonging to group.
func addCompletedArchiveJob(t *testing.T, q *queue, group, name string) *models.Job {
	t.Helper()

	job := &models.Job{
		Name:       name,
		RemotePath: "/remote/" + name,
		LocalPath:  "/downloads/set/",
		Status:     models.JobStatusCompleted,
		MaxRetries: 5,
		Metadata: models.JobMetadata{
			ExtraFields: map[string]interface{}{"archive_group": group},
		},
	}
	require.NoError(t, q.repo.CreateJob(job))
	return job
}

// extractionJobsFor returns the extraction jobs recorded for a group.
func extractionJobsFor(t *testing.T, q *queue, group string) []*models.Job {
	t.Helper()

	all, err := q.repo.GetJobsByArchiveGroup(group)
	require.NoError(t, err)

	var out []*models.Job
	for _, j := range all {
		if j.IsExtractionJob() {
			out = append(out, j)
		}
	}
	return out
}

// A truncated or gapped volume set must not be extracted. Extracting one
// produces a partial media file and a permanently failed job.
func TestCheckArchiveGroupCompleteSkipsSetWithMissingVolume(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	addCompletedArchiveJob(t, q, group, "movie.rar")
	addCompletedArchiveJob(t, q, group, "movie.r00")
	last := addCompletedArchiveJob(t, q, group, "movie.r02") // r01 missing

	q.checkArchiveGroupComplete(group, last)

	assert.Empty(t, extractionJobsFor(t, q, group),
		"no extraction job should be created while volume r01 is missing")
}

func TestCheckArchiveGroupCompleteCreatesJobForContiguousSet(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	addCompletedArchiveJob(t, q, group, "movie.rar")
	addCompletedArchiveJob(t, q, group, "movie.r00")
	last := addCompletedArchiveJob(t, q, group, "movie.r01")

	q.checkArchiveGroupComplete(group, last)

	jobs := extractionJobsFor(t, q, group)
	require.Len(t, jobs, 1, "a contiguous set should produce exactly one extraction job")
	assert.Equal(t, "/downloads/set/movie.rar", jobs[0].RemotePath,
		"extraction should start from the .rar first part")
}

func TestCheckArchiveGroupCompleteSkipsWhenADownloadIsStillRunning(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	addCompletedArchiveJob(t, q, group, "movie.rar")
	last := addCompletedArchiveJob(t, q, group, "movie.r00")

	pending := &models.Job{
		Name:       "movie.r01",
		RemotePath: "/remote/movie.r01",
		LocalPath:  "/downloads/set/",
		Status:     models.JobStatusRunning,
		MaxRetries: 5,
		Metadata: models.JobMetadata{
			ExtraFields: map[string]interface{}{"archive_group": group},
		},
	}
	require.NoError(t, q.repo.CreateJob(pending))

	q.checkArchiveGroupComplete(group, last)

	assert.Empty(t, extractionJobsFor(t, q, group),
		"no extraction job while a volume is still downloading")
}

func TestCheckArchiveGroupCompleteHandlesStandaloneZip(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/archive"

	last := addCompletedArchiveJob(t, q, group, "archive.zip")

	q.checkArchiveGroupComplete(group, last)

	jobs := extractionJobsFor(t, q, group)
	require.Len(t, jobs, 1, "a standalone zip should extract")
	assert.Equal(t, "/downloads/set/archive.zip", jobs[0].RemotePath)
}

func TestCheckArchiveGroupCompleteIsIdempotent(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	addCompletedArchiveJob(t, q, group, "movie.rar")
	last := addCompletedArchiveJob(t, q, group, "movie.r00")

	q.checkArchiveGroupComplete(group, last)
	q.checkArchiveGroupComplete(group, last)

	assert.Len(t, extractionJobsFor(t, q, group), 1,
		"repeated completions must not stack extraction jobs")
}

// fakeVolumeLister stands in for the seedbox listing.
type fakeVolumeLister struct {
	volumes []string
	err     error
	calls   int
}

func (f *fakeVolumeLister) ListArchiveVolumes(_ context.Context, _ string) ([]string, error) {
	f.calls++
	return f.volumes, f.err
}

// The bug that motivated this: a scan that catches a torrent mid-arrival sees a
// contiguous *prefix* of the volumes (.rar…r05 of a 40-volume set). Nothing
// local can tell that apart from a complete set, so the source is authoritative.
func TestCheckArchiveGroupCompleteDefersWhenRemoteHasMoreVolumes(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	addCompletedArchiveJob(t, q, group, "movie.rar")
	last := addCompletedArchiveJob(t, q, group, "movie.r00")

	lister := &fakeVolumeLister{volumes: []string{"movie.rar", "movie.r00", "movie.r01", "movie.r02"}}
	q.SetRemoteVolumeLister(lister)

	q.checkArchiveGroupComplete(group, last)

	assert.Equal(t, 1, lister.calls, "the remote should be consulted")
	assert.Empty(t, extractionJobsFor(t, q, group),
		"must not extract a contiguous prefix while the source still has more volumes")
}

func TestCheckArchiveGroupCompleteExtractsWhenRemoteMatchesLocal(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	addCompletedArchiveJob(t, q, group, "movie.rar")
	last := addCompletedArchiveJob(t, q, group, "movie.r00")

	q.SetRemoteVolumeLister(&fakeVolumeLister{volumes: []string{"movie.rar", "movie.r00"}})

	q.checkArchiveGroupComplete(group, last)

	assert.Len(t, extractionJobsFor(t, q, group), 1,
		"a set matching the source should extract")
}

// If the source cannot be reached we proceed rather than stall: nothing else
// re-triggers this check, and a genuinely short set now fails retryably.
func TestCheckArchiveGroupCompleteProceedsWhenRemoteListingFails(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	addCompletedArchiveJob(t, q, group, "movie.rar")
	last := addCompletedArchiveJob(t, q, group, "movie.r00")

	q.SetRemoteVolumeLister(&fakeVolumeLister{err: errors.New("ssh unreachable")})

	q.checkArchiveGroupComplete(group, last)

	assert.Len(t, extractionJobsFor(t, q, group), 1,
		"an unreachable source should not permanently block extraction")
}

func TestCheckArchiveGroupCompleteWithoutListerFallsBackToLocalCheck(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	addCompletedArchiveJob(t, q, group, "movie.rar")
	last := addCompletedArchiveJob(t, q, group, "movie.r00")

	q.checkArchiveGroupComplete(group, last)

	assert.Len(t, extractionJobsFor(t, q, group), 1,
		"with no lister configured the local contiguity check still applies")
}

// The extraction job is what finally yields an importable media file, so it has
// to carry the category forward or the import notification cannot be routed.
func TestCheckArchiveGroupCompleteCarriesCategoryToExtractionJob(t *testing.T) {
	q := newArchiveTestQueue(t)
	const group = "/downloads/set/movie"

	first := &models.Job{
		Name:       "movie.rar",
		RemotePath: "/remote/movie.rar",
		LocalPath:  "/downloads/set/",
		Status:     models.JobStatusCompleted,
		MaxRetries: 5,
		Metadata: models.JobMetadata{
			Category:    "dp-movies",
			ExtraFields: map[string]interface{}{"archive_group": group},
		},
	}
	require.NoError(t, q.repo.CreateJob(first))

	q.checkArchiveGroupComplete(group, first)

	jobs := extractionJobsFor(t, q, group)
	require.Len(t, jobs, 1)
	assert.Equal(t, "dp-movies", jobs[0].Metadata.Category,
		"extraction job should inherit the category of the archive it unpacks")
}
