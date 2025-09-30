package gatekeeper

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/interfaces"

	"golang.org/x/sys/unix"
)

// Gatekeeper manages resource constraints and enforces operational rules
type Gatekeeper struct {
	config         *config.Config
	syncRepository interfaces.SyncRepository
	rcloneClient   interfaces.RCloneClient

	mu             sync.RWMutex
	bandwidthUsage float64 // Current bandwidth usage in Mbps
	cacheUsage     float64 // Current cache usage percentage
	lastCheck      time.Time

	ctx    context.Context
	cancel context.CancelFunc
}

// GateDecision represents whether an operation can proceed
type GateDecision struct {
	Allowed bool
	Reason  string
	Details map[string]interface{}
}

func New(cfg *config.Config, syncRepo interfaces.SyncRepository, rcloneClient interfaces.RCloneClient) *Gatekeeper {
	ctx, cancel := context.WithCancel(context.Background())

	return &Gatekeeper{
		config:         cfg,
		syncRepository: syncRepo,
		rcloneClient:   rcloneClient,
		ctx:            ctx,
		cancel:         cancel,
		lastCheck:      time.Now(),
	}
}

func (g *Gatekeeper) Start() error {
	// Initial check
	g.updateResourceStatus()

	// Start monitoring loop
	go g.monitorLoop()

	slog.Info("gatekeeper started")
	return nil
}

func (g *Gatekeeper) Stop() error {
	g.cancel()
	slog.Info("gatekeeper stopped")
	return nil
}

// CanStartJob checks if a new job can be started
func (g *Gatekeeper) CanStartJob(fileSize int64) interfaces.GateDecision {
	g.mu.RLock()
	defer g.mu.RUnlock()

	gatekeeperCfg := g.config.GetGatekeeper()

	// Rule 1: Block if any syncs are running
	if gatekeeperCfg.Rules.BlockJobsDuringSync {
		activeSyncs, err := g.syncRepository.GetActiveSyncJobsCount()
		if err != nil {
			slog.Error("failed to check active syncs", "error", err)
			return interfaces.GateDecision{
				Allowed: false,
				Reason:  "Unable to verify sync status",
			}
		}

		if activeSyncs > 0 {
			return interfaces.GateDecision{
				Allowed: false,
				Reason:  "Sync operation in progress",
				Details: map[string]interface{}{
					"active_syncs": activeSyncs,
				},
			}
		}
	}

	// Rule 2: Check bandwidth availability
	if g.bandwidthUsage >= float64(gatekeeperCfg.Seedbox.BandwidthLimitMbps) {
		return interfaces.GateDecision{
			Allowed: false,
			Reason:  "Bandwidth limit reached",
			Details: map[string]interface{}{
				"current_mbps": g.bandwidthUsage,
				"limit_mbps":   gatekeeperCfg.Seedbox.BandwidthLimitMbps,
			},
		}
	}

	// Rule 3: Check cache disk space
	cacheMaxPercent := float64(gatekeeperCfg.CacheDisk.MaxUsagePercent)
	if g.cacheUsage >= cacheMaxPercent {
		return interfaces.GateDecision{
			Allowed: false,
			Reason:  "Cache disk usage too high",
			Details: map[string]interface{}{
				"current_percent": g.cacheUsage,
				"max_percent":     cacheMaxPercent,
			},
		}
	}

	// Rule 4: Check if filesize fits in available space
	if gatekeeperCfg.Rules.RequireFilesizeCheck && fileSize > 0 {
		stat, err := g.getCacheDiskStats()
		if err != nil {
			slog.Error("failed to check cache disk stats", "error", err)
			return interfaces.GateDecision{
				Allowed: false,
				Reason:  "Unable to verify disk space",
			}
		}

		availableBytes := int64(stat.Bavail * uint64(stat.Bsize))

		// Calculate what usage would be after this job
		totalBytes := int64(stat.Blocks * uint64(stat.Bsize))
		usedBytes := totalBytes - availableBytes
		projectedUsedBytes := usedBytes + fileSize
		projectedUsagePercent := float64(projectedUsedBytes) / float64(totalBytes) * 100

		if projectedUsagePercent > cacheMaxPercent {
			return interfaces.GateDecision{
				Allowed: false,
				Reason:  "File size would exceed cache limit",
				Details: map[string]interface{}{
					"file_size_bytes":         fileSize,
					"available_bytes":         availableBytes,
					"projected_usage_percent": projectedUsagePercent,
					"max_percent":             cacheMaxPercent,
				},
			}
		}
	}

	return interfaces.GateDecision{
		Allowed: true,
		Reason:  "All checks passed",
	}
}

// CanStartSync checks if a new sync operation can be started
func (g *Gatekeeper) CanStartSync() interfaces.GateDecision {
	g.mu.RLock()
	defer g.mu.RUnlock()

	gatekeeperCfg := g.config.GetGatekeeper()

	// Rule 1: Only one sync at a time
	activeSyncs, err := g.syncRepository.GetActiveSyncJobsCount()
	if err != nil {
		slog.Error("failed to check active syncs", "error", err)
		return interfaces.GateDecision{
			Allowed: false,
			Reason:  "Unable to verify sync status",
		}
	}

	if activeSyncs > 0 {
		return interfaces.GateDecision{
			Allowed: false,
			Reason:  "Another sync is already running",
			Details: map[string]interface{}{
				"active_syncs": activeSyncs,
			},
		}
	}

	// Rule 2: Check bandwidth availability
	if g.bandwidthUsage >= float64(gatekeeperCfg.Seedbox.BandwidthLimitMbps) {
		return interfaces.GateDecision{
			Allowed: false,
			Reason:  "Bandwidth limit reached",
			Details: map[string]interface{}{
				"current_mbps": g.bandwidthUsage,
				"limit_mbps":   gatekeeperCfg.Seedbox.BandwidthLimitMbps,
			},
		}
	}

	// Rule 3: Check cache disk space (need some headroom for syncs)
	cacheMaxPercent := float64(gatekeeperCfg.CacheDisk.MaxUsagePercent)
	if g.cacheUsage >= cacheMaxPercent-10 { // Leave 10% buffer for syncs
		return interfaces.GateDecision{
			Allowed: false,
			Reason:  "Insufficient cache space for sync",
			Details: map[string]interface{}{
				"current_percent":  g.cacheUsage,
				"required_percent": cacheMaxPercent - 10,
			},
		}
	}

	return interfaces.GateDecision{
		Allowed: true,
		Reason:  "All checks passed",
	}
}

// GetResourceStatus returns current resource status
func (g *Gatekeeper) GetResourceStatus() interfaces.GatekeeperResourceStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()

	gatekeeperCfg := g.config.GetGatekeeper()

	activeSyncs := 0
	count, err := g.syncRepository.GetActiveSyncJobsCount()
	if err == nil {
		activeSyncs = count
	}

	var cacheFreeBytes, cacheTotalBytes int64
	if stat, err := g.getCacheDiskStats(); err == nil {
		cacheFreeBytes = int64(stat.Bavail * uint64(stat.Bsize))
		cacheTotalBytes = int64(stat.Blocks * uint64(stat.Bsize))
	}

	return interfaces.GatekeeperResourceStatus{
		BandwidthUsageMbps: g.bandwidthUsage,
		BandwidthLimitMbps: gatekeeperCfg.Seedbox.BandwidthLimitMbps,
		CacheUsagePercent:  g.cacheUsage,
		CacheMaxPercent:    gatekeeperCfg.CacheDisk.MaxUsagePercent,
		CacheFreeBytes:     cacheFreeBytes,
		CacheTotalBytes:    cacheTotalBytes,
		ActiveSyncs:        activeSyncs,
	}
}

func (g *Gatekeeper) monitorLoop() {
	gatekeeperCfg := g.config.GetGatekeeper()

	// Use the shorter of the two check intervals
	checkInterval := gatekeeperCfg.Seedbox.CheckInterval
	if gatekeeperCfg.CacheDisk.CheckInterval < checkInterval {
		checkInterval = gatekeeperCfg.CacheDisk.CheckInterval
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.updateResourceStatus()
		}
	}
}

func (g *Gatekeeper) updateResourceStatus() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.lastCheck = time.Now()

	// Update bandwidth usage
	bandwidthUsage, err := g.checkBandwidthUsage()
	if err != nil {
		slog.Error("failed to check bandwidth usage", "error", err)
		// Keep previous value
	} else {
		g.bandwidthUsage = bandwidthUsage
	}

	// Update cache usage
	cacheUsage, err := g.checkCacheUsage()
	if err != nil {
		slog.Error("failed to check cache usage", "error", err)
		// Keep previous value
	} else {
		g.cacheUsage = cacheUsage
	}

	slog.Debug("resource status updated",
		"bandwidth_mbps", g.bandwidthUsage,
		"cache_percent", g.cacheUsage,
	)
}

func (g *Gatekeeper) checkBandwidthUsage() (float64, error) {
	// Query rclone daemon for current transfer stats
	ctx, cancel := context.WithTimeout(g.ctx, 5*time.Second)
	defer cancel()

	jobs, err := g.rcloneClient.ListJobs(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list rclone jobs: %w", err)
	}

	// Sum up bandwidth from all active jobs
	var totalBytesPerSecond float64
	for _, jobID := range jobs.JobIDs {
		status, err := g.rcloneClient.GetJobStatus(ctx, jobID)
		if err != nil {
			slog.Warn("failed to get job status", "job_id", jobID, "error", err)
			continue
		}

		if !status.Finished {
			// Speed is in bytes per second
			totalBytesPerSecond += status.Output.Speed
		}
	}

	// Convert bytes per second to Mbps
	mbps := (totalBytesPerSecond * 8) / 1_000_000

	return mbps, nil
}

func (g *Gatekeeper) checkCacheUsage() (float64, error) {
	stat, err := g.getCacheDiskStats()
	if err != nil {
		return 0, err
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	availableBytes := stat.Bavail * uint64(stat.Bsize)
	usedBytes := totalBytes - availableBytes

	usagePercent := float64(usedBytes) / float64(totalBytes) * 100

	return usagePercent, nil
}

func (g *Gatekeeper) getCacheDiskStats() (*unix.Statfs_t, error) {
	gatekeeperCfg := g.config.GetGatekeeper()

	var stat unix.Statfs_t
	err := unix.Statfs(gatekeeperCfg.CacheDisk.Path, &stat)
	if err != nil {
		return nil, fmt.Errorf("failed to stat cache disk: %w", err)
	}

	return &stat, nil
}
