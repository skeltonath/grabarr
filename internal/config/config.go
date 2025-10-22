package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/goccy/go-yaml"
)

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Downloads     DownloadsConfig     `yaml:"downloads"`
	Rclone        RcloneConfig        `yaml:"rclone"`
	Rsync         RsyncConfig         `yaml:"rsync"`
	Gatekeeper    GatekeeperConfig    `yaml:"gatekeeper"`
	Jobs          JobsConfig          `yaml:"jobs"`
	Database      DatabaseConfig      `yaml:"database"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Logging       LoggingConfig       `yaml:"logging"`

	mu       sync.RWMutex
	watchers []chan<- struct{}
}

type ServerConfig struct {
	Port            int           `yaml:"port"`
	Host            string        `yaml:"host"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type DownloadsConfig struct {
	LocalPath         string   `yaml:"local_path"`
	AllowedCategories []string `yaml:"allowed_categories"`
}

type RcloneConfig struct {
	RemoteName      string        `yaml:"remote_name"`
	ConfigFile      string        `yaml:"config_file"`
	BandwidthLimit  string        `yaml:"bandwidth_limit"`
	TransferTimeout time.Duration `yaml:"transfer_timeout"`
	AdditionalArgs  []string      `yaml:"additional_args"`
	DaemonAddr      string        `yaml:"daemon_addr"`
}

type RsyncConfig struct {
	SSHHost    string `yaml:"ssh_host"`
	SSHUser    string `yaml:"ssh_user"`
	SSHKeyFile string `yaml:"ssh_key_file"`
}

type GatekeeperConfig struct {
	Seedbox   SeedboxConfig   `yaml:"seedbox"`
	CacheDisk CacheDiskConfig `yaml:"cache_disk"`
	Rules     GatekeeperRules `yaml:"rules"`
}

type SeedboxConfig struct {
	BandwidthLimitMbps int           `yaml:"bandwidth_limit_mbps"`
	CheckInterval      time.Duration `yaml:"check_interval"`
}

type CacheDiskConfig struct {
	Path            string        `yaml:"path"`
	MaxUsagePercent int           `yaml:"max_usage_percent"`
	CheckInterval   time.Duration `yaml:"check_interval"`
}

type GatekeeperRules struct {
	BlockJobsDuringSync  bool `yaml:"block_jobs_during_sync"`
	RequireFilesizeCheck bool `yaml:"require_filesize_check"`
}

type JobsConfig struct {
	MaxConcurrent         int           `yaml:"max_concurrent"`
	MaxRetries            int           `yaml:"max_retries"`
	RetryBackoffBase      time.Duration `yaml:"retry_backoff_base"`
	RetryBackoffMax       time.Duration `yaml:"retry_backoff_max"`
	CleanupCompletedAfter time.Duration `yaml:"cleanup_completed_after"`
	CleanupFailedAfter    time.Duration `yaml:"cleanup_failed_after"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type NotificationsConfig struct {
	Pushover PushoverConfig `yaml:"pushover"`
}

type PushoverConfig struct {
	Token         string        `yaml:"token"`
	User          string        `yaml:"user"`
	Enabled       bool          `yaml:"enabled"`
	Priority      int           `yaml:"priority"`
	RetryInterval time.Duration `yaml:"retry_interval"`
	ExpireTime    time.Duration `yaml:"expire_time"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	File   string `yaml:"file"`
}

var (
	globalConfig *Config
	configOnce   sync.Once
)

// Load loads configuration from file with environment variable expansion
func Load(configPath string) (*Config, error) {
	var err error
	configOnce.Do(func() {
		globalConfig, err = loadConfig(configPath)
		if err == nil && globalConfig != nil {
			go globalConfig.watchConfig(configPath)
		}
	})
	return globalConfig, err
}

// Get returns the global configuration instance
func Get() *Config {
	if globalConfig == nil {
		panic("configuration not loaded - call Load() first")
	}
	return globalConfig
}

func loadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	content := os.ExpandEnv(string(data))

	var config Config
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	// Ensure directories exist
	if err := config.ensureDirectories(); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	return &config, nil
}

func (c *Config) validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Jobs.MaxConcurrent <= 0 {
		return fmt.Errorf("max_concurrent must be greater than 0")
	}

	if c.Jobs.MaxRetries < 0 {
		return fmt.Errorf("max_retries cannot be negative")
	}

	if c.Notifications.Pushover.Enabled {
		if c.Notifications.Pushover.Token == "" || strings.HasPrefix(c.Notifications.Pushover.Token, "${") {
			return fmt.Errorf("pushover token is required when notifications are enabled")
		}
		if c.Notifications.Pushover.User == "" || strings.HasPrefix(c.Notifications.Pushover.User, "${") {
			return fmt.Errorf("pushover user is required when notifications are enabled")
		}
	}

	return nil
}

func (c *Config) ensureDirectories() error {
	dirs := []string{
		filepath.Dir(c.Database.Path),
	}

	if c.Logging.File != "" {
		dirs = append(dirs, filepath.Dir(c.Logging.File))
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// WatchForChanges registers a channel to receive notifications when config changes
func (c *Config) WatchForChanges() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan struct{}, 1)
	c.watchers = append(c.watchers, ch)
	return ch
}

func (c *Config) watchConfig(configPath string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("failed to create config watcher", "error", err)
		return
	}
	defer watcher.Close()

	configDir := filepath.Dir(configPath)
	if err := watcher.Add(configDir); err != nil {
		slog.Error("failed to watch config directory", "error", err, "path", configDir)
		return
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Check if the config file was modified
			if filepath.Base(event.Name) == filepath.Base(configPath) &&
				(event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
				slog.Info("config file changed, reloading", "file", configPath)

				// Small delay to ensure file write is complete
				time.Sleep(100 * time.Millisecond)

				if err := c.reload(configPath); err != nil {
					slog.Error("failed to reload config", "error", err)
				} else {
					c.notifyWatchers()
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("config watcher error", "error", err)
		}
	}
}

func (c *Config) reload(configPath string) error {
	newConfig, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update all fields
	c.Server = newConfig.Server
	c.Rclone = newConfig.Rclone
	c.Gatekeeper = newConfig.Gatekeeper
	c.Jobs = newConfig.Jobs
	c.Database = newConfig.Database
	c.Notifications = newConfig.Notifications
	c.Logging = newConfig.Logging

	slog.Info("configuration reloaded successfully")
	return nil
}

func (c *Config) notifyWatchers() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, watcher := range c.watchers {
		select {
		case watcher <- struct{}{}:
		default:
			// Non-blocking send - if buffer is full, skip
		}
	}
}

// GetRClone returns a copy of the rclone configuration
func (c *Config) GetRClone() RcloneConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Rclone
}

func (c *Config) GetRsync() RsyncConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Rsync
}

// GetJobs returns a copy of the jobs configuration
func (c *Config) GetJobs() JobsConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Jobs
}

// GetServer returns a copy of the server configuration
func (c *Config) GetServer() ServerConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Server
}

// GetDownloads returns a copy of the downloads configuration
func (c *Config) GetDownloads() DownloadsConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Downloads
}

// GetGatekeeper returns a copy of the gatekeeper configuration
func (c *Config) GetGatekeeper() GatekeeperConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Gatekeeper
}

// GetDatabase returns a copy of the database configuration
func (c *Config) GetDatabase() DatabaseConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Database
}

// GetNotifications returns a copy of the notifications configuration
func (c *Config) GetNotifications() NotificationsConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Notifications
}

// GetLogging returns a copy of the logging configuration
func (c *Config) GetLogging() LoggingConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Logging
}
