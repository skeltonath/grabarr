package gatekeeper

import (
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
	"grabarr/internal/rclone"

	"github.com/stretchr/testify/mock"
)

func createTestConfig() *config.Config {
	return &config.Config{
		Gatekeeper: config.GatekeeperConfig{
			Seedbox: config.SeedboxConfig{
				BandwidthLimitMbps: 500,
				CheckInterval:      30 * time.Second,
			},
			CacheDisk: config.CacheDiskConfig{
				Path:            "/tmp",
				MaxUsagePercent: 80,
				CheckInterval:   30 * time.Second,
			},
			Rules: config.GatekeeperRules{
				BlockJobsDuringSync:  true,
				RequireFilesizeCheck: true,
			},
		},
	}
}

func TestCanStartJob_NoSyncs_Success(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(0, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)

	decision := gk.CanStartJob(0)

	if !decision.Allowed {
		t.Errorf("Expected job to be allowed, but got: %s", decision.Reason)
	}
}

func TestCanStartJob_SyncRunning_Blocked(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(1, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)

	decision := gk.CanStartJob(0)

	if decision.Allowed {
		t.Error("Expected job to be blocked when sync is running")
	}

	if decision.Reason != "Sync operation in progress" {
		t.Errorf("Expected reason 'Sync operation in progress', got: %s", decision.Reason)
	}
}

func TestCanStartJob_BandwidthExceeded_Blocked(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(0, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)

	// Manually set bandwidth usage to exceed limit
	gk.bandwidthUsage = 600 // Exceeds 500Mbps limit

	decision := gk.CanStartJob(0)

	if decision.Allowed {
		t.Error("Expected job to be blocked when bandwidth limit exceeded")
	}

	if decision.Reason != "Bandwidth limit reached" {
		t.Errorf("Expected reason 'Bandwidth limit reached', got: %s", decision.Reason)
	}
}

func TestCanStartJob_CacheUsageHigh_Blocked(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(0, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)

	// Manually set cache usage to exceed limit
	gk.cacheUsage = 85 // Exceeds 80% limit

	decision := gk.CanStartJob(0)

	if decision.Allowed {
		t.Error("Expected job to be blocked when cache usage high")
	}

	if decision.Reason != "Cache disk usage too high" {
		t.Errorf("Expected reason 'Cache disk usage too high', got: %s", decision.Reason)
	}
}

func TestCanStartSync_NoSyncs_Success(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(0, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)
	rcloneClient.EXPECT().GetCoreStats(mock.Anything).Return(&rclone.CoreStats{}, nil).Maybe()

	gk := New(cfg, syncRepo, rcloneClient)

	decision := gk.CanStartSync()

	if !decision.Allowed {
		t.Errorf("Expected sync to be allowed, but got: %s", decision.Reason)
	}
}

func TestCanStartSync_SyncRunning_Blocked(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(1, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)

	decision := gk.CanStartSync()

	if decision.Allowed {
		t.Error("Expected sync to be blocked when another sync is running")
	}

	if decision.Reason != "Another sync is already running" {
		t.Errorf("Expected reason 'Another sync is already running', got: %s", decision.Reason)
	}
}

func TestCanStartSync_BandwidthExceeded_Blocked(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(0, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)

	// Manually set bandwidth usage to exceed limit
	gk.bandwidthUsage = 600 // Exceeds 500Mbps limit

	decision := gk.CanStartSync()

	if decision.Allowed {
		t.Error("Expected sync to be blocked when bandwidth limit exceeded")
	}

	if decision.Reason != "Bandwidth limit reached" {
		t.Errorf("Expected reason 'Bandwidth limit reached', got: %s", decision.Reason)
	}
}

func TestCanStartSync_CacheUsageHigh_Blocked(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(0, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)

	// Manually set cache usage to be above threshold (80% - 10% buffer = 70%)
	gk.cacheUsage = 75 // Should be blocked

	decision := gk.CanStartSync()

	if decision.Allowed {
		t.Error("Expected sync to be blocked when cache usage high")
	}

	if decision.Reason != "Insufficient cache space for sync" {
		t.Errorf("Expected reason 'Insufficient cache space for sync', got: %s", decision.Reason)
	}
}

func TestGetResourceStatus(t *testing.T) {
	cfg := createTestConfig()

	syncRepo := mocks.NewMockSyncRepository(t)
	syncRepo.EXPECT().GetActiveSyncJobsCount().Return(2, nil)

	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)
	gk.bandwidthUsage = 250.5
	gk.cacheUsage = 45.2

	status := gk.GetResourceStatus()

	if status.BandwidthUsageMbps != 250.5 {
		t.Errorf("Expected bandwidth usage 250.5, got: %f", status.BandwidthUsageMbps)
	}

	if status.BandwidthLimitMbps != 500 {
		t.Errorf("Expected bandwidth limit 500, got: %d", status.BandwidthLimitMbps)
	}

	if status.CacheUsagePercent != 45.2 {
		t.Errorf("Expected cache usage 45.2%%, got: %f", status.CacheUsagePercent)
	}

	if status.CacheMaxPercent != 80 {
		t.Errorf("Expected cache max 80%%, got: %d", status.CacheMaxPercent)
	}

	if status.ActiveSyncs != 2 {
		t.Errorf("Expected 2 active syncs, got: %d", status.ActiveSyncs)
	}
}

func TestRulesCanBeDisabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Gatekeeper.Rules.BlockJobsDuringSync = false

	syncRepo := mocks.NewMockSyncRepository(t)
	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, syncRepo, rcloneClient)

	decision := gk.CanStartJob(0)

	// Should be allowed since rule is disabled
	if !decision.Allowed {
		t.Errorf("Expected job to be allowed when BlockJobsDuringSync is false, but got: %s", decision.Reason)
	}
}
