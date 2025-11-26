package main

import (
	"encoding/json"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if !config.Enabled {
		t.Error("default Enabled should be true")
	}
	if config.TTLSeconds != 3600 {
		t.Errorf("default TTLSeconds should be 3600, got %d", config.TTLSeconds)
	}
	if config.MaxEntrySizeKB != 2048 {
		t.Errorf("default MaxEntrySizeKB should be 2048, got %d", config.MaxEntrySizeKB)
	}
	if config.MaxCacheSizeMB != 256 {
		t.Errorf("default MaxCacheSizeMB should be 256, got %d", config.MaxCacheSizeMB)
	}
	if len(config.Namespaces) != 1 || config.Namespaces[0] != "api_key" {
		t.Error("default Namespaces should be ['api_key']")
	}
	if !config.NormalizePrompts {
		t.Error("default NormalizePrompts should be true")
	}
	if config.ExposeCacheKeyHeader {
		t.Error("default ExposeCacheKeyHeader should be false")
	}
	if config.ReportIntervalSeconds != 60 {
		t.Errorf("default ReportIntervalSeconds should be 60, got %d", config.ReportIntervalSeconds)
	}
}

func TestParseConfigEmpty(t *testing.T) {
	config, err := ParseConfig(map[string]string{})

	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	// Should return defaults
	if config.TTLSeconds != 3600 {
		t.Error("empty config should use default TTLSeconds")
	}
}

func TestParseConfigEnabled(t *testing.T) {
	config, _ := ParseConfig(map[string]string{
		"enabled": "true",
	})
	if !config.Enabled {
		t.Error("enabled=true should set Enabled to true")
	}

	config, _ = ParseConfig(map[string]string{
		"enabled": "false",
	})
	if config.Enabled {
		t.Error("enabled=false should set Enabled to false")
	}
}

func TestParseConfigTTL(t *testing.T) {
	config, _ := ParseConfig(map[string]string{
		"ttl_seconds": "7200",
	})
	if config.TTLSeconds != 7200 {
		t.Errorf("expected TTLSeconds 7200, got %d", config.TTLSeconds)
	}
}

func TestParseConfigTTLMinimum(t *testing.T) {
	// TTL below 60 should be clamped to 60
	config, _ := ParseConfig(map[string]string{
		"ttl_seconds": "30",
	})
	if config.TTLSeconds != 60 {
		t.Errorf("TTL below 60 should be clamped to 60, got %d", config.TTLSeconds)
	}
}

func TestParseConfigTTLMaximum(t *testing.T) {
	// TTL above 86400 should be clamped to 86400
	config, _ := ParseConfig(map[string]string{
		"ttl_seconds": "100000",
	})
	if config.TTLSeconds != 86400 {
		t.Errorf("TTL above 86400 should be clamped to 86400, got %d", config.TTLSeconds)
	}
}

func TestParseConfigMaxEntrySize(t *testing.T) {
	config, _ := ParseConfig(map[string]string{
		"max_entry_size_kb": "4096",
	})
	if config.MaxEntrySizeKB != 4096 {
		t.Errorf("expected MaxEntrySizeKB 4096, got %d", config.MaxEntrySizeKB)
	}
}

func TestParseConfigMaxEntrySizeMinimum(t *testing.T) {
	// MaxEntrySizeKB below 1 should be clamped to 1
	config, _ := ParseConfig(map[string]string{
		"max_entry_size_kb": "0",
	})
	if config.MaxEntrySizeKB != 1 {
		t.Errorf("MaxEntrySizeKB below 1 should be clamped to 1, got %d", config.MaxEntrySizeKB)
	}
}

func TestParseConfigMaxCacheSize(t *testing.T) {
	config, _ := ParseConfig(map[string]string{
		"max_cache_size_mb": "512",
	})
	if config.MaxCacheSizeMB != 512 {
		t.Errorf("expected MaxCacheSizeMB 512, got %d", config.MaxCacheSizeMB)
	}
}

func TestParseConfigMaxCacheSizeMinimum(t *testing.T) {
	// MaxCacheSizeMB below 1 should be clamped to 1
	config, _ := ParseConfig(map[string]string{
		"max_cache_size_mb": "0",
	})
	if config.MaxCacheSizeMB != 1 {
		t.Errorf("MaxCacheSizeMB below 1 should be clamped to 1, got %d", config.MaxCacheSizeMB)
	}
}

func TestParseConfigNamespaces(t *testing.T) {
	namespaces := []string{"api_key", "user_id", "org_id"}
	nsJSON, _ := json.Marshal(namespaces)

	config, _ := ParseConfig(map[string]string{
		"namespaces": string(nsJSON),
	})

	if len(config.Namespaces) != 3 {
		t.Errorf("expected 3 namespaces, got %d", len(config.Namespaces))
	}
	if config.Namespaces[0] != "api_key" {
		t.Error("first namespace should be 'api_key'")
	}
	if config.Namespaces[2] != "org_id" {
		t.Error("third namespace should be 'org_id'")
	}
}

func TestParseConfigNamespacesEmpty(t *testing.T) {
	// Empty namespaces should default to ["api_key"]
	config, _ := ParseConfig(map[string]string{
		"namespaces": "[]",
	})

	if len(config.Namespaces) != 1 || config.Namespaces[0] != "api_key" {
		t.Error("empty namespaces should default to ['api_key']")
	}
}

func TestParseConfigNamespacesInvalidJSON(t *testing.T) {
	// Invalid JSON should keep defaults
	config, _ := ParseConfig(map[string]string{
		"namespaces": "{invalid}",
	})

	if len(config.Namespaces) != 1 || config.Namespaces[0] != "api_key" {
		t.Error("invalid namespaces JSON should keep defaults")
	}
}

func TestParseConfigNormalizePrompts(t *testing.T) {
	config, _ := ParseConfig(map[string]string{
		"normalize_prompts": "false",
	})
	if config.NormalizePrompts {
		t.Error("normalize_prompts=false should set NormalizePrompts to false")
	}

	config, _ = ParseConfig(map[string]string{
		"normalize_prompts": "true",
	})
	if !config.NormalizePrompts {
		t.Error("normalize_prompts=true should set NormalizePrompts to true")
	}
}

func TestParseConfigExposeCacheKeyHeader(t *testing.T) {
	config, _ := ParseConfig(map[string]string{
		"expose_cache_key_header": "true",
	})
	if !config.ExposeCacheKeyHeader {
		t.Error("expose_cache_key_header=true should set ExposeCacheKeyHeader to true")
	}

	config, _ = ParseConfig(map[string]string{
		"expose_cache_key_header": "false",
	})
	if config.ExposeCacheKeyHeader {
		t.Error("expose_cache_key_header=false should set ExposeCacheKeyHeader to false")
	}
}

func TestParseConfigReportIntervalSeconds(t *testing.T) {
	config, _ := ParseConfig(map[string]string{
		"report_interval_seconds": "120",
	})
	if config.ReportIntervalSeconds != 120 {
		t.Errorf("expected ReportIntervalSeconds 120, got %d", config.ReportIntervalSeconds)
	}
}

func TestParseConfigReportIntervalMinimum(t *testing.T) {
	// ReportIntervalSeconds below 10 should be clamped to 10
	config, _ := ParseConfig(map[string]string{
		"report_interval_seconds": "5",
	})
	if config.ReportIntervalSeconds != 10 {
		t.Errorf("ReportIntervalSeconds below 10 should be clamped to 10, got %d", config.ReportIntervalSeconds)
	}
}

func TestParseConfigReportIntervalMaximum(t *testing.T) {
	// ReportIntervalSeconds above 300 should be clamped to 300
	config, _ := ParseConfig(map[string]string{
		"report_interval_seconds": "500",
	})
	if config.ReportIntervalSeconds != 300 {
		t.Errorf("ReportIntervalSeconds above 300 should be clamped to 300, got %d", config.ReportIntervalSeconds)
	}
}

func TestParseConfigInvalidIntegers(t *testing.T) {
	// Invalid integer values should keep defaults
	config, _ := ParseConfig(map[string]string{
		"ttl_seconds":        "not-a-number",
		"max_entry_size_kb":  "invalid",
		"max_cache_size_mb":  "abc",
		"report_interval_seconds": "xyz",
	})

	// Should keep defaults
	if config.TTLSeconds != 3600 {
		t.Error("invalid ttl_seconds should keep default")
	}
	if config.MaxEntrySizeKB != 2048 {
		t.Error("invalid max_entry_size_kb should keep default")
	}
	if config.MaxCacheSizeMB != 256 {
		t.Error("invalid max_cache_size_mb should keep default")
	}
	if config.ReportIntervalSeconds != 60 {
		t.Error("invalid report_interval_seconds should keep default")
	}
}

func TestParseConfigFullConfig(t *testing.T) {
	namespaces := []string{"org_id", "user_id"}
	nsJSON, _ := json.Marshal(namespaces)

	config, err := ParseConfig(map[string]string{
		"enabled":                 "true",
		"ttl_seconds":             "1800",
		"max_entry_size_kb":       "1024",
		"max_cache_size_mb":       "128",
		"namespaces":              string(nsJSON),
		"normalize_prompts":       "false",
		"expose_cache_key_header": "true",
		"report_interval_seconds": "30",
	})

	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if !config.Enabled {
		t.Error("Enabled should be true")
	}
	if config.TTLSeconds != 1800 {
		t.Errorf("TTLSeconds should be 1800, got %d", config.TTLSeconds)
	}
	if config.MaxEntrySizeKB != 1024 {
		t.Errorf("MaxEntrySizeKB should be 1024, got %d", config.MaxEntrySizeKB)
	}
	if config.MaxCacheSizeMB != 128 {
		t.Errorf("MaxCacheSizeMB should be 128, got %d", config.MaxCacheSizeMB)
	}
	if len(config.Namespaces) != 2 {
		t.Errorf("Namespaces should have 2 entries, got %d", len(config.Namespaces))
	}
	if config.NormalizePrompts {
		t.Error("NormalizePrompts should be false")
	}
	if !config.ExposeCacheKeyHeader {
		t.Error("ExposeCacheKeyHeader should be true")
	}
	if config.ReportIntervalSeconds != 30 {
		t.Errorf("ReportIntervalSeconds should be 30, got %d", config.ReportIntervalSeconds)
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"123", 123, false},
		{"0", 0, false},
		{"-50", -50, false},
		{"abc", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		result, err := parseInt(tt.input)
		if tt.hasError && err == nil {
			t.Errorf("parseInt(%q) should have error", tt.input)
		}
		if !tt.hasError && err != nil {
			t.Errorf("parseInt(%q) unexpected error: %v", tt.input, err)
		}
		if !tt.hasError && result != tt.expected {
			t.Errorf("parseInt(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"123456789", 123456789, false},
		{"0", 0, false},
		{"-9999999999", -9999999999, false},
		{"abc", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		result, err := parseInt64(tt.input)
		if tt.hasError && err == nil {
			t.Errorf("parseInt64(%q) should have error", tt.input)
		}
		if !tt.hasError && err != nil {
			t.Errorf("parseInt64(%q) unexpected error: %v", tt.input, err)
		}
		if !tt.hasError && result != tt.expected {
			t.Errorf("parseInt64(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestConfigToJSON(t *testing.T) {
	config := DefaultConfig()

	jsonBytes, err := config.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Parse back and verify
	var parsed CacheConfig
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if parsed.TTLSeconds != config.TTLSeconds {
		t.Error("TTLSeconds not preserved in JSON roundtrip")
	}
	if parsed.MaxEntrySizeKB != config.MaxEntrySizeKB {
		t.Error("MaxEntrySizeKB not preserved in JSON roundtrip")
	}
	if len(parsed.Namespaces) != len(config.Namespaces) {
		t.Error("Namespaces not preserved in JSON roundtrip")
	}
}
