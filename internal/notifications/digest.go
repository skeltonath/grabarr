package notifications

import (
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/skeltonath/grabarr/internal/interfaces"
	"github.com/skeltonath/grabarr/internal/models"
)

// FailureDigest batches job-failure alerts. The first failure opens a window;
// failures arriving inside it (each one extends it, up to maxWait) go out as
// one alert. A lone failure still gets the usual per-job alert, just a little
// later. Everything else passes straight through.
//
// Without it, one bad torrent meant one Pushover alert per file.
type FailureDigest struct {
	next    interfaces.Notifier
	window  time.Duration
	maxWait time.Duration
	jobsURL string // where the alert points, e.g. https://…/jobs?status=failed

	mu      sync.Mutex
	pending []*models.Job
	opened  time.Time
	timer   *time.Timer
	now     func() time.Time
}

// NewFailureDigest wraps next. jobsURL is linked from digest alerts; empty
// leaves the link out.
func NewFailureDigest(next interfaces.Notifier, window, maxWait time.Duration, jobsURL string) *FailureDigest {
	return &FailureDigest{next: next, window: window, maxWait: maxWait, jobsURL: jobsURL, now: time.Now}
}

func (d *FailureDigest) IsEnabled() bool { return d.next.IsEnabled() }

func (d *FailureDigest) NotifyJobCompleted(job *models.Job) error {
	return d.next.NotifyJobCompleted(job)
}

func (d *FailureDigest) NotifySystemAlert(title, message string, priority int) error {
	return d.next.NotifySystemAlert(title, message, priority)
}

// NotifyJobFailed queues the failure for the next digest. It never fails: the
// alert is sent later, and a send error is logged then.
func (d *FailureDigest) NotifyJobFailed(job *models.Job) error {
	snapshot := *job
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pending = append(d.pending, &snapshot)
	now := d.now()
	switch {
	case d.timer == nil:
		d.opened = now
		d.timer = time.AfterFunc(d.window, d.Flush)
	case now.Sub(d.opened)+d.window <= d.maxWait:
		d.timer.Reset(d.window)
	}
	return nil
}

// Flush sends whatever is pending now. Called by the timer, and on shutdown so
// nothing is lost.
func (d *FailureDigest) Flush() {
	d.mu.Lock()
	jobs := d.pending
	d.pending = nil
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.mu.Unlock()

	if len(jobs) == 0 {
		return
	}
	var err error
	if len(jobs) == 1 {
		err = d.next.NotifyJobFailed(jobs[0])
	} else {
		title, message := digestMessage(jobs, d.jobsURL)
		err = d.next.NotifySystemAlert(title, message, 1)
	}
	if err != nil {
		slog.Error("failed to send job failure alert", "jobs", len(jobs), "error", err)
	}
}

// digestMessage summarises several failures: how many, which folders, the
// most common error, and where to look.
func digestMessage(jobs []*models.Job, jobsURL string) (string, string) {
	folders := map[string]int{}
	errs := map[string]int{}
	for _, j := range jobs {
		folders[path.Base(path.Dir(j.RemotePath))]++
		if j.ErrorMessage != "" {
			errs[j.ErrorMessage]++
		}
	}

	var msg strings.Builder
	for _, f := range topKeys(folders, 5) {
		fmt.Fprintf(&msg, "%d in %s\n", folders[f], f)
	}
	if extra := len(folders) - 5; extra > 0 {
		fmt.Fprintf(&msg, "and %d more folders\n", extra)
	}
	if common := topKeys(errs, 1); len(common) == 1 {
		fmt.Fprintf(&msg, "\nError: %s", common[0])
		if n := errs[common[0]]; n < len(jobs) {
			fmt.Fprintf(&msg, " (%d of %d)", n, len(jobs))
		}
		msg.WriteString("\n")
	}
	if jobsURL != "" {
		fmt.Fprintf(&msg, "\n%s", jobsURL)
	}
	return fmt.Sprintf("Grabarr: %d copies failed", len(jobs)), strings.TrimSpace(msg.String())
}

// topKeys returns up to n keys, most frequent first, ties by name.
func topKeys(counts map[string]int, n int) []string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > n {
		keys = keys[:n]
	}
	return keys
}
