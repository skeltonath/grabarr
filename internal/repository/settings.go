package repository

import (
	"fmt"
	"strings"
)

// settingOverridePrefix namespaces runtime setting overrides inside the
// system_config table, keeping them clearly separate from the bookkeeping keys
// (last_cleanup, schema_version, ...) that share the table.
const settingOverridePrefix = "setting."

// GetSettingOverrides returns every persisted runtime override, keyed by
// setting key with the storage prefix stripped.
func (r *Repository) GetSettingOverrides() (map[string]string, error) {
	rows, err := r.db.Query(
		"SELECT key, value FROM system_config WHERE key LIKE ?",
		settingOverridePrefix+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query setting overrides: %w", err)
	}
	defer rows.Close()

	overrides := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan setting override: %w", err)
		}
		overrides[strings.TrimPrefix(key, settingOverridePrefix)] = value
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read setting overrides: %w", err)
	}

	return overrides, nil
}

// SetSettingOverride persists a single runtime override.
func (r *Repository) SetSettingOverride(key, value string) error {
	if err := r.SetConfig(settingOverridePrefix+key, value); err != nil {
		return fmt.Errorf("failed to set override for %s: %w", key, err)
	}
	return nil
}

// DeleteSettingOverride removes a runtime override, returning the setting to
// whatever config.yaml specifies. Deleting an absent override is not an error.
func (r *Repository) DeleteSettingOverride(key string) error {
	_, err := r.db.Exec("DELETE FROM system_config WHERE key = ?", settingOverridePrefix+key)
	if err != nil {
		return fmt.Errorf("failed to delete override for %s: %w", key, err)
	}
	return nil
}
