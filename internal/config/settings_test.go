package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore is an in-memory SettingsStore for exercising persistence.
type fakeStore struct {
	values  map[string]string
	loadErr error
	setErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{values: map[string]string{}}
}

func (f *fakeStore) GetSettingOverrides() (map[string]string, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	out := make(map[string]string, len(f.values))
	for k, v := range f.values {
		out[k] = v
	}
	return out, nil
}

func (f *fakeStore) SetSettingOverride(key, value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value
	return nil
}

func (f *fakeStore) DeleteSettingOverride(key string) error {
	delete(f.values, key)
	return nil
}

const settingsTestConfig = `
server:
  port: 8080
  host: "0.0.0.0"
  shutdown_timeout: 30s

gatekeeper:
  seedbox:
    bandwidth_limit_mbps: 800
    check_interval: 30s
  cache_disk:
    path: "/tmp"
    max_usage_percent: 80
    check_interval: 30s

jobs:
  max_concurrent: 4
  max_retries: 3
  cleanup_completed_after: 168h
  cleanup_failed_after: 720h

sync:
  enabled: true
  scan_interval: 5m

database:
  path: "%s"

logging:
  level: "info"
  format: "json"
`

// newTestConfig loads a config from a temp file without touching the package
// level singleton, so tests stay independent of each other.
func newTestConfig(t *testing.T) (*Config, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "data", "grabarr.db")
	configPath := filepath.Join(tmpDir, "config.yaml")

	content := settingsTestConfig
	content = replaceDBPath(content, dbPath)
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	cfg, err := loadConfig(configPath)
	require.NoError(t, err)
	return cfg, configPath
}

func replaceDBPath(content, dbPath string) string {
	return stringsReplace(content, "%s", dbPath)
}

func stringsReplace(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func settingByKey(t *testing.T, settings []Setting, key string) Setting {
	t.Helper()
	for _, s := range settings {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("setting %s not found", key)
	return Setting{}
}

func TestSettings_ReflectConfigFileByDefault(t *testing.T) {
	cfg, _ := newTestConfig(t)

	settings := cfg.Settings()
	require.Len(t, settings, len(settingDefs))

	concurrent := settingByKey(t, settings, "jobs.max_concurrent")
	assert.Equal(t, 4, concurrent.Value)
	assert.Equal(t, 4, concurrent.ConfigValue)
	assert.False(t, concurrent.Overridden)

	// Durations are exposed in seconds so the UI does not have to parse Go syntax.
	interval := settingByKey(t, settings, "sync.scan_interval")
	assert.Equal(t, int64(300), interval.Value)
	assert.Equal(t, KindDuration, interval.Kind)

	enabled := settingByKey(t, settings, "sync.enabled")
	assert.Equal(t, true, enabled.Value)
	assert.Equal(t, KindBool, enabled.Kind)
}

func TestUpdateSettings_AppliesToLiveConfig(t *testing.T) {
	cfg, _ := newTestConfig(t)

	require.NoError(t, cfg.UpdateSettings(map[string]any{
		"jobs.max_concurrent":                     float64(9),
		"gatekeeper.cache_disk.max_usage_percent": float64(65),
		"sync.enabled":                            false,
		"sync.scan_interval":                      float64(900),
	}))

	assert.Equal(t, 9, cfg.GetJobs().MaxConcurrent)
	assert.Equal(t, 65, cfg.GetGatekeeper().CacheDisk.MaxUsagePercent)
	assert.False(t, cfg.GetSync().Enabled)
	assert.Equal(t, 15*time.Minute, cfg.GetSync().ScanInterval)

	concurrent := settingByKey(t, cfg.Settings(), "jobs.max_concurrent")
	assert.Equal(t, 9, concurrent.Value)
	assert.Equal(t, 4, concurrent.ConfigValue, "config file value should still be reported")
	assert.True(t, concurrent.Overridden)
}

func TestUpdateSettings_AcceptsDurationStrings(t *testing.T) {
	cfg, _ := newTestConfig(t)

	require.NoError(t, cfg.UpdateSettings(map[string]any{"sync.scan_interval": "45m"}))
	assert.Equal(t, 45*time.Minute, cfg.GetSync().ScanInterval)
}

func TestUpdateSettings_NilClearsOverride(t *testing.T) {
	cfg, _ := newTestConfig(t)

	require.NoError(t, cfg.UpdateSettings(map[string]any{"jobs.max_concurrent": float64(9)}))
	require.Equal(t, 9, cfg.GetJobs().MaxConcurrent)

	require.NoError(t, cfg.UpdateSettings(map[string]any{"jobs.max_concurrent": nil}))
	assert.Equal(t, 4, cfg.GetJobs().MaxConcurrent)
	assert.False(t, settingByKey(t, cfg.Settings(), "jobs.max_concurrent").Overridden)
}

func TestUpdateSettings_RejectsOutOfRange(t *testing.T) {
	cfg, _ := newTestConfig(t)

	tests := []struct {
		name    string
		updates map[string]any
	}{
		{"zero concurrency", map[string]any{"jobs.max_concurrent": float64(0)}},
		{"too much concurrency", map[string]any{"jobs.max_concurrent": float64(999)}},
		{"cache over 100 percent", map[string]any{"gatekeeper.cache_disk.max_usage_percent": float64(101)}},
		{"negative bandwidth", map[string]any{"gatekeeper.seedbox.bandwidth_limit_mbps": float64(-1)}},
		{"scan interval too short", map[string]any{"sync.scan_interval": float64(5)}},
		{"fractional int", map[string]any{"jobs.max_retries": 2.5}},
		{"wrong type for bool", map[string]any{"sync.enabled": float64(1)}},
		{"unparseable duration", map[string]any{"sync.scan_interval": "soon"}},
		{"unknown key", map[string]any{"jobs.nope": float64(1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, cfg.UpdateSettings(tt.updates))
			// Nothing in the batch should have been applied.
			assert.Equal(t, 4, cfg.GetJobs().MaxConcurrent)
		})
	}
}

// A batch is validated in full before anything is applied, so one bad field
// cannot leave the service half-updated.
func TestUpdateSettings_BatchIsAllOrNothing(t *testing.T) {
	cfg, _ := newTestConfig(t)

	err := cfg.UpdateSettings(map[string]any{
		"jobs.max_concurrent":                     float64(9),
		"gatekeeper.cache_disk.max_usage_percent": float64(500),
	})
	require.Error(t, err)

	assert.Equal(t, 4, cfg.GetJobs().MaxConcurrent)
	assert.Equal(t, 80, cfg.GetGatekeeper().CacheDisk.MaxUsagePercent)
}

func TestUpdateSettings_PersistsToStore(t *testing.T) {
	cfg, _ := newTestConfig(t)
	store := newFakeStore()
	require.NoError(t, cfg.AttachSettingsStore(store))

	require.NoError(t, cfg.UpdateSettings(map[string]any{
		"jobs.max_concurrent": float64(9),
		"sync.scan_interval":  float64(900),
	}))

	assert.Equal(t, map[string]string{
		"jobs.max_concurrent": "9",
		"sync.scan_interval":  "15m0s",
	}, store.values)

	require.NoError(t, cfg.UpdateSettings(map[string]any{"jobs.max_concurrent": nil}))
	assert.Equal(t, map[string]string{"sync.scan_interval": "15m0s"}, store.values)
}

func TestAttachSettingsStore_RestoresSavedOverrides(t *testing.T) {
	cfg, _ := newTestConfig(t)

	store := newFakeStore()
	store.values["jobs.max_concurrent"] = "12"
	store.values["sync.scan_interval"] = "20m"

	require.NoError(t, cfg.AttachSettingsStore(store))

	assert.Equal(t, 12, cfg.GetJobs().MaxConcurrent)
	assert.Equal(t, 20*time.Minute, cfg.GetSync().ScanInterval)
	assert.True(t, settingByKey(t, cfg.Settings(), "jobs.max_concurrent").Overridden)
}

// A stored value that no longer parses (or names a setting that no longer
// exists) must not stop the service from starting.
func TestAttachSettingsStore_SkipsUnusableOverrides(t *testing.T) {
	cfg, _ := newTestConfig(t)

	store := newFakeStore()
	store.values["jobs.max_concurrent"] = "not-a-number"
	store.values["jobs.retired_setting"] = "1"
	store.values["gatekeeper.cache_disk.max_usage_percent"] = "600"
	store.values["jobs.max_retries"] = "7"

	require.NoError(t, cfg.AttachSettingsStore(store))

	assert.Equal(t, 4, cfg.GetJobs().MaxConcurrent, "unparseable override should be ignored")
	assert.Equal(t, 80, cfg.GetGatekeeper().CacheDisk.MaxUsagePercent, "out-of-range override should be ignored")
	assert.Equal(t, 7, cfg.GetJobs().MaxRetries, "valid overrides should still apply")
}

// A config.yaml edit should refresh the reported file values without reverting
// a setting that was deliberately changed from the web UI.
func TestReload_KeepsOverridesAndRefreshesBaseline(t *testing.T) {
	cfg, configPath := newTestConfig(t)
	require.NoError(t, cfg.AttachSettingsStore(newFakeStore()))
	require.NoError(t, cfg.UpdateSettings(map[string]any{"jobs.max_concurrent": float64(9)}))

	original, err := os.ReadFile(configPath)
	require.NoError(t, err)
	updated := stringsReplace(string(original), "max_concurrent: 4", "max_concurrent: 6")
	updated = stringsReplace(updated, "max_retries: 3", "max_retries: 5")
	require.NoError(t, os.WriteFile(configPath, []byte(updated), 0644))

	require.NoError(t, cfg.reload(configPath))

	assert.Equal(t, 9, cfg.GetJobs().MaxConcurrent, "override should survive a config file reload")
	assert.Equal(t, 5, cfg.GetJobs().MaxRetries, "un-overridden settings should follow the file")

	concurrent := settingByKey(t, cfg.Settings(), "jobs.max_concurrent")
	assert.Equal(t, 6, concurrent.ConfigValue, "reported file value should follow the file")
	assert.Equal(t, 9, concurrent.Value)
}

func TestUpdateSettings_NotifiesWatchers(t *testing.T) {
	cfg, _ := newTestConfig(t)
	changes := cfg.WatchForChanges()

	require.NoError(t, cfg.UpdateSettings(map[string]any{"jobs.max_concurrent": float64(9)}))

	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("expected a config change notification")
	}
}

func TestPerJobBandwidthKiBps(t *testing.T) {
	tests := []struct {
		name          string
		limitMbps     int
		maxConcurrent int
		want          int
	}{
		// 800 Mbps = 100 MB/s total; over 4 slots that is 25 MB/s each.
		{"splits across slots", 800, 4, 24414},
		{"single slot gets it all", 800, 1, 97656},
		{"zero means unlimited", 0, 4, 0},
		// A limit too small to divide evenly must still throttle, not go unlimited.
		{"rounds up to a real limit", 1, 64, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := newTestConfig(t)
			require.NoError(t, cfg.UpdateSettings(map[string]any{
				"gatekeeper.seedbox.bandwidth_limit_mbps": tt.limitMbps,
				"jobs.max_concurrent":                     tt.maxConcurrent,
			}))

			assert.Equal(t, tt.want, cfg.PerJobBandwidthKiBps())
		})
	}
}

// The settings API rejects a negative limit, but config.yaml is not validated
// that closely, so the conversion has to treat it as unlimited too.
func TestPerJobBandwidthKiBps_NegativeFileValueIsUnlimited(t *testing.T) {
	cfg, _ := newTestConfig(t)
	cfg.Gatekeeper.Seedbox.BandwidthLimitMbps = -1

	assert.Equal(t, 0, cfg.PerJobBandwidthKiBps())
}

// Values reach the registry as JSON (numbers, strings, booleans) and leave it as
// strings for storage. Both directions have to survive the trip intact.
func TestSettingDef_CoerceAndParse(t *testing.T) {
	intDef, _ := lookupSetting("jobs.max_concurrent")
	boolDef, _ := lookupSetting("sync.enabled")
	durDef, _ := lookupSetting("sync.scan_interval")

	tests := []struct {
		name    string
		def     settingDef
		input   any
		want    any
		wantErr bool
	}{
		{name: "int from json number", def: intDef, input: float64(6), want: 6},
		{name: "int from string", def: intDef, input: "6", want: 6},
		{name: "int from int64", def: intDef, input: int64(6), want: 6},
		{name: "int from garbage string", def: intDef, input: "six", wantErr: true},
		{name: "int from bool", def: intDef, input: true, wantErr: true},
		{name: "bool from bool", def: boolDef, input: false, want: false},
		{name: "bool from string", def: boolDef, input: "true", want: true},
		{name: "bool from garbage string", def: boolDef, input: "yes please", wantErr: true},
		{name: "duration from seconds", def: durDef, input: float64(120), want: 2 * time.Minute},
		{name: "duration from int", def: durDef, input: 120, want: 2 * time.Minute},
		{name: "duration from string", def: durDef, input: "2m", want: 2 * time.Minute},
		{name: "duration from fractional seconds", def: durDef, input: 90.5, wantErr: true},
		{name: "duration from bool", def: durDef, input: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.def.coerce(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			// Whatever coerce accepts must survive a storage round trip.
			reparsed, err := tt.def.parse(tt.def.format(got))
			require.NoError(t, err)
			assert.Equal(t, got, reparsed)
		})
	}
}

func TestSettingDef_ParseRejectsBadStoredValues(t *testing.T) {
	boolDef, _ := lookupSetting("sync.enabled")
	durDef, _ := lookupSetting("sync.scan_interval")

	_, err := boolDef.parse("perhaps")
	assert.Error(t, err)

	_, err = durDef.parse("a while")
	assert.Error(t, err)

	// Out-of-range values are rejected on the way back in, not just on the way out.
	_, err = durDef.parse("1s")
	assert.Error(t, err)
}
