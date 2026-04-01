package main

import (
	"encoding/json"
	"fmt"
	"log"
)

// Config holds global plugin settings. Rules are managed via UI/RPC but stored
// in the config so they propagate to edge gateways via config snapshots.
type Config struct {
	FailOpen          bool   `json:"fail_open"`
	StorageBackend    string `json:"storage_backend"`
	RedisURL          string `json:"redis_url"`
	WindowSizeSeconds int    `json:"window_size_seconds"`
	Rules             []Rule `json:"rules,omitempty"`
}

// DefaultConfig returns sane defaults.
func DefaultConfig() *Config {
	return &Config{
		FailOpen:          true,
		StorageBackend:    "kv",
		WindowSizeSeconds: 60,
	}
}

// ParseConfig extracts global settings from the plugin config map.
// Supports multiple config sources following the community plugin convention:
// 1. "config" key (nested JSON) — Studio passes this
// 2. "plugin_config" key — microgateway may use this
// 3. Individual flat keys as fallback
func ParseConfig(configMap map[string]string) *Config {
	config := DefaultConfig()

	// Try "config" key (nested JSON)
	if configJSON, ok := configMap["config"]; ok && configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), config); err != nil {
			log.Printf("rate-limiter: failed to parse 'config' key: %v", err)
		} else {
			return validateConfig(config)
		}
	}

	// Try "plugin_config" key (microgateway)
	if configJSON, ok := configMap["plugin_config"]; ok && configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), config); err != nil {
			log.Printf("rate-limiter: failed to parse 'plugin_config' key: %v", err)
		} else {
			return validateConfig(config)
		}
	}

	// Fall back to individual flat keys
	if v, ok := configMap["fail_open"]; ok {
		config.FailOpen = v == "true"
	}
	if v, ok := configMap["storage_backend"]; ok && v != "" {
		config.StorageBackend = v
	}
	if v, ok := configMap["redis_url"]; ok && v != "" {
		config.RedisURL = v
	}
	if v, ok := configMap["window_size_seconds"]; ok && v != "" {
		if val, err := parseInt(v); err == nil {
			config.WindowSizeSeconds = val
		}
	}
	if v, ok := configMap["rules"]; ok && v != "" {
		var rules []Rule
		if err := json.Unmarshal([]byte(v), &rules); err != nil {
			log.Printf("rate-limiter: failed to parse 'rules' from config: %v", err)
		} else {
			config.Rules = rules
		}
	}

	return validateConfig(config)
}

func validateConfig(config *Config) *Config {
	if config.WindowSizeSeconds < 10 {
		config.WindowSizeSeconds = 10
	}
	if config.WindowSizeSeconds > 3600 {
		config.WindowSizeSeconds = 3600
	}
	if config.StorageBackend != "kv" && config.StorageBackend != "redis" {
		config.StorageBackend = "kv"
	}
	return config
}

func parseInt(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
