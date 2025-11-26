package main

import (
	"sync/atomic"
)

// CacheMetrics tracks cache performance metrics
type CacheMetrics struct {
	hitCount         int64
	missCount        int64
	bypassCount      int64
	totalTokensSaved int64
}

// NewCacheMetrics creates a new metrics tracker
func NewCacheMetrics() *CacheMetrics {
	return &CacheMetrics{}
}

// IncrementHit increments the hit counter
func (m *CacheMetrics) IncrementHit() {
	atomic.AddInt64(&m.hitCount, 1)
}

// IncrementMiss increments the miss counter
func (m *CacheMetrics) IncrementMiss() {
	atomic.AddInt64(&m.missCount, 1)
}

// IncrementBypass increments the bypass counter
func (m *CacheMetrics) IncrementBypass() {
	atomic.AddInt64(&m.bypassCount, 1)
}

// AddTokensSaved adds to the total tokens saved counter
func (m *CacheMetrics) AddTokensSaved(tokens int64) {
	atomic.AddInt64(&m.totalTokensSaved, tokens)
}

// GetHitCount returns the current hit count
func (m *CacheMetrics) GetHitCount() int64 {
	return atomic.LoadInt64(&m.hitCount)
}

// GetMissCount returns the current miss count
func (m *CacheMetrics) GetMissCount() int64 {
	return atomic.LoadInt64(&m.missCount)
}

// GetBypassCount returns the current bypass count
func (m *CacheMetrics) GetBypassCount() int64 {
	return atomic.LoadInt64(&m.bypassCount)
}

// GetTotalTokensSaved returns the total tokens saved
func (m *CacheMetrics) GetTotalTokensSaved() int64 {
	return atomic.LoadInt64(&m.totalTokensSaved)
}

// GetHitRate calculates the cache hit rate
func (m *CacheMetrics) GetHitRate() float64 {
	hits := atomic.LoadInt64(&m.hitCount)
	misses := atomic.LoadInt64(&m.missCount)

	total := hits + misses
	if total == 0 {
		return 0.0
	}

	return float64(hits) / float64(total)
}

// Reset resets all metrics to zero
func (m *CacheMetrics) Reset() {
	atomic.StoreInt64(&m.hitCount, 0)
	atomic.StoreInt64(&m.missCount, 0)
	atomic.StoreInt64(&m.bypassCount, 0)
	atomic.StoreInt64(&m.totalTokensSaved, 0)
}

// Snapshot returns a copy of all current metrics
func (m *CacheMetrics) Snapshot() map[string]interface{} {
	hits := atomic.LoadInt64(&m.hitCount)
	misses := atomic.LoadInt64(&m.missCount)
	bypasses := atomic.LoadInt64(&m.bypassCount)
	tokens := atomic.LoadInt64(&m.totalTokensSaved)

	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return map[string]interface{}{
		"hit_count":          hits,
		"miss_count":         misses,
		"bypass_count":       bypasses,
		"total_tokens_saved": tokens,
		"hit_rate":           hitRate,
	}
}
