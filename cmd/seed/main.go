// Command seed builds a preview database with demo data for both watch paths:
// jobs in every state (queued, running with progress, failed, completed,
// cancelled) and seedbox files in every status, with a believable history.
// Nothing touches a seedbox; config.preview.yaml runs grabarr in preview mode
// against it.
//
//	go run ./cmd/seed -db data/preview.db -reset
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/skeltonath/grabarr/internal/models"
	"github.com/skeltonath/grabarr/internal/repository"
)

const (
	videoRoot = "/home/psychomanteum/downloads/completed/dp/"
	booksRoot = "/home/psychomanteum/downloads/completed/books/"
	videoDest = "/unraid/user/media/downloads/"
	booksDest = "/unraid/user/media/downloads/books/"

	gb = int64(1) << 30
	mb = int64(1) << 20
)

// item is one file on the seedbox and, unless it's only been seen, its job.
type item struct {
	root, rel string
	size      int64
	category  string
	file      models.FileStatus
	job       models.JobStatus // "" = no job
	age       time.Duration    // how long ago it was first seen
	progress  float64          // running jobs
	retries   int
	err       string
}

func items() []item {
	h := time.Hour
	var out []item
	add := func(it item) { out = append(out, it) }

	// A season pack mid-copy: some done, one running, the rest waiting.
	for ep := 1; ep <= 10; ep++ {
		it := item{root: videoRoot, rel: fmt.Sprintf("Severance.S02.1080p.ATVP.WEB-DL/Severance.S02E%02d.1080p.ATVP.WEB-DL.mkv", ep),
			size: 3*gb + int64(ep)*97*mb, category: "dp-tv", age: 6*h - time.Duration(ep)*10*time.Minute}
		switch {
		case ep <= 6:
			it.file, it.job = models.FileStatusDownloaded, models.JobStatusCompleted
		case ep == 7:
			it.file, it.job, it.progress = models.FileStatusDownloading, models.JobStatusRunning, 46
		default:
			it.file, it.job = models.FileStatusQueued, models.JobStatusQueued
		}
		add(it)
	}
	add(item{root: videoRoot, rel: "Severance.S02.1080p.ATVP.WEB-DL/Sample/severance.s02e01.sample.mkv",
		size: 48 * mb, category: "dp-tv", file: models.FileStatusIgnored, age: 6 * h})

	add(item{root: videoRoot, rel: "Suspiria.1977.2160p.UHD.BluRay.REMUX/Suspiria.1977.2160p.UHD.BluRay.REMUX.mkv",
		size: 61 * gb, category: "dp-movies", file: models.FileStatusDownloading, job: models.JobStatusRunning, progress: 12, age: 3 * h})
	add(item{root: videoRoot, rel: "The.Descent.2005.1080p.BluRay.x264/The.Descent.2005.1080p.BluRay.x264.mkv",
		size: 9 * gb, category: "dp-movies", file: models.FileStatusDownloaded, job: models.JobStatusCompleted, age: 26 * h})
	add(item{root: videoRoot, rel: "The.Descent.2005.1080p.BluRay.x264/The.Descent.2005.1080p.BluRay.x264.srt",
		size: 90 * 1024, category: "dp-movies", file: models.FileStatusDownloaded, job: models.JobStatusCompleted, age: 26 * h})
	add(item{root: videoRoot, rel: "Dune.Part.Two.2024.2160p.WEB-DL.DDP5.1.Atmos/Dune.Part.Two.2024.2160p.WEB-DL.mkv",
		size: 24 * gb, category: "dp-movies", file: models.FileStatusOnSeedbox, job: models.JobStatusFailed, age: 30 * h,
		retries: 5, err: "rsync permanent failure (exit 23): some files could not be transferred"})
	add(item{root: videoRoot, rel: "Nosferatu.2024.1080p.WEB-DL/Nosferatu.2024.1080p.WEB-DL.mkv",
		size: 7 * gb, category: "dp-movies", file: models.FileStatusOnSeedbox, job: models.JobStatusCancelled, age: 50 * h})
	for ep := 1; ep <= 4; ep++ {
		add(item{root: videoRoot, rel: fmt.Sprintf("Alone.S11.1080p.WEB.h264/Alone.S11E%02d.1080p.WEB.h264.mkv", ep),
			size: 2*gb + int64(ep)*41*mb, category: "dp-tv", file: models.FileStatusOnSeedbox, age: 20 * time.Minute})
	}

	// Books: an ebook done, an audiobook part copying, its cover not queued.
	add(item{root: booksRoot, rel: "ebooks/The Left Hand of Darkness - Ursula K. Le Guin/The Left Hand of Darkness.epub",
		size: 2 * mb, category: "dp-books", file: models.FileStatusDownloaded, job: models.JobStatusCompleted, age: 9 * h})
	add(item{root: booksRoot, rel: "ebooks/Piranesi - Susanna Clarke/Piranesi.epub",
		size: 3 * mb, category: "dp-books", file: models.FileStatusOnSeedbox, age: 40 * time.Minute})
	for part := 1; part <= 3; part++ {
		it := item{root: booksRoot, rel: fmt.Sprintf("audiobooks/Project Hail Mary - Andy Weir/Project Hail Mary - Part %d.m4b", part),
			size: 310*mb + int64(part)*7*mb, category: "dp-audiobooks", age: 2 * h}
		if part == 1 {
			it.file, it.job, it.progress = models.FileStatusDownloading, models.JobStatusRunning, 71
		} else {
			it.file, it.job = models.FileStatusQueued, models.JobStatusQueued
		}
		add(it)
	}
	add(item{root: booksRoot, rel: "audiobooks/Project Hail Mary - Andy Weir/cover.jpg",
		size: 400 * 1024, category: "dp-audiobooks", file: models.FileStatusOnSeedbox, age: 2 * h})
	return out
}

func main() {
	dbPath := flag.String("db", "data/preview.db", "database to create")
	reset := flag.Bool("reset", false, "delete the database first")
	flag.Parse()

	if err := run(*dbPath, *reset, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(dbPath string, reset bool, now time.Time) error {
	if reset {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	repo, err := repository.New(dbPath) // creates the schema
	if err != nil {
		return err
	}
	defer repo.Close()
	if n, err := repo.CountJobs(models.JobFilter{}); err != nil {
		return err
	} else if n > 0 {
		return fmt.Errorf("%s already has %d jobs; pass -reset to start over", dbPath, n)
	}

	// Backdating timestamps needs plain SQL: the repository stamps "now".
	raw, err := sql.Open("sqlite", "file:"+dbPath+"?_time_format=sqlite&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer raw.Close()

	var jobs, files int
	for _, it := range items() {
		seen := now.Add(-it.age)
		rf := &models.RemoteFile{
			RemotePath:  it.root + it.rel,
			Name:        path.Base(it.rel),
			Size:        it.size,
			Extension:   strings.TrimPrefix(path.Ext(it.rel), "."),
			Status:      it.file,
			WatchedPath: it.root,
			FirstSeenAt: seen,
			LastSeenAt:  now.Add(-2 * time.Minute),
		}
		if err := repo.UpsertRemoteFile(rf); err != nil {
			return err
		}
		files++
		if it.job == "" {
			continue
		}

		job, err := newJob(it, seen, now)
		if err != nil {
			return err
		}
		if err := repo.CreateJob(job); err != nil {
			return err
		}
		if err := repo.UpdateJob(job); err != nil {
			return err
		}
		if _, err := raw.Exec(`UPDATE jobs SET created_at = ? WHERE id = ?`, seen.UTC().Format(time.DateTime), job.ID); err != nil {
			return err
		}
		if err := repo.LinkRemoteFileToJob(rf.ID, job.ID, it.file); err != nil {
			return err
		}
		if it.job == models.JobStatusFailed {
			if err := repo.CreateJobAttempt(&models.JobAttempt{
				JobID: job.ID, AttemptNum: it.retries, Status: models.JobStatusFailed, ErrorMessage: it.err,
			}); err != nil {
				return err
			}
		}
		jobs++
	}
	fmt.Printf("seeded %s: %d jobs, %d seedbox files\n", dbPath, jobs, files)
	return nil
}

func newJob(it item, seen, now time.Time) (*models.Job, error) {
	dest := videoDest
	if it.root == booksRoot {
		dest = booksDest
	}
	torrent, _, _ := strings.Cut(it.rel, "/")
	if it.root == booksRoot {
		// ebooks/<torrent>/... and audiobooks/<torrent>/...
		torrent = strings.Split(it.rel, "/")[1]
	}
	job := &models.Job{
		Name:       path.Base(it.rel),
		RemotePath: it.root + it.rel,
		LocalPath:  dest + path.Dir(it.rel) + "/",
		Status:     it.job,
		MaxRetries: 5,
		Retries:    it.retries,
		FileSize:   it.size,
		Metadata:   models.JobMetadata{Category: it.category, TorrentName: torrent},
	}
	started := seen.Add(5 * time.Minute)
	switch it.job {
	case models.JobStatusRunning:
		done := int64(float64(it.size) * it.progress / 100)
		speed := int64(58 * mb)
		eta := now.Add(time.Duration(float64(it.size-done)/float64(speed)) * time.Second)
		job.StartedAt = &started
		job.TransferredBytes, job.TransferSpeed = done, speed
		job.Progress = models.JobProgress{
			Percentage: it.progress, TransferredBytes: done, TotalBytes: it.size, TransferSpeed: speed,
			ETA: &eta, CurrentFile: path.Base(it.rel), FilesTotal: 1, LastUpdateTime: now,
		}
	case models.JobStatusCompleted:
		finished := started.Add(time.Duration(it.size/(55*mb)) * time.Second)
		job.StartedAt, job.CompletedAt = &started, &finished
		job.TransferredBytes = it.size
		job.Progress = models.JobProgress{Percentage: 100, TransferredBytes: it.size, TotalBytes: it.size,
			FilesCompleted: 1, FilesTotal: 1, LastUpdateTime: finished}
	case models.JobStatusFailed:
		job.StartedAt = &started
		job.ErrorMessage = it.err
	case models.JobStatusQueued, models.JobStatusCancelled:
	default:
		return nil, fmt.Errorf("no seed recipe for job status %q", it.job)
	}
	return job, nil
}
