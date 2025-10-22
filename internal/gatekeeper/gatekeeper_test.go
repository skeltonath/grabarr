package gatekeeper

import (
	"testing"
	"time"

	"grabarr/internal/config"
	"grabarr/internal/mocks"
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
				RequireFilesizeCheck: true,
			},
		},
	}
}

func TestCanStartJob_Success(t *testing.T) {
	cfg := createTestConfig()
	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, rcloneClient)

	decision := gk.CanStartJob(0)

	if !decision.Allowed {
		t.Errorf("Expected job to be allowed, but got: %s", decision.Reason)
	}
}

func TestCanStartJob_BandwidthExceeded_Blocked(t *testing.T) {
	cfg := createTestConfig()
	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, rcloneClient)

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
	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, rcloneClient)

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

func TestGetResourceStatus(t *testing.T) {
	cfg := createTestConfig()
	rcloneClient := mocks.NewMockRCloneClient(t)

	gk := New(cfg, rcloneClient)
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
}
