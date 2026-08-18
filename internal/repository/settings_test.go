package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettingOverrides_RoundTrip(t *testing.T) {
	repo := setupTestRepo(t)

	overrides, err := repo.GetSettingOverrides()
	require.NoError(t, err)
	assert.Empty(t, overrides)

	require.NoError(t, repo.SetSettingOverride("jobs.max_concurrent", "8"))
	require.NoError(t, repo.SetSettingOverride("sync.enabled", "false"))

	overrides, err = repo.GetSettingOverrides()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"jobs.max_concurrent": "8",
		"sync.enabled":        "false",
	}, overrides)
}

func TestSettingOverrides_Update(t *testing.T) {
	repo := setupTestRepo(t)

	require.NoError(t, repo.SetSettingOverride("jobs.max_concurrent", "8"))
	require.NoError(t, repo.SetSettingOverride("jobs.max_concurrent", "3"))

	overrides, err := repo.GetSettingOverrides()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"jobs.max_concurrent": "3"}, overrides)
}

func TestSettingOverrides_Delete(t *testing.T) {
	repo := setupTestRepo(t)

	require.NoError(t, repo.SetSettingOverride("jobs.max_concurrent", "8"))
	require.NoError(t, repo.DeleteSettingOverride("jobs.max_concurrent"))

	overrides, err := repo.GetSettingOverrides()
	require.NoError(t, err)
	assert.Empty(t, overrides)

	// Deleting an override that was never set is a no-op, not an error.
	assert.NoError(t, repo.DeleteSettingOverride("jobs.max_concurrent"))
}

// The overrides share the system_config table with bookkeeping keys, so the
// prefix has to keep them apart in both directions.
func TestSettingOverrides_IgnoresNonSettingKeys(t *testing.T) {
	repo := setupTestRepo(t)

	require.NoError(t, repo.SetConfig("last_cleanup", "2024-01-01T00:00:00Z"))
	require.NoError(t, repo.SetSettingOverride("jobs.max_concurrent", "8"))

	overrides, err := repo.GetSettingOverrides()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"jobs.max_concurrent": "8"}, overrides)

	// And the override must not be readable under its bare key.
	_, err = repo.GetConfig("jobs.max_concurrent")
	assert.Error(t, err)
}
