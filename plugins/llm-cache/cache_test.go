package main

import (
	"sync"
	"testing"
	"time"
)

func TestNewMemoryCache(t *testing.T) {
	cache := NewMemoryCache(100, 1024, 3600)

	if cache == nil {
		t.Fatal("NewMemoryCache returned nil")
	}

	// Verify size conversions
	if cache.maxSize != 100*1024*1024 {
		t.Errorf("expected maxSize %d, got %d", 100*1024*1024, cache.maxSize)
	}

	if cache.maxEntrySize != 1024*1024 {
		t.Errorf("expected maxEntrySize %d, got %d", 1024*1024, cache.maxEntrySize)
	}

	if cache.defaultTTL != 3600*time.Second {
		t.Errorf("expected defaultTTL %v, got %v", 3600*time.Second, cache.defaultTTL)
	}
}

func TestCacheSetAndGet(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	response := []byte(`{"response": "Hello, World!"}`)
	headers := map[string]string{"Content-Type": "application/json"}

	// Test Set
	ok := cache.Set("key1", response, headers, "gpt-4", 100, nil)
	if !ok {
		t.Fatal("Set returned false")
	}

	// Test Get
	entry := cache.Get("key1")
	if entry == nil {
		t.Fatal("Get returned nil")
	}

	if string(entry.Response) != string(response) {
		t.Errorf("expected response %s, got %s", response, entry.Response)
	}

	if entry.Model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", entry.Model)
	}

	if entry.TokensSaved != 100 {
		t.Errorf("expected tokensSaved 100, got %d", entry.TokensSaved)
	}
}

func TestCacheGetNonExistent(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	entry := cache.Get("non-existent")
	if entry != nil {
		t.Error("Get should return nil for non-existent key")
	}
}

func TestCacheExpiration(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	response := []byte(`{"test": "data"}`)
	// Use 1 second TTL
	// ExpiresAt = now.Add(1s).Unix()
	// Entry expires when time.Now().Unix() > ExpiresAt
	// So we need to wait at least 2 full seconds to guarantee current_time > ExpiresAt
	ttl := 1 * time.Second

	cache.Set("expire-key", response, nil, "gpt-4", 0, &ttl)

	// Should exist initially
	entry := cache.Get("expire-key")
	if entry == nil {
		t.Fatal("Entry should exist before expiration")
	}

	// Wait well past expiration (need current_time > ExpiresAt)
	time.Sleep(2100 * time.Millisecond)

	// Should be expired now
	entry = cache.Get("expire-key")
	if entry != nil {
		t.Error("Entry should be nil after expiration")
	}
}

func TestCacheDelete(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	cache.Set("delete-key", []byte("data"), nil, "gpt-4", 0, nil)

	// Verify it exists
	if cache.Get("delete-key") == nil {
		t.Fatal("Entry should exist before delete")
	}

	// Delete it
	deleted := cache.Delete("delete-key")
	if !deleted {
		t.Error("Delete should return true for existing key")
	}

	// Verify it's gone
	if cache.Get("delete-key") != nil {
		t.Error("Entry should be nil after delete")
	}

	// Delete non-existent should return false
	deleted = cache.Delete("non-existent")
	if deleted {
		t.Error("Delete should return false for non-existent key")
	}
}

func TestCacheClear(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	// Add multiple entries
	cache.Set("key1", []byte("data1"), nil, "gpt-4", 0, nil)
	cache.Set("key2", []byte("data2"), nil, "gpt-4", 0, nil)
	cache.Set("key3", []byte("data3"), nil, "gpt-4", 0, nil)

	entries, _, _ := cache.Stats()
	if entries != 3 {
		t.Errorf("expected 3 entries, got %d", entries)
	}

	// Clear the cache
	cache.Clear()

	entries, size, _ := cache.Stats()
	if entries != 0 {
		t.Errorf("expected 0 entries after clear, got %d", entries)
	}
	if size != 0 {
		t.Errorf("expected 0 size after clear, got %d", size)
	}
}

func TestCacheStats(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	data := []byte("test data")
	cache.Set("key1", data, nil, "gpt-4", 0, nil)

	entries, sizeBytes, evictions := cache.Stats()

	if entries != 1 {
		t.Errorf("expected 1 entry, got %d", entries)
	}

	if sizeBytes == 0 {
		t.Error("expected non-zero size")
	}

	if evictions != 0 {
		t.Errorf("expected 0 evictions, got %d", evictions)
	}
}

func TestCacheLRUEviction(t *testing.T) {
	// Create a very small cache (1KB max, 512 byte max entry)
	cache := NewMemoryCache(0, 512, 3600) // 0 MB = 0 bytes, will force eviction
	cache.maxSize = 1000                  // Set directly for testing: ~1KB

	// Each entry will be roughly 100 bytes
	data := make([]byte, 100)

	// Add entries until eviction should occur
	for i := 0; i < 15; i++ {
		cache.Set("key"+string(rune('A'+i)), data, nil, "gpt-4", 0, nil)
	}

	// Should have had some evictions
	_, _, evictions := cache.Stats()
	if evictions == 0 {
		t.Error("expected evictions to occur")
	}
}

func TestCacheEntryTooLarge(t *testing.T) {
	// Create cache with 1KB max entry size
	cache := NewMemoryCache(10, 1, 3600) // 1 KB max entry

	// Try to add entry larger than max
	largeData := make([]byte, 2000) // 2KB
	ok := cache.Set("large-key", largeData, nil, "gpt-4", 0, nil)

	if ok {
		t.Error("Set should return false for entry larger than max entry size")
	}
}

func TestCacheHitCount(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	cache.Set("hit-key", []byte("data"), nil, "gpt-4", 0, nil)

	// Multiple gets should increment hit count
	// Each Get() increments the hit count
	for i := 0; i < 5; i++ {
		cache.Get("hit-key")
	}

	entry := cache.Get("hit-key") // This is the 6th get
	if entry.HitCount != 6 {
		t.Errorf("expected 6 hits (5 + this get), got %d", entry.HitCount)
	}
}

func TestCacheUpdate(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	cache.Set("update-key", []byte("original"), nil, "gpt-4", 0, nil)

	entry := cache.Get("update-key")
	if string(entry.Response) != "original" {
		t.Error("initial value should be 'original'")
	}

	// Update with new value
	cache.Set("update-key", []byte("updated"), nil, "gpt-4", 0, nil)

	entry = cache.Get("update-key")
	if string(entry.Response) != "updated" {
		t.Error("value should be 'updated' after set")
	}
}

func TestCacheCleanupExpired(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	// Use 1 second TTL
	// Entry expires when time.Now().Unix() > ExpiresAt
	ttl := 1 * time.Second

	cache.Set("expire1", []byte("data"), nil, "gpt-4", 0, &ttl)
	cache.Set("expire2", []byte("data"), nil, "gpt-4", 0, &ttl)
	cache.Set("keep", []byte("data"), nil, "gpt-4", 0, nil) // Uses default long TTL

	// Wait well past expiration (need current_time > ExpiresAt)
	time.Sleep(2100 * time.Millisecond)

	removed := cache.CleanupExpired()
	if removed != 2 {
		t.Errorf("expected 2 entries removed, got %d", removed)
	}

	// "keep" should still exist
	if cache.Get("keep") == nil {
		t.Error("'keep' entry should still exist")
	}
}

func TestCacheConcurrency(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	var wg sync.WaitGroup
	numGoroutines := 100
	numOperations := 100

	// Launch concurrent writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := "key" + string(rune(id%26+'a')) + string(rune(j%26+'a'))
				cache.Set(key, []byte("data"), nil, "gpt-4", 0, nil)
				cache.Get(key)
			}
		}(i)
	}

	wg.Wait()

	// If we got here without deadlock or panic, the test passes
	entries, _, _ := cache.Stats()
	if entries == 0 {
		t.Error("expected some entries after concurrent operations")
	}
}

func TestCacheGetMaxSize(t *testing.T) {
	cache := NewMemoryCache(100, 1024, 3600)

	expected := int64(100 * 1024 * 1024)
	if cache.GetMaxSize() != expected {
		t.Errorf("expected max size %d, got %d", expected, cache.GetMaxSize())
	}
}

func TestCacheGetCurrentSize(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	if cache.GetCurrentSize() != 0 {
		t.Error("new cache should have 0 current size")
	}

	cache.Set("key1", []byte("data"), nil, "gpt-4", 0, nil)

	if cache.GetCurrentSize() == 0 {
		t.Error("cache should have non-zero size after adding entry")
	}
}

func TestCacheHeaderSizeEstimation(t *testing.T) {
	cache := NewMemoryCache(10, 1024, 3600)

	headers := map[string]string{
		"Content-Type":     "application/json",
		"X-Custom-Header":  "value",
		"X-Another-Header": "another-value",
	}

	size := cache.estimateHeaderSize(headers)
	if size == 0 {
		t.Error("header size estimation should be non-zero")
	}

	// Verify it accounts for key-value pairs
	emptySize := cache.estimateHeaderSize(nil)
	if emptySize != 0 {
		t.Error("empty headers should have 0 size")
	}
}
