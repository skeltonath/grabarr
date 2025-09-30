package monitor

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"

	"golang.org/x/sys/unix"
)

type Monitor struct {
	config *config.Config

	// Resource monitoring state
	mu                sync.RWMutex
	lastResourceCheck time.Time
	resourceStatus    interfaces.ResourceStatus
	metrics           map[string]interface{}

	// Bandwidth monitoring
	bandwidthMonitor interfaces.BandwidthMonitor

	// Context management
	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg *config.Config) *Monitor {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Monitor{
		config:  cfg,
		ctx:     ctx,
		cancel:  cancel,
		metrics: make(map[string]interface{}),
		bandwidthMonitor: &mockBandwidthMonitor{}, // Use mock for now
	}

	// Initialize resource status
	m.updateResourceStatus()

	return m
}

func (m *Monitor) Start() error {
	go m.monitorLoop()
	slog.Info("resource monitor started")
	return nil
}

func (m *Monitor) Stop() error {
	m.cancel()
	slog.Info("resource monitor stopped")
	return nil
}

func (m *Monitor) GetResourceStatus() interfaces.ResourceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resourceStatus
}

func (m *Monitor) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a copy to avoid race conditions
	metrics := make(map[string]interface{})
	for k, v := range m.metrics {
		metrics[k] = v
	}

	return metrics
}

func (m *Monitor) CanScheduleJob() bool {
	status := m.GetResourceStatus()
	return status.BandwidthAvailable && status.DiskSpaceAvailable
}

func (m *Monitor) monitorLoop() {
	resourceTicker := time.NewTicker(m.config.GetMonitoring().ResourceCheckInterval)
	defer resourceTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-resourceTicker.C:
			m.updateResourceStatus()
			m.updateMetrics()
		}
	}
}

func (m *Monitor) updateResourceStatus() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastResourceCheck = time.Now()

	// Check bandwidth availability
	bandwidthUsage, err := m.bandwidthMonitor.GetCurrentUsage()
	if err != nil {
		slog.Error("failed to check bandwidth usage", "error", err)
		bandwidthUsage = 0
	}

	cfg := m.config.GetResources()
	maxBandwidthUsage := float64(cfg.Bandwidth.MaxUsagePercent)

	m.resourceStatus.BandwidthUsage = bandwidthUsage
	m.resourceStatus.BandwidthAvailable = bandwidthUsage < maxBandwidthUsage

	// Check disk space availability
	cacheFreeMB, arrayFreeMB := m.checkDiskSpace()
	m.resourceStatus.CacheDiskFree = cacheFreeMB * 1024 * 1024 // Convert to bytes
	m.resourceStatus.ArrayDiskFree = arrayFreeMB * 1024 * 1024

	// Parse minimum free space requirements
	cacheMinFreeMB := m.parseSizeString(cfg.Disk.CacheDriveMinFree)
	arrayMinFreeMB := m.parseSizeString(cfg.Disk.ArrayMinFree)

	m.resourceStatus.DiskSpaceAvailable = cacheFreeMB >= cacheMinFreeMB && arrayFreeMB >= arrayMinFreeMB

	slog.Debug("resource status updated",
		"bandwidth_usage", bandwidthUsage,
		"bandwidth_available", m.resourceStatus.BandwidthAvailable,
		"cache_free_gb", cacheFreeMB/1024,
		"array_free_gb", arrayFreeMB/1024,
		"disk_space_available", m.resourceStatus.DiskSpaceAvailable,
	)
}

func (m *Monitor) updateMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	// System metrics
	m.metrics["timestamp"] = now.UTC()
	m.metrics["uptime"] = time.Since(now).String() // This will be updated by the main service

	// Resource metrics
	m.metrics["bandwidth"] = map[string]interface{}{
		"usage_percent": m.resourceStatus.BandwidthUsage,
		"available":     m.resourceStatus.BandwidthAvailable,
		"last_checked":  m.lastResourceCheck,
	}

	m.metrics["disk"] = map[string]interface{}{
		"cache_free_bytes":  m.resourceStatus.CacheDiskFree,
		"array_free_bytes":  m.resourceStatus.ArrayDiskFree,
		"space_available":   m.resourceStatus.DiskSpaceAvailable,
		"last_checked":      m.lastResourceCheck,
	}

	// Load average (Linux/Unix only)
	if loadAvg := m.getLoadAverage(); len(loadAvg) > 0 {
		m.metrics["load_average"] = loadAvg
	}

	// Memory usage
	if memInfo := m.getMemoryInfo(); memInfo != nil {
		m.metrics["memory"] = memInfo
	}
}

func (m *Monitor) checkDiskSpace() (cacheFreeMB, arrayFreeMB int64) {
	cfg := m.config.GetResources()

	// Check cache drive space
	if stat, err := m.getDiskUsage(cfg.Disk.CacheDrivePath); err == nil {
		cacheFreeMB = int64(stat.Bavail * uint64(stat.Bsize)) / (1024 * 1024)
	} else {
		slog.Error("failed to check cache drive space", "path", cfg.Disk.CacheDrivePath, "error", err)
	}

	// For array space, we'll check the parent directory of cache drive
	// In a real deployment, you might have a separate mount point for the array
	if stat, err := m.getDiskUsage("/"); err == nil {
		arrayFreeMB = int64(stat.Bavail * uint64(stat.Bsize)) / (1024 * 1024)
	} else {
		slog.Error("failed to check array space", "error", err)
	}

	return cacheFreeMB, arrayFreeMB
}

func (m *Monitor) getDiskUsage(path string) (*unix.Statfs_t, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	return &stat, err
}

func (m *Monitor) parseSizeString(sizeStr string) int64 {
	// Simple parser for size strings like "100GB", "1TB", etc.
	if len(sizeStr) < 2 {
		return 0
	}

	unit := sizeStr[len(sizeStr)-2:]
	valueStr := sizeStr[:len(sizeStr)-2]

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		slog.Error("failed to parse size string", "size", sizeStr, "error", err)
		return 0
	}

	switch unit {
	case "GB":
		return int64(value * 1024) // Return in MB
	case "TB":
		return int64(value * 1024 * 1024) // Return in MB
	case "MB":
		return int64(value)
	default:
		slog.Warn("unknown size unit", "unit", unit, "size", sizeStr)
		return int64(value) // Assume MB
	}
}

func (m *Monitor) getLoadAverage() []float64 {
	// This is a simplified implementation
	// In a real system, you might read from /proc/loadavg or use a system call
	return []float64{} // Placeholder
}

func (m *Monitor) getMemoryInfo() map[string]interface{} {
	// This is a simplified implementation
	// In a real system, you might read from /proc/meminfo
	return map[string]interface{}{
		"total":     0,
		"available": 0,
		"used":      0,
		"cached":    0,
	}
}

// Mock bandwidth monitor for testing
type mockBandwidthMonitor struct{}

func (m *mockBandwidthMonitor) GetCurrentUsage() (float64, error) {
	// Return a mock bandwidth usage between 0-50%
	// In a real implementation, this would:
	// 1. Connect to your seedbox via SSH or API
	// 2. Check current bandwidth usage (e.g., from vnstat, iftop, or router API)
	// 3. Calculate percentage based on your 1Gbps limit
	return 25.0, nil
}

func (m *mockBandwidthMonitor) IsAvailable() bool {
	return true
}

// Real bandwidth monitor implementation would look like this:
type sshBandwidthMonitor struct {
	host     string
	username string
	keyPath  string
}

func NewSSHBandwidthMonitor(host, username, keyPath string) interfaces.BandwidthMonitor {
	return &sshBandwidthMonitor{
		host:     host,
		username: username,
		keyPath:  keyPath,
	}
}

func (s *sshBandwidthMonitor) GetCurrentUsage() (float64, error) {
	// Implementation would:
	// 1. SSH to seedbox
	// 2. Run command like `vnstat -i eth0 --json` or check /proc/net/dev
	// 3. Calculate current bandwidth usage
	// 4. Return as percentage of total bandwidth

	// For now, return mock data
	return 30.0, nil
}

func (s *sshBandwidthMonitor) IsAvailable() bool {
	// Check if we can connect to the seedbox
	return true
}