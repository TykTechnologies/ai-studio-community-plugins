package main

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

// CacheEntry represents a single cached LLM response
type CacheEntry struct {
	Key         string            `json:"key"`
	Response    []byte            `json:"response"`
	Headers     map[string]string `json:"headers"`
	CreatedAt   int64             `json:"created_at"`
	ExpiresAt   int64             `json:"expires_at"`
	HitCount    int64             `json:"hit_count"`
	Model       string            `json:"model"`
	TokensSaved int               `json:"tokens_saved"`
	Size        int64             `json:"size"`
}

// MemoryCache is a thread-safe in-memory LRU cache with TTL support
type MemoryCache struct {
	mu           sync.RWMutex
	entries      map[string]*CacheEntry
	lruList      *list.List
	lruIndex     map[string]*list.Element
	currentSize  int64
	maxSize      int64
	maxEntrySize int64
	defaultTTL   time.Duration

	// Metrics (updated atomically)
	evictionCount int64
}

// lruItem wraps cache key for the LRU list
type lruItem struct {
	key string
}

// NewMemoryCache creates a new in-memory LRU cache
func NewMemoryCache(maxSizeMB, maxEntrySizeKB int64, defaultTTLSeconds int) *MemoryCache {
	return &MemoryCache{
		entries:      make(map[string]*CacheEntry),
		lruList:      list.New(),
		lruIndex:     make(map[string]*list.Element),
		currentSize:  0,
		maxSize:      maxSizeMB * 1024 * 1024,      // Convert MB to bytes
		maxEntrySize: maxEntrySizeKB * 1024,        // Convert KB to bytes
		defaultTTL:   time.Duration(defaultTTLSeconds) * time.Second,
	}
}

// Get retrieves an entry from the cache
// Returns nil if not found or expired
func (c *MemoryCache) Get(key string) *CacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Now().Unix() > entry.ExpiresAt {
		c.removeEntryLocked(key)
		return nil
	}

	// Update LRU order - move to front
	if elem, ok := c.lruIndex[key]; ok {
		c.lruList.MoveToFront(elem)
	}

	// Increment hit count atomically
	atomic.AddInt64(&entry.HitCount, 1)

	return entry
}

// Set stores an entry in the cache
// Returns true if the entry was stored, false if it was too large
func (c *MemoryCache) Set(key string, response []byte, headers map[string]string, model string, tokensSaved int, ttl *time.Duration) bool {
	entrySize := int64(len(response)) + c.estimateHeaderSize(headers)

	// Check if entry is too large
	if entrySize > c.maxEntrySize {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove existing entry if present
	if _, exists := c.entries[key]; exists {
		c.removeEntryLocked(key)
	}

	// Evict entries if necessary to make room
	for c.currentSize+entrySize > c.maxSize && c.lruList.Len() > 0 {
		c.evictLRULocked()
	}

	// Determine TTL
	duration := c.defaultTTL
	if ttl != nil {
		duration = *ttl
	}

	now := time.Now()
	entry := &CacheEntry{
		Key:         key,
		Response:    response,
		Headers:     headers,
		CreatedAt:   now.Unix(),
		ExpiresAt:   now.Add(duration).Unix(),
		HitCount:    0,
		Model:       model,
		TokensSaved: tokensSaved,
		Size:        entrySize,
	}

	// Add to cache
	c.entries[key] = entry
	elem := c.lruList.PushFront(&lruItem{key: key})
	c.lruIndex[key] = elem
	c.currentSize += entrySize

	return true
}

// Delete removes an entry from the cache
func (c *MemoryCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; !exists {
		return false
	}

	c.removeEntryLocked(key)
	return true
}

// Clear removes all entries from the cache
func (c *MemoryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	c.lruList = list.New()
	c.lruIndex = make(map[string]*list.Element)
	c.currentSize = 0
}

// Stats returns current cache statistics
func (c *MemoryCache) Stats() (entries int, sizeBytes int64, evictions int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries), c.currentSize, atomic.LoadInt64(&c.evictionCount)
}

// CleanupExpired removes all expired entries
// Should be called periodically
func (c *MemoryCache) CleanupExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().Unix()
	removed := 0

	for key, entry := range c.entries {
		if now > entry.ExpiresAt {
			c.removeEntryLocked(key)
			removed++
		}
	}

	return removed
}

// removeEntryLocked removes an entry without acquiring lock
// Caller must hold the lock
func (c *MemoryCache) removeEntryLocked(key string) {
	entry, exists := c.entries[key]
	if !exists {
		return
	}

	// Remove from LRU list
	if elem, ok := c.lruIndex[key]; ok {
		c.lruList.Remove(elem)
		delete(c.lruIndex, key)
	}

	// Update size and remove entry
	c.currentSize -= entry.Size
	delete(c.entries, key)
}

// evictLRULocked evicts the least recently used entry
// Caller must hold the lock
func (c *MemoryCache) evictLRULocked() {
	elem := c.lruList.Back()
	if elem == nil {
		return
	}

	item := elem.Value.(*lruItem)
	c.removeEntryLocked(item.key)
	atomic.AddInt64(&c.evictionCount, 1)
}

// estimateHeaderSize estimates the size of headers in bytes
func (c *MemoryCache) estimateHeaderSize(headers map[string]string) int64 {
	size := int64(0)
	for k, v := range headers {
		size += int64(len(k) + len(v) + 4) // 4 bytes for separators
	}
	return size
}

// GetMaxSize returns the maximum cache size in bytes
func (c *MemoryCache) GetMaxSize() int64 {
	return c.maxSize
}

// GetCurrentSize returns the current cache size in bytes
func (c *MemoryCache) GetCurrentSize() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSize
}
