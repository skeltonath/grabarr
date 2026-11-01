package repository

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func columns(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		out = append(out, name)
	}
	return out
}

func TestMigrate_FreshDatabase(t *testing.T) {
	repo := setupTestRepo(t)

	var version int
	var dirty bool
	require.NoError(t, repo.db.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, 2, version)
	assert.False(t, dirty)

	assert.NotContains(t, columns(t, repo.db, "jobs"), "download_config")
	for _, table := range []string{"jobs", "job_attempts", "system_config", "remote_files", "users", "sessions", "api_tokens"} {
		assert.NotEmpty(t, columns(t, repo.db, table), "table %s", table)
	}
}

// A database created by the old schema.sql + ALTER TABLE code (like
// production's) has no schema_migrations table. It must adopt the migrations,
// keep its rows and lose only download_config.
func TestMigrate_AdoptsPreMigrationDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	initial, err := os.ReadFile("migrations/0001_initial.up.sql")
	require.NoError(t, err)

	old, err := sql.Open("sqlite", buildDSN(path))
	require.NoError(t, err)
	_, err = old.Exec(string(initial))
	require.NoError(t, err)
	_, err = old.Exec(`INSERT INTO jobs (name, remote_path, local_path, download_config, created_at)
		VALUES ('old job', '/r/old.mkv', '/l/', '{"transfers":4}', '2026-10-01 12:00:00')`)
	require.NoError(t, err)
	_, err = old.Exec(`INSERT INTO system_config (key, value) VALUES ('setting.jobs.max_concurrent', '2')`)
	require.NoError(t, err)
	require.NoError(t, old.Close())

	repo, err := New(path)
	require.NoError(t, err)
	defer repo.Close()

	assert.NotContains(t, columns(t, repo.db, "jobs"), "download_config")
	job, err := repo.GetJob(1)
	require.NoError(t, err)
	assert.Equal(t, "old job", job.Name)
	assert.Equal(t, 2026, job.CreatedAt.Year(), "timestamps written by the old driver still parse")

	overrides, err := repo.GetSettingOverrides()
	require.NoError(t, err)
	assert.Equal(t, "2", overrides["jobs.max_concurrent"])

	// Opening again is a no-op.
	require.NoError(t, repo.Close())
	repo, err = New(path)
	require.NoError(t, err)
	_, err = repo.GetJob(1)
	assert.NoError(t, err)
}
