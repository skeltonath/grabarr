package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDownloadConfig(t *testing.T) {
	config := DefaultDownloadConfig()

	// Verify all fields are set
	require.NotNil(t, config.Transfers)
	assert.Equal(t, 1, *config.Transfers)

	require.NotNil(t, config.Checkers)
	assert.Equal(t, 1, *config.Checkers)

	require.NotNil(t, config.BwLimit)
	assert.Equal(t, "10M", *config.BwLimit)

	require.NotNil(t, config.BwLimitFile)
	assert.Equal(t, "10M", *config.BwLimitFile)

	require.NotNil(t, config.BufferSize)
	assert.Equal(t, "32M", *config.BufferSize)

	require.NotNil(t, config.UseMmap)
	assert.Equal(t, true, *config.UseMmap)

	require.NotNil(t, config.MultiThreadStreams)
	assert.Equal(t, 1, *config.MultiThreadStreams)

	require.NotNil(t, config.MultiThreadCutoff)
	assert.Equal(t, "10G", *config.MultiThreadCutoff)

	require.NotNil(t, config.IgnoreExisting)
	assert.Equal(t, true, *config.IgnoreExisting)

	require.NotNil(t, config.NoTraverse)
	assert.Equal(t, true, *config.NoTraverse)

	require.NotNil(t, config.UpdateOlder)
	assert.Equal(t, true, *config.UpdateOlder)
}

func TestDownloadConfig_MergeWithDefaults_NilConfig(t *testing.T) {
	var config *DownloadConfig = nil
	merged := config.MergeWithDefaults()

	// Should return full default config
	defaults := DefaultDownloadConfig()
	assert.Equal(t, defaults, merged)
}

func TestDownloadConfig_MergeWithDefaults_EmptyConfig(t *testing.T) {
	config := &DownloadConfig{}
	merged := config.MergeWithDefaults()

	// Should return full default config
	defaults := DefaultDownloadConfig()
	assert.Equal(t, defaults, merged)
}

func TestDownloadConfig_MergeWithDefaults_PartialConfig(t *testing.T) {
	transfers := 5
	bwLimit := "50M"
	config := &DownloadConfig{
		Transfers: &transfers,
		BwLimit:   &bwLimit,
		// Other fields are nil
	}

	merged := config.MergeWithDefaults()

	// Custom values should be preserved
	require.NotNil(t, merged.Transfers)
	assert.Equal(t, 5, *merged.Transfers)

	require.NotNil(t, merged.BwLimit)
	assert.Equal(t, "50M", *merged.BwLimit)

	// Other fields should be filled from defaults
	require.NotNil(t, merged.Checkers)
	assert.Equal(t, 1, *merged.Checkers)

	require.NotNil(t, merged.BwLimitFile)
	assert.Equal(t, "10M", *merged.BwLimitFile)
}

func TestDownloadConfig_MergeWithDefaults_FullCustomConfig(t *testing.T) {
	transfers := 10
	checkers := 8
	bwLimit := "100M"
	bwLimitFile := "50M"
	bufferSize := "64M"
	useMmap := false
	multiThreadStreams := 4
	multiThreadCutoff := "50G"
	ignoreExisting := false
	noTraverse := false
	updateOlder := false

	config := &DownloadConfig{
		Transfers:          &transfers,
		Checkers:           &checkers,
		BwLimit:            &bwLimit,
		BwLimitFile:        &bwLimitFile,
		BufferSize:         &bufferSize,
		UseMmap:            &useMmap,
		MultiThreadStreams: &multiThreadStreams,
		MultiThreadCutoff:  &multiThreadCutoff,
		IgnoreExisting:     &ignoreExisting,
		NoTraverse:         &noTraverse,
		UpdateOlder:        &updateOlder,
	}

	merged := config.MergeWithDefaults()

	// All values should be the custom values
	assert.Equal(t, config, merged)
}

func TestDownloadConfig_ToRCloneConfig_NilConfig(t *testing.T) {
	var config *DownloadConfig = nil
	rcloneConfig := config.ToRCloneConfig()

	// Should return default config as map
	assert.Equal(t, 1, rcloneConfig["Transfers"])
	assert.Equal(t, 1, rcloneConfig["Checkers"])
	assert.Equal(t, "10M", rcloneConfig["BwLimit"])
	assert.Equal(t, "10M", rcloneConfig["BwLimitFile"])
	assert.Equal(t, "32M", rcloneConfig["BufferSize"])
	assert.Equal(t, true, rcloneConfig["UseMmap"])
	assert.Equal(t, 1, rcloneConfig["MultiThreadStreams"])
	assert.Equal(t, "10G", rcloneConfig["MultiThreadCutoff"])
	assert.Equal(t, true, rcloneConfig["IgnoreExisting"])
	assert.Equal(t, true, rcloneConfig["NoTraverse"])
	assert.Equal(t, true, rcloneConfig["UpdateOlder"])
}

func TestDownloadConfig_ToRCloneConfig_CustomConfig(t *testing.T) {
	transfers := 5
	bwLimit := "50M"
	config := &DownloadConfig{
		Transfers: &transfers,
		BwLimit:   &bwLimit,
	}

	rcloneConfig := config.ToRCloneConfig()

	// Custom values should be in the map
	assert.Equal(t, 5, rcloneConfig["Transfers"])
	assert.Equal(t, "50M", rcloneConfig["BwLimit"])

	// Default values should fill in the rest
	assert.Equal(t, 1, rcloneConfig["Checkers"])
	assert.Equal(t, "10M", rcloneConfig["BwLimitFile"])
}

func TestDownloadConfig_DatabaseSerialization(t *testing.T) {
	transfers := 5
	bwLimit := "50M"
	config := &DownloadConfig{
		Transfers: &transfers,
		BwLimit:   &bwLimit,
	}

	// Test Value (marshal)
	value, err := config.Value()
	require.NoError(t, err)
	require.NotNil(t, value)

	// Test Scan (unmarshal)
	var scanned DownloadConfig
	err = scanned.Scan(value)
	require.NoError(t, err)

	assert.Equal(t, config, &scanned)
}

func TestDownloadConfig_Scan_Nil(t *testing.T) {
	var config DownloadConfig
	err := config.Scan(nil)
	require.NoError(t, err)
}

func TestDownloadConfig_Scan_InvalidType(t *testing.T) {
	var config DownloadConfig
	err := config.Scan(12345)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot scan")
}
