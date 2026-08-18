package config

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"
)

// SettingKind describes how a setting's value is parsed, validated and rendered.
type SettingKind string

const (
	KindInt      SettingKind = "int"
	KindBool     SettingKind = "bool"
	KindDuration SettingKind = "duration"
)

// SettingsStore persists runtime overrides. The repository implements it; the
// interface lives here so the config package stays free of database imports.
type SettingsStore interface {
	GetSettingOverrides() (map[string]string, error)
	SetSettingOverride(key, value string) error
	DeleteSettingOverride(key string) error
}

// settingDef describes one setting that can be changed at runtime.
//
// Every tunable lives in this one table: the API descriptors, validation, the
// persisted form and the write-back into the live Config all come from here, so
// adding a setting means adding one entry rather than touching five files.
type settingDef struct {
	Key         string
	Label       string
	Description string
	Kind        SettingKind
	Unit        string
	Min         float64
	Max         float64

	// get reads the setting's current value out of a Config.
	get func(c *Config) any
	// set writes a validated value back into a Config.
	set func(c *Config, v any)
}

// settingDefs is the registry of runtime-tunable settings, in display order.
var settingDefs = []settingDef{
	{
		Key:         "jobs.max_concurrent",
		Label:       "Max concurrent jobs",
		Description: "How many transfers may run at once. Extra jobs wait in the queue.",
		Kind:        KindInt,
		Unit:        "jobs",
		Min:         1,
		Max:         64,
		get:         func(c *Config) any { return c.Jobs.MaxConcurrent },
		set:         func(c *Config, v any) { c.Jobs.MaxConcurrent = v.(int) },
	},
	{
		Key:         "gatekeeper.seedbox.bandwidth_limit_mbps",
		Label:       "Total bandwidth limit",
		Description: "Overall transfer ceiling, split evenly across the concurrent job slots. 0 means unlimited.",
		Kind:        KindInt,
		Unit:        "Mbps",
		Min:         0,
		Max:         100000,
		get:         func(c *Config) any { return c.Gatekeeper.Seedbox.BandwidthLimitMbps },
		set:         func(c *Config, v any) { c.Gatekeeper.Seedbox.BandwidthLimitMbps = v.(int) },
	},
	{
		Key:         "gatekeeper.cache_disk.max_usage_percent",
		Label:       "Cache disk max usage",
		Description: "New jobs are held once the cache drive passes this fill level.",
		Kind:        KindInt,
		Unit:        "%",
		Min:         1,
		Max:         100,
		get:         func(c *Config) any { return c.Gatekeeper.CacheDisk.MaxUsagePercent },
		set:         func(c *Config, v any) { c.Gatekeeper.CacheDisk.MaxUsagePercent = v.(int) },
	},
	{
		Key:         "jobs.max_retries",
		Label:       "Max retries",
		Description: "How many times a failed transfer is retried automatically. Applies to newly created jobs.",
		Kind:        KindInt,
		Unit:        "attempts",
		Min:         0,
		Max:         20,
		get:         func(c *Config) any { return c.Jobs.MaxRetries },
		set:         func(c *Config, v any) { c.Jobs.MaxRetries = v.(int) },
	},
	{
		Key:         "jobs.cleanup_completed_after",
		Label:       "Keep completed jobs for",
		Description: "How long finished jobs stay in the job list before being deleted.",
		Kind:        KindDuration,
		Min:         float64(time.Hour),
		Max:         float64(8760 * time.Hour),
		get:         func(c *Config) any { return c.Jobs.CleanupCompletedAfter },
		set:         func(c *Config, v any) { c.Jobs.CleanupCompletedAfter = v.(time.Duration) },
	},
	{
		Key:         "jobs.cleanup_failed_after",
		Label:       "Keep failed jobs for",
		Description: "How long failed jobs stay in the job list before being deleted.",
		Kind:        KindDuration,
		Min:         float64(time.Hour),
		Max:         float64(8760 * time.Hour),
		get:         func(c *Config) any { return c.Jobs.CleanupFailedAfter },
		set:         func(c *Config, v any) { c.Jobs.CleanupFailedAfter = v.(time.Duration) },
	},
	{
		Key:         "sync.enabled",
		Label:       "Seedbox scanning",
		Description: "Whether the seedbox is scanned periodically for new files.",
		Kind:        KindBool,
		get:         func(c *Config) any { return c.Sync.Enabled },
		set:         func(c *Config, v any) { c.Sync.Enabled = v.(bool) },
	},
	{
		Key:         "sync.scan_interval",
		Label:       "Scan interval",
		Description: "How often the seedbox is scanned for new files.",
		Kind:        KindDuration,
		Min:         float64(30 * time.Second),
		Max:         float64(24 * time.Hour),
		get:         func(c *Config) any { return c.Sync.ScanInterval },
		set:         func(c *Config, v any) { c.Sync.ScanInterval = v.(time.Duration) },
	},
}

func lookupSetting(key string) (settingDef, bool) {
	for _, def := range settingDefs {
		if def.Key == key {
			return def, true
		}
	}
	return settingDef{}, false
}

// Setting is the API-facing view of one tunable: what it is, what it is set to
// now, and what config.yaml would give it if the override were removed.
type Setting struct {
	Key         string      `json:"key"`
	Label       string      `json:"label"`
	Description string      `json:"description"`
	Kind        SettingKind `json:"kind"`
	Unit        string      `json:"unit,omitempty"`
	Min         *float64    `json:"min,omitempty"`
	Max         *float64    `json:"max,omitempty"`

	// Value is what the service is running with right now.
	Value any `json:"value"`
	// ConfigValue is what config.yaml specifies, shown so the UI can offer a
	// meaningful "reset" and explain where a value came from.
	ConfigValue any `json:"config_value"`
	// Overridden reports whether Value came from an override rather than the file.
	Overridden bool `json:"overridden"`
}

// AttachSettingsStore wires up override persistence and applies any overrides
// that were saved by a previous run. Without a store the service still works;
// changes just do not survive a restart.
func (c *Config) AttachSettingsStore(store SettingsStore) error {
	c.mu.Lock()
	c.store = store
	c.mu.Unlock()

	saved, err := store.GetSettingOverrides()
	if err != nil {
		return fmt.Errorf("failed to load setting overrides: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.overrides = make(map[string]any, len(saved))
	for key, raw := range saved {
		def, ok := lookupSetting(key)
		if !ok {
			// A key left behind by an older build. Ignore it rather than
			// failing startup over a setting nothing reads any more.
			slog.Warn("ignoring unknown setting override", "key", key)
			continue
		}

		value, err := def.parse(raw)
		if err != nil {
			slog.Warn("ignoring invalid setting override", "key", key, "value", raw, "error", err)
			continue
		}
		c.overrides[key] = value
	}

	c.applyOverridesLocked()

	if len(c.overrides) > 0 {
		slog.Info("applied saved setting overrides", "count", len(c.overrides))
	}
	return nil
}

// Settings returns every tunable with its current and file-configured values.
func (c *Config) Settings() []Setting {
	// Takes the write lock because the baseline may need capturing first.
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ensureBaselineLocked()

	out := make([]Setting, 0, len(settingDefs))
	for _, def := range settingDefs {
		value, overridden := c.overrides[def.Key]
		if !overridden {
			value = def.get(c)
		}

		s := Setting{
			Key:         def.Key,
			Label:       def.Label,
			Description: def.Description,
			Kind:        def.Kind,
			Unit:        def.Unit,
			Value:       def.render(value),
			ConfigValue: def.render(c.baseline[def.Key]),
			Overridden:  overridden,
		}
		if def.Min != 0 || def.Max != 0 {
			min, max := def.renderBound(def.Min), def.renderBound(def.Max)
			s.Min, s.Max = &min, &max
		}
		out = append(out, s)
	}
	return out
}

// UpdateSettings applies a batch of setting changes. A nil value clears the
// override, returning that setting to its config.yaml value.
//
// The batch is validated in full before anything is applied, so a request with
// one bad field leaves the running configuration untouched rather than
// half-updated.
func (c *Config) UpdateSettings(updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	// Sort so validation errors are reported deterministically.
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	type change struct {
		def   settingDef
		value any // nil means "clear the override"
	}

	changes := make([]change, 0, len(keys))
	for _, key := range keys {
		def, ok := lookupSetting(key)
		if !ok {
			return fmt.Errorf("unknown setting: %s", key)
		}

		raw := updates[key]
		if raw == nil {
			changes = append(changes, change{def: def})
			continue
		}

		value, err := def.coerce(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", def.Label, err)
		}
		changes = append(changes, change{def: def, value: value})
	}

	c.mu.Lock()
	c.ensureBaselineLocked()

	store := c.store
	if c.overrides == nil {
		c.overrides = make(map[string]any)
	}

	persist := make([]change, 0, len(changes))
	for _, ch := range changes {
		if ch.value == nil {
			if _, had := c.overrides[ch.def.Key]; !had {
				continue // already on the config.yaml value; nothing to do
			}
			delete(c.overrides, ch.def.Key)
		} else {
			if current, had := c.overrides[ch.def.Key]; had && current == ch.value {
				continue // unchanged; skip the write
			}
			c.overrides[ch.def.Key] = ch.value
		}
		persist = append(persist, ch)
	}

	c.applyOverridesLocked()
	c.mu.Unlock()

	if len(persist) == 0 {
		return nil
	}

	if store != nil {
		for _, ch := range persist {
			var err error
			if ch.value == nil {
				err = store.DeleteSettingOverride(ch.def.Key)
			} else {
				err = store.SetSettingOverride(ch.def.Key, ch.def.format(ch.value))
			}
			// The change is already live; a failed write only costs persistence
			// across a restart, so log it rather than rolling back.
			if err != nil {
				slog.Error("failed to persist setting override", "key", ch.def.Key, "error", err)
			}
		}
	}

	for _, ch := range persist {
		if ch.value == nil {
			slog.Info("setting reset to config file value", "key", ch.def.Key)
		} else {
			slog.Info("setting updated", "key", ch.def.Key, "value", ch.def.format(ch.value))
		}
	}

	c.notifyWatchers()
	return nil
}

// ensureBaselineLocked captures the baseline if it is missing. loadConfig
// captures it up front, but a Config built directly — as tests and embedders do
// — would otherwise have nothing to reset an override back to.
func (c *Config) ensureBaselineLocked() {
	if c.baseline == nil {
		c.captureBaselineLocked()
	}
}

// captureBaselineLocked snapshots the values config.yaml specifies, so an
// override can be undone and the UI can show where a value came from.
func (c *Config) captureBaselineLocked() {
	c.baseline = make(map[string]any, len(settingDefs))
	for _, def := range settingDefs {
		c.baseline[def.Key] = def.get(c)
	}
}

// applyOverridesLocked rebuilds every tunable from the baseline and then stamps
// the overrides on top. It runs after load, after every config.yaml reload and
// after every settings change, so a file edit cannot silently undo a value set
// from the web UI, and clearing an override restores the file's value.
func (c *Config) applyOverridesLocked() {
	c.ensureBaselineLocked()

	for _, def := range settingDefs {
		if base, ok := c.baseline[def.Key]; ok {
			def.set(c, base)
		}
	}
	for key, value := range c.overrides {
		if def, ok := lookupSetting(key); ok {
			def.set(c, value)
		}
	}
}

// PerJobBandwidthKiBps converts the overall bandwidth limit into the per-job
// rsync --bwlimit value, in KiB/s. The limit is a total for the service, so it
// is divided across the concurrent job slots; 0 means unlimited.
func (c *Config) PerJobBandwidthKiBps() int {
	c.mu.RLock()
	limitMbps := c.Gatekeeper.Seedbox.BandwidthLimitMbps
	slots := c.Jobs.MaxConcurrent
	c.mu.RUnlock()

	if limitMbps <= 0 {
		return 0
	}
	if slots < 1 {
		slots = 1
	}

	// Mbps is megabits/second; rsync wants kibibytes/second.
	bytesPerSec := float64(limitMbps) * 1_000_000 / 8
	perJob := int(bytesPerSec / float64(slots) / 1024)

	// A limit small enough to round to zero would read as "unlimited" to rsync,
	// which is the opposite of what was asked for.
	if perJob < 1 {
		perJob = 1
	}
	return perJob
}

// ---- per-kind value handling ----

// parse converts a persisted string back into a typed value.
func (d settingDef) parse(raw string) (any, error) {
	switch d.Kind {
	case KindInt:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("not a whole number: %q", raw)
		}
		return n, d.checkRange(float64(n))
	case KindBool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("not a boolean: %q", raw)
		}
		return b, nil
	case KindDuration:
		dur, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("not a duration: %q", raw)
		}
		return dur, d.checkRange(float64(dur))
	}
	return nil, fmt.Errorf("unsupported setting kind: %s", d.Kind)
}

// coerce converts a value decoded from JSON into the setting's Go type.
// JSON numbers arrive as float64, and durations arrive as strings ("30m") or as
// a number of seconds, whichever the client finds easier to send.
func (d settingDef) coerce(raw any) (any, error) {
	switch d.Kind {
	case KindInt:
		n, err := toInt(raw)
		if err != nil {
			return nil, err
		}
		return n, d.checkRange(float64(n))
	case KindBool:
		switch v := raw.(type) {
		case bool:
			return v, nil
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("expected true or false, got %q", v)
			}
			return b, nil
		}
		return nil, fmt.Errorf("expected true or false, got %v", raw)
	case KindDuration:
		dur, err := toDuration(raw)
		if err != nil {
			return nil, err
		}
		return dur, d.checkRange(float64(dur))
	}
	return nil, fmt.Errorf("unsupported setting kind: %s", d.Kind)
}

// format renders a value for persistence.
func (d settingDef) format(v any) string {
	switch value := v.(type) {
	case int:
		return strconv.Itoa(value)
	case bool:
		return strconv.FormatBool(value)
	case time.Duration:
		return value.String()
	}
	return fmt.Sprintf("%v", v)
}

// render converts a value into the JSON shape the API exposes. Durations go out
// as seconds so the UI can do arithmetic on them without parsing Go syntax.
func (d settingDef) render(v any) any {
	switch value := v.(type) {
	case time.Duration:
		return int64(value / time.Second)
	default:
		return v
	}
}

// renderBound puts a bound in the same units as the rendered value.
func (d settingDef) renderBound(bound float64) float64 {
	if d.Kind == KindDuration {
		return bound / float64(time.Second)
	}
	return bound
}

func (d settingDef) checkRange(v float64) error {
	if d.Min == 0 && d.Max == 0 {
		return nil
	}
	if v < d.Min || v > d.Max {
		return fmt.Errorf("must be between %s and %s",
			d.format(d.fromFloat(d.Min)), d.format(d.fromFloat(d.Max)))
	}
	return nil
}

func (d settingDef) fromFloat(v float64) any {
	if d.Kind == KindDuration {
		return time.Duration(v)
	}
	return int(v)
}

func toInt(raw any) (int, error) {
	switch v := raw.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v != float64(int(v)) {
			return 0, fmt.Errorf("expected a whole number, got %v", v)
		}
		return int(v), nil
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("expected a whole number, got %q", v)
		}
		return n, nil
	}
	return 0, fmt.Errorf("expected a whole number, got %v", raw)
}

func toDuration(raw any) (time.Duration, error) {
	switch v := raw.(type) {
	case time.Duration:
		return v, nil
	case float64:
		if v != float64(int64(v)) {
			return 0, fmt.Errorf("expected whole seconds, got %v", v)
		}
		return time.Duration(int64(v)) * time.Second, nil
	case int:
		return time.Duration(v) * time.Second, nil
	case int64:
		return time.Duration(v) * time.Second, nil
	case string:
		dur, err := time.ParseDuration(v)
		if err != nil {
			return 0, fmt.Errorf("expected a duration like \"30m\", got %q", v)
		}
		return dur, nil
	}
	return 0, fmt.Errorf("expected a duration, got %v", raw)
}
