package main

import (
	"errors"
	"sync"
	"time"
)

// asyncLookup caches values the request path needs from the host (an LLM's
// endpoint, an App's metadata) and never makes a request wait on the host
// longer than the caller allows (getWait). A miss starts one background
// fetch for that key; a stale value is served while it is refreshed. The
// host's service broker can take tens of seconds to fail (its dial retries
// ignore the caller's deadline), which must cost a request next to nothing.
type asyncLookup[K comparable, V any] struct {
	ttl, errTTL time.Duration
	max         int
	now         func() time.Time
	fetch       func(K) (V, error)

	mu      sync.Mutex
	entries map[K]*lookupEntry[V]
	// inflight counts background fetches, for tests.
	inflight sync.WaitGroup
}

type lookupEntry[V any] struct {
	val      V
	ok       bool
	expires  time.Time // when a good value goes stale
	retryAt  time.Time // when a failed key may be fetched again
	fetching bool
	done     chan struct{} // closed when the current fetch ends
}

func newAsyncLookup[K comparable, V any](ttl, errTTL time.Duration, max int, fetch func(K) (V, error)) *asyncLookup[K, V] {
	return &asyncLookup[K, V]{ttl: ttl, errTTL: errTTL, max: max, now: time.Now, fetch: fetch, entries: map[K]*lookupEntry[V]{}}
}

// get returns the cached value for k, fresh or stale, starting a background
// fetch when it is missing or stale. ok is false when no value is known yet.
func (c *asyncLookup[K, V]) get(k K) (V, bool) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[k]
	if e == nil {
		if len(c.entries) >= c.max {
			c.evictLocked()
		}
		e = &lookupEntry[V]{}
		c.entries[k] = e
	}
	fresh := e.ok && now.Before(e.expires)
	if !fresh && !e.fetching && !now.Before(e.retryAt) {
		e.fetching = true
		e.done = make(chan struct{})
		c.inflight.Add(1)
		go c.refresh(k, e)
	}
	return e.val, e.ok
}

// getWait is get, but when no value is known and a fetch is running it
// waits up to d for it. A healthy host answers in about a millisecond, so
// the first request already sees the value; a host that hangs costs a
// request at most d.
func (c *asyncLookup[K, V]) getWait(k K, d time.Duration) (V, bool) {
	v, ok := c.get(k)
	if ok || d <= 0 {
		return v, ok
	}
	c.mu.Lock()
	e := c.entries[k]
	var done chan struct{}
	if e != nil && e.fetching {
		done = e.done
	}
	c.mu.Unlock()
	if done == nil {
		return v, ok
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		return v, ok
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return e.val, e.ok
}

func (c *asyncLookup[K, V]) refresh(k K, e *lookupEntry[V]) {
	defer c.inflight.Done()
	var (
		v   V
		err error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = errors.New("lookup panicked")
			}
		}()
		v, err = c.fetch(k)
	}()
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	e.fetching = false
	close(e.done)
	if err != nil {
		e.retryAt = now.Add(c.errTTL)
		return
	}
	e.val, e.ok, e.expires = v, true, now.Add(c.ttl)
}

// evictLocked drops idle entries, and everything when that is not enough.
func (c *asyncLookup[K, V]) evictLocked() {
	now := c.now()
	for k, e := range c.entries {
		if !e.fetching && (!e.ok || !now.Before(e.expires)) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) >= c.max {
		for k, e := range c.entries {
			if !e.fetching {
				delete(c.entries, k)
			}
		}
	}
}
