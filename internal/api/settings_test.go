package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSettingsHandlers builds handlers over a config with known tunable values.
func setupSettingsHandlers(t *testing.T) (*Handlers, *config.Config) {
	t.Helper()

	cfg := &config.Config{
		Jobs: config.JobsConfig{
			MaxConcurrent:         4,
			MaxRetries:            3,
			CleanupCompletedAfter: 168 * time.Hour,
			CleanupFailedAfter:    720 * time.Hour,
		},
		Gatekeeper: config.GatekeeperConfig{
			Seedbox:   config.SeedboxConfig{BandwidthLimitMbps: 800},
			CacheDisk: config.CacheDiskConfig{MaxUsagePercent: 80},
		},
		Sync: config.SyncConfig{Enabled: true, ScanInterval: 5 * time.Minute},
	}

	handlers := NewHandlers(mocks.NewMockJobQueue(t), mocks.NewMockGatekeeper(t), cfg, nil, nil)
	return handlers, cfg
}

// decodeSettings pulls the settings payload out of the standard API envelope.
func decodeSettings(t *testing.T, body []byte) map[string]map[string]any {
	t.Helper()

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Settings             []map[string]any `json:"settings"`
			PerJobBandwidthKiBps int              `json:"per_job_bandwidth_kib_s"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.True(t, response.Success)

	byKey := make(map[string]map[string]any, len(response.Data.Settings))
	for _, s := range response.Data.Settings {
		byKey[s["key"].(string)] = s
	}
	return byKey
}

func TestGetSettings(t *testing.T) {
	h, _ := setupSettingsHandlers(t)

	rec := httptest.NewRecorder()
	h.GetSettings(rec, httptest.NewRequest("GET", "/api/v1/settings", nil))

	assert.Equal(t, http.StatusOK, rec.Code)

	settings := decodeSettings(t, rec.Body.Bytes())
	concurrent := settings["jobs.max_concurrent"]
	assert.Equal(t, float64(4), concurrent["value"])
	assert.Equal(t, float64(4), concurrent["config_value"])
	assert.Equal(t, false, concurrent["overridden"])
	assert.Equal(t, "Max concurrent jobs", concurrent["label"])
	assert.Equal(t, "int", concurrent["kind"])

	// The descriptors carry their own bounds so the UI can validate inline.
	assert.Equal(t, float64(1), concurrent["min"])
	assert.Equal(t, float64(64), concurrent["max"])
}

func TestGetSettings_ReportsPerJobBandwidth(t *testing.T) {
	h, _ := setupSettingsHandlers(t)

	rec := httptest.NewRecorder()
	h.GetSettings(rec, httptest.NewRequest("GET", "/api/v1/settings", nil))

	var response struct {
		Data struct {
			PerJobBandwidthKiBps int `json:"per_job_bandwidth_kib_s"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))

	// 800 Mbps split across 4 slots.
	assert.Equal(t, 24414, response.Data.PerJobBandwidthKiBps)
}

func TestUpdateSettings_AppliesAndReturnsNewState(t *testing.T) {
	h, cfg := setupSettingsHandlers(t)

	body := `{"jobs.max_concurrent": 8, "sync.scan_interval": 600}`
	req := httptest.NewRequest("PATCH", "/api/v1/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.UpdateSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 8, cfg.GetJobs().MaxConcurrent)
	assert.Equal(t, 10*time.Minute, cfg.GetSync().ScanInterval)

	settings := decodeSettings(t, rec.Body.Bytes())
	assert.Equal(t, float64(8), settings["jobs.max_concurrent"]["value"])
	assert.Equal(t, true, settings["jobs.max_concurrent"]["overridden"])
}

func TestUpdateSettings_NullResetsToConfigValue(t *testing.T) {
	h, cfg := setupSettingsHandlers(t)

	req := httptest.NewRequest("PATCH", "/api/v1/settings", strings.NewReader(`{"jobs.max_concurrent": 8}`))
	h.UpdateSettings(httptest.NewRecorder(), req)
	require.Equal(t, 8, cfg.GetJobs().MaxConcurrent)

	req = httptest.NewRequest("PATCH", "/api/v1/settings", strings.NewReader(`{"jobs.max_concurrent": null}`))
	rec := httptest.NewRecorder()
	h.UpdateSettings(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 4, cfg.GetJobs().MaxConcurrent)
	assert.Equal(t, false, decodeSettings(t, rec.Body.Bytes())["jobs.max_concurrent"]["overridden"])
}

func TestUpdateSettings_RejectsBadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed json", `{"jobs.max_concurrent":`},
		{"empty body", `{}`},
		{"unknown setting", `{"jobs.nonexistent": 1}`},
		{"out of range", `{"gatekeeper.cache_disk.max_usage_percent": 400}`},
		{"wrong type", `{"sync.enabled": "maybe"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, cfg := setupSettingsHandlers(t)

			req := httptest.NewRequest("PATCH", "/api/v1/settings", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.UpdateSettings(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, 4, cfg.GetJobs().MaxConcurrent, "config must be untouched")

			var response APIResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			assert.False(t, response.Success)
			assert.NotEmpty(t, response.Error)
		})
	}
}
