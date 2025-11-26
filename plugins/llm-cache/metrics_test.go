package main

import (
	"sync"
	"testing"
)

func TestNewCacheMetrics(t *testing.T) {
	metrics := NewCacheMetrics()

	if metrics == nil {
		t.Fatal("NewCacheMetrics returned nil")
	}

	if metrics.GetHitCount() != 0 {
		t.Error("new metrics should have 0 hits")
	}
	if metrics.GetMissCount() != 0 {
		t.Error("new metrics should have 0 misses")
	}
	if metrics.GetBypassCount() != 0 {
		t.Error("new metrics should have 0 bypasses")
	}
	if metrics.GetTotalTokensSaved() != 0 {
		t.Error("new metrics should have 0 tokens saved")
	}
}

func TestMetricsIncrementHit(t *testing.T) {
	metrics := NewCacheMetrics()

	metrics.IncrementHit()
	metrics.IncrementHit()
	metrics.IncrementHit()

	if metrics.GetHitCount() != 3 {
		t.Errorf("expected 3 hits, got %d", metrics.GetHitCount())
	}
}

func TestMetricsIncrementMiss(t *testing.T) {
	metrics := NewCacheMetrics()

	metrics.IncrementMiss()
	metrics.IncrementMiss()

	if metrics.GetMissCount() != 2 {
		t.Errorf("expected 2 misses, got %d", metrics.GetMissCount())
	}
}

func TestMetricsIncrementBypass(t *testing.T) {
	metrics := NewCacheMetrics()

	metrics.IncrementBypass()

	if metrics.GetBypassCount() != 1 {
		t.Errorf("expected 1 bypass, got %d", metrics.GetBypassCount())
	}
}

func TestMetricsAddTokensSaved(t *testing.T) {
	metrics := NewCacheMetrics()

	metrics.AddTokensSaved(100)
	metrics.AddTokensSaved(50)
	metrics.AddTokensSaved(25)

	if metrics.GetTotalTokensSaved() != 175 {
		t.Errorf("expected 175 tokens saved, got %d", metrics.GetTotalTokensSaved())
	}
}

func TestMetricsGetHitRate(t *testing.T) {
	metrics := NewCacheMetrics()

	// No requests yet
	if metrics.GetHitRate() != 0.0 {
		t.Error("hit rate should be 0 with no requests")
	}

	// 3 hits, 1 miss = 75% hit rate
	metrics.IncrementHit()
	metrics.IncrementHit()
	metrics.IncrementHit()
	metrics.IncrementMiss()

	hitRate := metrics.GetHitRate()
	if hitRate != 0.75 {
		t.Errorf("expected 0.75 hit rate, got %f", hitRate)
	}

	// All hits
	metrics.Reset()
	metrics.IncrementHit()
	if metrics.GetHitRate() != 1.0 {
		t.Error("100% hits should have 1.0 hit rate")
	}

	// All misses
	metrics.Reset()
	metrics.IncrementMiss()
	if metrics.GetHitRate() != 0.0 {
		t.Error("0% hits should have 0.0 hit rate")
	}
}

func TestMetricsReset(t *testing.T) {
	metrics := NewCacheMetrics()

	metrics.IncrementHit()
	metrics.IncrementHit()
	metrics.IncrementMiss()
	metrics.IncrementBypass()
	metrics.AddTokensSaved(1000)

	// Verify they're set
	if metrics.GetHitCount() != 2 {
		t.Error("hits should be 2 before reset")
	}

	// Reset
	metrics.Reset()

	if metrics.GetHitCount() != 0 {
		t.Error("hits should be 0 after reset")
	}
	if metrics.GetMissCount() != 0 {
		t.Error("misses should be 0 after reset")
	}
	if metrics.GetBypassCount() != 0 {
		t.Error("bypasses should be 0 after reset")
	}
	if metrics.GetTotalTokensSaved() != 0 {
		t.Error("tokens saved should be 0 after reset")
	}
}

func TestMetricsSnapshot(t *testing.T) {
	metrics := NewCacheMetrics()

	metrics.IncrementHit()
	metrics.IncrementHit()
	metrics.IncrementMiss()
	metrics.IncrementBypass()
	metrics.AddTokensSaved(500)

	snapshot := metrics.Snapshot()

	if snapshot["hit_count"].(int64) != 2 {
		t.Error("snapshot hit_count should be 2")
	}
	if snapshot["miss_count"].(int64) != 1 {
		t.Error("snapshot miss_count should be 1")
	}
	if snapshot["bypass_count"].(int64) != 1 {
		t.Error("snapshot bypass_count should be 1")
	}
	if snapshot["total_tokens_saved"].(int64) != 500 {
		t.Error("snapshot total_tokens_saved should be 500")
	}

	// Hit rate: 2/(2+1) = 0.666...
	hitRate := snapshot["hit_rate"].(float64)
	if hitRate < 0.66 || hitRate > 0.67 {
		t.Errorf("snapshot hit_rate should be ~0.666, got %f", hitRate)
	}
}

func TestMetricsSnapshotZeroRequests(t *testing.T) {
	metrics := NewCacheMetrics()

	snapshot := metrics.Snapshot()

	if snapshot["hit_rate"].(float64) != 0.0 {
		t.Error("hit_rate should be 0 with no requests")
	}
}

func TestMetricsConcurrency(t *testing.T) {
	metrics := NewCacheMetrics()

	var wg sync.WaitGroup
	numGoroutines := 100
	numOperations := 100

	// Launch concurrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				metrics.IncrementHit()
				metrics.IncrementMiss()
				metrics.IncrementBypass()
				metrics.AddTokensSaved(10)
			}
		}()
	}

	wg.Wait()

	expectedCount := int64(numGoroutines * numOperations)
	expectedTokens := int64(numGoroutines * numOperations * 10)

	if metrics.GetHitCount() != expectedCount {
		t.Errorf("expected %d hits, got %d", expectedCount, metrics.GetHitCount())
	}
	if metrics.GetMissCount() != expectedCount {
		t.Errorf("expected %d misses, got %d", expectedCount, metrics.GetMissCount())
	}
	if metrics.GetBypassCount() != expectedCount {
		t.Errorf("expected %d bypasses, got %d", expectedCount, metrics.GetBypassCount())
	}
	if metrics.GetTotalTokensSaved() != expectedTokens {
		t.Errorf("expected %d tokens, got %d", expectedTokens, metrics.GetTotalTokensSaved())
	}
}

func TestEdgeCacheStatsStruct(t *testing.T) {
	stats := EdgeCacheStats{
		HitCount:         100,
		MissCount:        25,
		BypassCount:      5,
		EvictionCount:    10,
		ActiveEntries:    50,
		CacheSizeBytes:   1024000,
		MaxSizeBytes:     10240000,
		HitRate:          0.8,
		TotalTokensSaved: 5000,
		Timestamp:        1234567890,
	}

	if stats.HitCount != 100 {
		t.Error("HitCount should be 100")
	}
	if stats.HitRate != 0.8 {
		t.Error("HitRate should be 0.8")
	}
}

func TestEdgeStatsRecordStruct(t *testing.T) {
	record := EdgeStatsRecord{
		EdgeID:    "edge-001",
		Namespace: "test-namespace",
		Stats: EdgeCacheStats{
			HitCount:  50,
			MissCount: 10,
		},
	}

	if record.EdgeID != "edge-001" {
		t.Error("EdgeID should be 'edge-001'")
	}
	if record.Namespace != "test-namespace" {
		t.Error("Namespace should be 'test-namespace'")
	}
	if record.Stats.HitCount != 50 {
		t.Error("Stats.HitCount should be 50")
	}
}
