package arr

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// InstanceConfig describes a single Radarr/Sonarr instance and the download
// categories it owns.
type InstanceConfig struct {
	Name       string
	Enabled    bool
	URL        string
	APIKey     string
	Categories []string
}

// Notifier fans job completions out to the *arr instance that owns the job's
// category.
//
// Completions arrive one per file, so a season pack yields dozens in quick
// succession. RefreshMonitoredDownloads is idempotent, so rather than issuing
// one call per file the notifier debounces: the first completion arms a timer,
// later completions inside the window are absorbed, and a single refresh fires
// once the burst settles.
type Notifier struct {
	targets  []*target
	debounce time.Duration
}

type target struct {
	name       string
	client     *Client
	categories map[string]struct{}

	mu      sync.Mutex
	timer   *time.Timer
	pending bool
	wg      sync.WaitGroup
}

// NewNotifier builds a Notifier from instance configs. Disabled instances and
// instances missing a URL or API key are dropped.
func NewNotifier(instances []InstanceConfig, debounce time.Duration) *Notifier {
	n := &Notifier{debounce: debounce}

	for _, inst := range instances {
		if !inst.Enabled || inst.URL == "" || inst.APIKey == "" {
			continue
		}

		cats := make(map[string]struct{}, len(inst.Categories))
		for _, c := range inst.Categories {
			cats[strings.ToLower(strings.TrimSpace(c))] = struct{}{}
		}

		n.targets = append(n.targets, &target{
			name:       inst.Name,
			client:     NewClient(inst.URL, inst.APIKey),
			categories: cats,
		})
	}

	return n
}

// HasTargets reports whether any usable instance was configured.
func (n *Notifier) HasTargets() bool {
	return len(n.targets) > 0
}

// NotifyCompleted records that a download for the given category has landed
// locally. The matching instance is refreshed once the burst settles.
//
// An empty category broadcasts to every instance. Jobs discovered by the sync
// scanner have no category — it lists one seedbox directory shared by all of
// them — and a refresh is cheap and idempotent, so fanning out beats dropping
// the event.
func (n *Notifier) NotifyCompleted(ctx context.Context, category string) {
	cat := strings.ToLower(strings.TrimSpace(category))

	for _, t := range n.targets {
		if _, ok := t.categories[cat]; ok || cat == "" {
			t.schedule(ctx, n.debounce)
		}
	}
}

// Flush waits for any armed timers to fire and their refreshes to finish.
// It exists so callers (and tests) can force the pending burst out immediately.
func (n *Notifier) Flush() {
	for _, t := range n.targets {
		t.fireNow()
		t.wg.Wait()
	}
}

// schedule arms the debounce timer, or extends it if a burst is already in flight.
//
// Exactly one wg slot is held per burst — taken when the burst opens and
// released by whichever refresh consumes it. Re-arming the timer must not add
// another slot, or Flush would wait on a count no timer will ever retire.
func (t *target) schedule(ctx context.Context, debounce time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.pending {
		t.pending = true
		t.wg.Add(1)
	}

	if t.timer != nil {
		t.timer.Stop()
	}

	t.timer = time.AfterFunc(debounce, func() {
		t.refresh(ctx)
	})
}

// fireNow collapses any armed timer into an immediate refresh.
func (t *target) fireNow() {
	t.mu.Lock()
	if t.timer != nil {
		t.timer.Stop()
	}
	t.mu.Unlock()

	// If the timer already fired and is mid-refresh, this call finds the burst
	// consumed and returns without touching the wg; Flush still waits on the
	// in-flight one.
	t.refresh(context.Background())
}

func (t *target) refresh(ctx context.Context) {
	t.mu.Lock()
	if !t.pending {
		t.mu.Unlock()
		return
	}
	t.pending = false
	t.mu.Unlock()

	// We consumed the burst, so we own its wg slot.
	defer t.wg.Done()

	if err := t.client.RefreshMonitoredDownloads(ctx); err != nil {
		slog.Error("failed to refresh monitored downloads", "instance", t.name, "error", err)
		return
	}

	slog.Info("triggered import refresh", "instance", t.name)
}
