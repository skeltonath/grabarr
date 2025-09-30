package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "data", "grabarr.db")

	configContent := `
server:
  port: 8080
  host: "0.0.0.0"
  shutdown_timeout: 30s

downloads:
  local_path: "` + tmpDir + `/downloads"
  allowed_categories: ["movies", "tv"]

rclone:
  remote_name: "seedbox"
  config_file: "` + tmpDir + `/rclone.conf"
  bandwidth_limit: "10M"
  transfer_timeout: 1h
  daemon_addr: "localhost:5572"

resources:
  bandwidth:
    max_usage_percent: 80
    check_interval: 10s
  disk:
    cache_drive_path: "` + tmpDir + `/cache"
    cache_drive_min_free: "10GB"
    array_min_free: "50GB"
    check_interval: 30s

jobs:
  max_concurrent: 3
  max_retries: 3
  retry_backoff_base: 30s
  retry_backoff_max: 10m
  cleanup_completed_after: 168h
  cleanup_failed_after: 168h

database:
  path: "` + dbPath + `"

notifications:
  pushover:
    enabled: false
    token: "test-token"
    user: "test-user"
    priority: 0
    retry_interval: 60s
    expire_time: 3600s

logging:
  level: "info"
  format: "json"
  file: ""

monitoring:
  resource_check_interval: 30s
`

	// Create temp config file
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Reset global config for testing
	globalConfig = nil
	configOnce = sync.Once{}

	// Load config
	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// Verify server config
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 30*time.Second, cfg.Server.ShutdownTimeout)

	// Verify downloads config
	assert.Contains(t, cfg.Downloads.LocalPath, "downloads")
	assert.Equal(t, []string{"movies", "tv"}, cfg.Downloads.AllowedCategories)

	// Verify rclone config
	assert.Equal(t, "seedbox", cfg.Rclone.RemoteName)
	assert.Equal(t, "10M", cfg.Rclone.BandwidthLimit)

	// Verify jobs config
	assert.Equal(t, 3, cfg.Jobs.MaxConcurrent)
	assert.Equal(t, 3, cfg.Jobs.MaxRetries)
	assert.Equal(t, 30*time.Second, cfg.Jobs.RetryBackoffBase)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "invalid port - negative",
			config: &Config{
				Server: ServerConfig{Port: -1},
				Jobs:   JobsConfig{MaxConcurrent: 1},
			},
			expectError: true,
			errorMsg:    "invalid server port",
		},
		{
			name: "invalid port - too high",
			config: &Config{
				Server: ServerConfig{Port: 99999},
				Jobs:   JobsConfig{MaxConcurrent: 1},
			},
			expectError: true,
			errorMsg:    "invalid server port",
		},
		{
			name: "invalid max concurrent",
			config: &Config{
				Server: ServerConfig{Port: 8080},
				Jobs:   JobsConfig{MaxConcurrent: 0},
			},
			expectError: true,
			errorMsg:    "max_concurrent must be greater than 0",
		},
		{
			name: "invalid max retries",
			config: &Config{
				Server: ServerConfig{Port: 8080},
				Jobs:   JobsConfig{MaxConcurrent: 1, MaxRetries: -1},
			},
			expectError: true,
			errorMsg:    "max_retries cannot be negative",
		},
		{
			name: "pushover enabled without token",
			config: &Config{
				Server: ServerConfig{Port: 8080},
				Jobs:   JobsConfig{MaxConcurrent: 1},
				Notifications: NotificationsConfig{
					Pushover: PushoverConfig{
						Enabled: true,
						Token:   "${PUSHOVER_TOKEN}",
						User:    "test",
					},
				},
			},
			expectError: true,
			errorMsg:    "pushover token is required",
		},
		{
			name: "valid config",
			config: &Config{
				Server: ServerConfig{Port: 8080},
				Jobs:   JobsConfig{MaxConcurrent: 3, MaxRetries: 3},
				Notifications: NotificationsConfig{
					Pushover: PushoverConfig{Enabled: false},
				},
			},
			expectError: false,
		},
	}

	for i := range tests {
		tt := tests[i] // Create local copy to avoid range var issue
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigGetters(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Port: 8080, Host: "localhost"},
		Jobs: JobsConfig{
			MaxConcurrent:    5,
			MaxRetries:       3,
			RetryBackoffBase: 30 * time.Second,
		},
		Downloads: DownloadsConfig{
			LocalPath:         "/downloads",
			AllowedCategories: []string{"movies"},
		},
		Rclone: RcloneConfig{
			RemoteName:     "seedbox",
			BandwidthLimit: "10M",
		},
		Database: DatabaseConfig{
			Path: "/data/db.sqlite",
		},
	}

	// Test all getters
	serverCfg := cfg.GetServer()
	assert.Equal(t, 8080, serverCfg.Port)
	assert.Equal(t, "localhost", serverCfg.Host)

	jobsCfg := cfg.GetJobs()
	assert.Equal(t, 5, jobsCfg.MaxConcurrent)
	assert.Equal(t, 3, jobsCfg.MaxRetries)

	downloadsCfg := cfg.GetDownloads()
	assert.Equal(t, "/downloads", downloadsCfg.LocalPath)

	rcloneCfg := cfg.GetRClone()
	assert.Equal(t, "seedbox", rcloneCfg.RemoteName)

	dbCfg := cfg.GetDatabase()
	assert.Equal(t, "/data/db.sqlite", dbCfg.Path)
}

func TestLoadConfigWithEnvVars(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	downloadPath := filepath.Join(tmpDir, "downloads")
	dbPath := filepath.Join(tmpDir, "data", "test.db")

	configContent := `
server:
  port: 8080
  host: "${HOST_BIND}"
  shutdown_timeout: 30s

downloads:
  local_path: "${DOWNLOAD_PATH}"

rclone:
  remote_name: "seedbox"
  config_file: "` + tmpDir + `/rclone.conf"
  daemon_addr: "localhost:5572"

jobs:
  max_concurrent: 3
  max_retries: 3
  retry_backoff_base: 30s
  retry_backoff_max: 10m
  cleanup_completed_after: 168h
  cleanup_failed_after: 168h

database:
  path: "${DB_PATH}"

notifications:
  pushover:
    enabled: false
    token: ""
    user: ""

resources:
  bandwidth:
    max_usage_percent: 80
    check_interval: 10s
  disk:
    cache_drive_path: "` + tmpDir + `/cache"
    cache_drive_min_free: "10GB"
    array_min_free: "50GB"
    check_interval: 30s

logging:
  level: "info"
  format: "json"

monitoring:
  resource_check_interval: 30s
`

	// Set environment variables
	os.Setenv("HOST_BIND", "192.168.1.100")
	os.Setenv("DOWNLOAD_PATH", downloadPath)
	os.Setenv("DB_PATH", dbPath)
	defer func() {
		os.Unsetenv("HOST_BIND")
		os.Unsetenv("DOWNLOAD_PATH")
		os.Unsetenv("DB_PATH")
	}()

	// Create temp config file
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Reset global config for testing
	globalConfig = nil
	configOnce = sync.Once{}

	// Load config
	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// Verify environment variables were expanded
	assert.Equal(t, "192.168.1.100", cfg.Server.Host)
	assert.Equal(t, downloadPath, cfg.Downloads.LocalPath)
	assert.Equal(t, dbPath, cfg.Database.Path)
}

func TestConfigMissingFile(t *testing.T) {
	// Reset global config for testing
	globalConfig = nil
	configOnce = sync.Once{}

	_, err := Load("/nonexistent/config.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestConfigInvalidYAML(t *testing.T) {
	invalidYAML := `
server:
  port: invalid_port
  host: test
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(configPath, []byte(invalidYAML), 0644)
	require.NoError(t, err)

	// Reset global config for testing
	globalConfig = nil
	configOnce = sync.Once{}

	_, err = Load(configPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal config")
}
