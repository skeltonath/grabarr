package arr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// countingServer returns a test server that counts command requests.
func countingServer(t *testing.T, counter *int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(counter, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNotifierRoutesCategoryToMatchingInstance(t *testing.T) {
	var radarrCalls, sonarrCalls int64
	radarr := countingServer(t, &radarrCalls)
	sonarr := countingServer(t, &sonarrCalls)

	n := NewNotifier([]InstanceConfig{
		{Name: "radarr", Enabled: true, URL: radarr.URL, APIKey: "k", Categories: []string{"dp-movies"}},
		{Name: "sonarr", Enabled: true, URL: sonarr.URL, APIKey: "k", Categories: []string{"dp-tv", "dp-anime"}},
	}, 10*time.Millisecond)

	n.NotifyCompleted(context.Background(), "dp-movies")
	n.Flush()

	if got := atomic.LoadInt64(&radarrCalls); got != 1 {
		t.Errorf("radarr calls = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&sonarrCalls); got != 0 {
		t.Errorf("sonarr calls = %d, want 0 (category belongs to radarr)", got)
	}
}

func TestNotifierRoutesAnimeCategoryToSonarr(t *testing.T) {
	var radarrCalls, sonarrCalls int64
	radarr := countingServer(t, &radarrCalls)
	sonarr := countingServer(t, &sonarrCalls)

	n := NewNotifier([]InstanceConfig{
		{Name: "radarr", Enabled: true, URL: radarr.URL, APIKey: "k", Categories: []string{"dp-movies"}},
		{Name: "sonarr", Enabled: true, URL: sonarr.URL, APIKey: "k", Categories: []string{"dp-tv", "dp-anime"}},
	}, 10*time.Millisecond)

	n.NotifyCompleted(context.Background(), "dp-anime")
	n.Flush()

	if got := atomic.LoadInt64(&sonarrCalls); got != 1 {
		t.Errorf("sonarr calls = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&radarrCalls); got != 0 {
		t.Errorf("radarr calls = %d, want 0", got)
	}
}

// A season pack produces one job per episode, so a burst of completions must
// collapse into a single refresh rather than one HTTP call per file.
func TestNotifierCoalescesBurstIntoSingleCall(t *testing.T) {
	var calls int64
	srv := countingServer(t, &calls)

	n := NewNotifier([]InstanceConfig{
		{Name: "sonarr", Enabled: true, URL: srv.URL, APIKey: "k", Categories: []string{"dp-tv"}},
	}, 50*time.Millisecond)

	for i := 0; i < 26; i++ {
		n.NotifyCompleted(context.Background(), "dp-tv")
	}
	n.Flush()

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (26 completions should coalesce)", got)
	}
}

func TestNotifierIgnoresUnknownCategory(t *testing.T) {
	var calls int64
	srv := countingServer(t, &calls)

	n := NewNotifier([]InstanceConfig{
		{Name: "radarr", Enabled: true, URL: srv.URL, APIKey: "k", Categories: []string{"dp-movies"}},
	}, 10*time.Millisecond)

	n.NotifyCompleted(context.Background(), "some-other-category")
	n.Flush()

	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Errorf("calls = %d, want 0 for unmatched category", got)
	}
}

func TestNotifierSkipsDisabledInstance(t *testing.T) {
	var calls int64
	srv := countingServer(t, &calls)

	n := NewNotifier([]InstanceConfig{
		{Name: "radarr", Enabled: false, URL: srv.URL, APIKey: "k", Categories: []string{"dp-movies"}},
	}, 10*time.Millisecond)

	n.NotifyCompleted(context.Background(), "dp-movies")
	n.Flush()

	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Errorf("calls = %d, want 0 for disabled instance", got)
	}
}

// Two bursts separated by more than the debounce window are two distinct
// events and must each produce a refresh.
func TestNotifierFiresAgainAfterFlush(t *testing.T) {
	var calls int64
	srv := countingServer(t, &calls)

	n := NewNotifier([]InstanceConfig{
		{Name: "radarr", Enabled: true, URL: srv.URL, APIKey: "k", Categories: []string{"dp-movies"}},
	}, 10*time.Millisecond)

	n.NotifyCompleted(context.Background(), "dp-movies")
	n.Flush()
	n.NotifyCompleted(context.Background(), "dp-movies")
	n.Flush()

	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestNotifierMatchesCategoryCaseInsensitively(t *testing.T) {
	var calls int64
	srv := countingServer(t, &calls)

	n := NewNotifier([]InstanceConfig{
		{Name: "radarr", Enabled: true, URL: srv.URL, APIKey: "k", Categories: []string{"dp-movies"}},
	}, 10*time.Millisecond)

	n.NotifyCompleted(context.Background(), "DP-Movies")
	n.Flush()

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (category match should be case-insensitive)", got)
	}
}

// The sync scanner discovers files by listing a single seedbox directory that
// holds every category, so those jobs carry no category at all. A refresh is
// cheap and idempotent, so an unknown category fans out to every instance
// rather than being dropped.
func TestNotifierBroadcastsWhenCategoryEmpty(t *testing.T) {
	var radarrCalls, sonarrCalls int64
	radarr := countingServer(t, &radarrCalls)
	sonarr := countingServer(t, &sonarrCalls)

	n := NewNotifier([]InstanceConfig{
		{Name: "radarr", Enabled: true, URL: radarr.URL, APIKey: "k", Categories: []string{"dp-movies"}},
		{Name: "sonarr", Enabled: true, URL: sonarr.URL, APIKey: "k", Categories: []string{"dp-tv"}},
	}, 10*time.Millisecond)

	n.NotifyCompleted(context.Background(), "")
	n.Flush()

	if got := atomic.LoadInt64(&radarrCalls); got != 1 {
		t.Errorf("radarr calls = %d, want 1 (empty category should broadcast)", got)
	}
	if got := atomic.LoadInt64(&sonarrCalls); got != 1 {
		t.Errorf("sonarr calls = %d, want 1 (empty category should broadcast)", got)
	}
}

func TestNotifierHasTargets(t *testing.T) {
	tests := []struct {
		name      string
		instances []InstanceConfig
		want      bool
	}{
		{
			name:      "no instances",
			instances: nil,
			want:      false,
		},
		{
			name:      "one enabled instance",
			instances: []InstanceConfig{{Name: "radarr", Enabled: true, URL: "http://x", APIKey: "k"}},
			want:      true,
		},
		{
			name:      "only disabled instances",
			instances: []InstanceConfig{{Name: "radarr", Enabled: false, URL: "http://x", APIKey: "k"}},
			want:      false,
		},
		{
			name:      "enabled but missing api key",
			instances: []InstanceConfig{{Name: "radarr", Enabled: true, URL: "http://x"}},
			want:      false,
		},
		{
			name:      "enabled but missing url",
			instances: []InstanceConfig{{Name: "radarr", Enabled: true, APIKey: "k"}},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewNotifier(tt.instances, time.Second).HasTargets(); got != tt.want {
				t.Errorf("HasTargets() = %v, want %v", got, tt.want)
			}
		})
	}
}
