package main

import (
	"encoding/json"
	"fmt"
)

// CacheConfig holds the plugin configuration
type CacheConfig struct {
	Enabled               bool     `json:"enabled"`
	TTLSeconds            int      `json:"ttl_seconds"`
	MaxEntrySizeKB        int64    `json:"max_entry_size_kb"`
	MaxCacheSizeMB        int64    `json:"max_cache_size_mb"`
	Namespaces            []string `json:"namespaces"`
	NormalizePrompts      bool     `json:"normalize_prompts"`
	ExposeCacheKeyHeader  bool     `json:"expose_cache_key_header"`
	ReportIntervalSeconds int      `json:"report_interval_seconds"` // How often to send stats to control (edge mode)
}

// DefaultConfig returns the default configuration
func DefaultConfig() *CacheConfig {
	return &CacheConfig{
		Enabled:               true,
		TTLSeconds:            3600,
		MaxEntrySizeKB:        2048,
		MaxCacheSizeMB:        256,
		Namespaces:            []string{"api_key"},
		NormalizePrompts:      true,
		ExposeCacheKeyHeader:  false,
		ReportIntervalSeconds: 60, // Report stats to control every 60 seconds
	}
}

// ParseConfig parses configuration from the plugin config map
// The config map contains individual keys from the plugin's config JSON,
// with complex types (arrays, objects) JSON-encoded as strings.
func ParseConfig(configMap map[string]string) (*CacheConfig, error) {
	config := DefaultConfig()

	// Parse individual config keys (AI Studio passes config as flat key-value pairs)
	if enabled, ok := configMap["enabled"]; ok {
		config.Enabled = enabled == "true"
	}

	if ttl, ok := configMap["ttl_seconds"]; ok {
		if val, err := parseInt(ttl); err == nil {
			config.TTLSeconds = val
		}
	}

	if maxEntry, ok := configMap["max_entry_size_kb"]; ok {
		if val, err := parseInt64(maxEntry); err == nil {
			config.MaxEntrySizeKB = val
		}
	}

	if maxCache, ok := configMap["max_cache_size_mb"]; ok {
		if val, err := parseInt64(maxCache); err == nil {
			config.MaxCacheSizeMB = val
		}
	}

	if namespaces, ok := configMap["namespaces"]; ok && namespaces != "" {
		// Namespaces is JSON-encoded array
		var ns []string
		if err := json.Unmarshal([]byte(namespaces), &ns); err == nil && len(ns) > 0 {
			config.Namespaces = ns
		}
	}

	if normalize, ok := configMap["normalize_prompts"]; ok {
		config.NormalizePrompts = normalize == "true"
	}

	if exposeKey, ok := configMap["expose_cache_key_header"]; ok {
		config.ExposeCacheKeyHeader = exposeKey == "true"
	}

	if reportInterval, ok := configMap["report_interval_seconds"]; ok {
		if val, err := parseInt(reportInterval); err == nil {
			config.ReportIntervalSeconds = val
		}
	}

	// Validate configuration
	if config.TTLSeconds < 60 {
		config.TTLSeconds = 60
	}
	if config.TTLSeconds > 86400 {
		config.TTLSeconds = 86400
	}
	if config.MaxEntrySizeKB < 1 {
		config.MaxEntrySizeKB = 1
	}
	if config.MaxCacheSizeMB < 1 {
		config.MaxCacheSizeMB = 1
	}
	if len(config.Namespaces) == 0 {
		config.Namespaces = []string{"api_key"}
	}
	if config.ReportIntervalSeconds < 10 {
		config.ReportIntervalSeconds = 10
	}
	if config.ReportIntervalSeconds > 300 {
		config.ReportIntervalSeconds = 300
	}

	return config, nil
}

// parseInt parses a string to int
func parseInt(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// parseInt64 parses a string to int64
func parseInt64(s string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// ToJSON converts the config to JSON for RPC responses
func (c *CacheConfig) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}
