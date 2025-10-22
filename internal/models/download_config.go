package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// DownloadConfig represents configurable rclone download settings
// All fields are optional - nil values will use defaults
type DownloadConfig struct {
	// Transfer settings
	Transfers *int `json:"transfers,omitempty"`
	Checkers  *int `json:"checkers,omitempty"`

	// Bandwidth limits
	BwLimit     *string `json:"bw_limit,omitempty"`
	BwLimitFile *string `json:"bw_limit_file,omitempty"`

	// SFTP settings
	SftpChunkSize   *string `json:"sftp_chunk_size,omitempty"`
	SftpConcurrency *int    `json:"sftp_concurrency,omitempty"`

	// Buffer and threading settings
	BufferSize         *string `json:"buffer_size,omitempty"`
	UseMmap            *bool   `json:"use_mmap,omitempty"`
	MultiThreadStreams *int    `json:"multi_thread_streams,omitempty"`
	MultiThreadCutoff  *string `json:"multi_thread_cutoff,omitempty"`

	// Sync behavior settings
	IgnoreExisting *bool `json:"ignore_existing,omitempty"`
	NoTraverse     *bool `json:"no_traverse,omitempty"`
	UpdateOlder    *bool `json:"update_older,omitempty"`
}

// DefaultDownloadConfig returns the default download configuration used by the system
func DefaultDownloadConfig() *DownloadConfig {
	transfers := 1
	checkers := 1
	bwLimit := "10M"
	bwLimitFile := "10M"
	bufferSize := "32M"
	useMmap := true
	multiThreadStreams := 1
	multiThreadCutoff := "10G"
	ignoreExisting := true
	noTraverse := true
	updateOlder := true

	return &DownloadConfig{
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
}

// MergeWithDefaults returns a new config with any unset fields filled from defaults
func (dc *DownloadConfig) MergeWithDefaults() *DownloadConfig {
	if dc == nil {
		return DefaultDownloadConfig()
	}

	defaults := DefaultDownloadConfig()
	merged := &DownloadConfig{}

	// Merge each field - use custom value if set, otherwise use default
	if dc.Transfers != nil {
		merged.Transfers = dc.Transfers
	} else {
		merged.Transfers = defaults.Transfers
	}

	if dc.Checkers != nil {
		merged.Checkers = dc.Checkers
	} else {
		merged.Checkers = defaults.Checkers
	}

	if dc.BwLimit != nil {
		merged.BwLimit = dc.BwLimit
	} else {
		merged.BwLimit = defaults.BwLimit
	}

	if dc.BwLimitFile != nil {
		merged.BwLimitFile = dc.BwLimitFile
	} else {
		merged.BwLimitFile = defaults.BwLimitFile
	}

	if dc.BufferSize != nil {
		merged.BufferSize = dc.BufferSize
	} else {
		merged.BufferSize = defaults.BufferSize
	}

	if dc.UseMmap != nil {
		merged.UseMmap = dc.UseMmap
	} else {
		merged.UseMmap = defaults.UseMmap
	}

	if dc.MultiThreadStreams != nil {
		merged.MultiThreadStreams = dc.MultiThreadStreams
	} else {
		merged.MultiThreadStreams = defaults.MultiThreadStreams
	}

	if dc.MultiThreadCutoff != nil {
		merged.MultiThreadCutoff = dc.MultiThreadCutoff
	} else {
		merged.MultiThreadCutoff = defaults.MultiThreadCutoff
	}

	if dc.IgnoreExisting != nil {
		merged.IgnoreExisting = dc.IgnoreExisting
	} else {
		merged.IgnoreExisting = defaults.IgnoreExisting
	}

	if dc.NoTraverse != nil {
		merged.NoTraverse = dc.NoTraverse
	} else {
		merged.NoTraverse = defaults.NoTraverse
	}

	if dc.UpdateOlder != nil {
		merged.UpdateOlder = dc.UpdateOlder
	} else {
		merged.UpdateOlder = defaults.UpdateOlder
	}

	return merged
}

// ToRCloneConfig converts the DownloadConfig to a map suitable for rclone API
func (dc *DownloadConfig) ToRCloneConfig() map[string]interface{} {
	// Merge with defaults first to ensure all fields are set
	merged := dc.MergeWithDefaults()

	config := make(map[string]interface{})

	if merged.Transfers != nil {
		config["Transfers"] = *merged.Transfers
	}
	if merged.Checkers != nil {
		config["Checkers"] = *merged.Checkers
	}
	if merged.BwLimit != nil {
		config["BwLimit"] = *merged.BwLimit
	}
	if merged.BwLimitFile != nil {
		config["BwLimitFile"] = *merged.BwLimitFile
	}
	if merged.BufferSize != nil {
		config["BufferSize"] = *merged.BufferSize
	}
	if merged.UseMmap != nil {
		config["UseMmap"] = *merged.UseMmap
	}
	if merged.MultiThreadStreams != nil {
		config["MultiThreadStreams"] = *merged.MultiThreadStreams
	}
	if merged.MultiThreadCutoff != nil {
		config["MultiThreadCutoff"] = *merged.MultiThreadCutoff
	}
	if merged.IgnoreExisting != nil {
		config["IgnoreExisting"] = *merged.IgnoreExisting
	}
	if merged.NoTraverse != nil {
		config["NoTraverse"] = *merged.NoTraverse
	}
	if merged.UpdateOlder != nil {
		config["UpdateOlder"] = *merged.UpdateOlder
	}

	return config
}

// Database value methods for custom types
func (dc DownloadConfig) Value() (driver.Value, error) {
	return json.Marshal(dc)
}

func (dc *DownloadConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into DownloadConfig", value)
	}

	return json.Unmarshal(bytes, dc)
}
