package main

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if !c.FailOpen {
		t.Error("expected fail_open=true by default")
	}
	if c.StorageBackend != "kv" {
		t.Errorf("expected storage_backend=kv, got %s", c.StorageBackend)
	}
	if c.WindowSizeSeconds != 60 {
		t.Errorf("expected window_size_seconds=60, got %d", c.WindowSizeSeconds)
	}
}

func TestParseConfig_NestedJSON(t *testing.T) {
	m := map[string]string{
		"config": `{"fail_open":false,"storage_backend":"redis","redis_url":"redis://localhost:6379","window_size_seconds":120}`,
	}
	c := ParseConfig(m)
	if c.FailOpen {
		t.Error("expected fail_open=false")
	}
	if c.StorageBackend != "redis" {
		t.Errorf("expected redis, got %s", c.StorageBackend)
	}
	if c.RedisURL != "redis://localhost:6379" {
		t.Errorf("expected redis url, got %s", c.RedisURL)
	}
	if c.WindowSizeSeconds != 120 {
		t.Errorf("expected 120, got %d", c.WindowSizeSeconds)
	}
}

func TestParseConfig_PluginConfig(t *testing.T) {
	m := map[string]string{
		"plugin_config": `{"fail_open":false,"window_size_seconds":30}`,
	}
	c := ParseConfig(m)
	if c.FailOpen {
		t.Error("expected fail_open=false")
	}
	if c.WindowSizeSeconds != 30 {
		t.Errorf("expected 30, got %d", c.WindowSizeSeconds)
	}
}

func TestParseConfig_FlatKeys(t *testing.T) {
	m := map[string]string{
		"fail_open":          "false",
		"storage_backend":    "redis",
		"redis_url":          "redis://host:6379/1",
		"window_size_seconds": "90",
	}
	c := ParseConfig(m)
	if c.FailOpen {
		t.Error("expected fail_open=false")
	}
	if c.StorageBackend != "redis" {
		t.Errorf("expected redis, got %s", c.StorageBackend)
	}
	if c.RedisURL != "redis://host:6379/1" {
		t.Errorf("expected redis url, got %s", c.RedisURL)
	}
	if c.WindowSizeSeconds != 90 {
		t.Errorf("expected 90, got %d", c.WindowSizeSeconds)
	}
}

func TestParseConfig_EmptyMap(t *testing.T) {
	c := ParseConfig(map[string]string{})
	d := DefaultConfig()
	if c.FailOpen != d.FailOpen || c.StorageBackend != d.StorageBackend || c.WindowSizeSeconds != d.WindowSizeSeconds {
		t.Error("empty map should produce defaults")
	}
}

func TestValidateConfig_Clamps(t *testing.T) {
	c := &Config{WindowSizeSeconds: 5, StorageBackend: "invalid"}
	c = validateConfig(c)
	if c.WindowSizeSeconds != 10 {
		t.Errorf("expected clamped to 10, got %d", c.WindowSizeSeconds)
	}
	if c.StorageBackend != "kv" {
		t.Errorf("expected fallback to kv, got %s", c.StorageBackend)
	}

	c = &Config{WindowSizeSeconds: 9999}
	c = validateConfig(c)
	if c.WindowSizeSeconds != 3600 {
		t.Errorf("expected clamped to 3600, got %d", c.WindowSizeSeconds)
	}
}
