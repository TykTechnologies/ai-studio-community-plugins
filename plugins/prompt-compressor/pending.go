package main

import (
	"container/list"
	"sync"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

// Request outcomes the post-auth hook records besides compress.Outcome.
const (
	// OutcomeBypassed: the client sent X-Tyk-Compression: off and the config
	// allows it.
	OutcomeBypassed = "bypassed"
	// OutcomeFailedOpen: the hook failed (a panic) and the request went on
	// unchanged.
	OutcomeFailedOpen = "failed_open"
	// OutcomeFailedClosed: the external engine failed under failure_mode
	// closed and the request was refused with a 502.
	OutcomeFailedClosed = "failed_closed"
)

// pendingRecord is what the post-auth hook learned about a request, for the
// response hook to complete with usage. The response phase has no context
// the post-auth hook can write to, so the two meet by request ID.
type pendingRecord struct {
	RequestID  string
	Created    time.Time
	LLMID      uint32
	AppID      uint32
	Vendor     string
	Mode       config.Mode
	PolicyName string
	PolicyHash string
	Level      int
	// Outcome is the compress outcome, OutcomeBypassed or OutcomeFailedOpen.
	Outcome string
	Report  compress.Report
	// Forwarded: the compressed body went upstream (enforce mode).
	Forwarded bool
	// CompressMicros is the time the Compress call took.
	CompressMicros int64
	// Tools is what the request offered (tools.go).
	Tools toolsInfo
}

// Pending records live this long, or until the response hook takes them.
const (
	pendingTTL     = 300 * time.Second
	pendingMaxSize = 8192
)

// pendingCache holds pending records by request ID. It is bounded by count
// as well as age: entries are kept in arrival order, and each put drops
// expired ones from the front and the oldest past the cap, so no sweeper
// goroutine is needed and a burst cannot grow it.
type pendingCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	now     func() time.Time
	order   *list.List
	entries map[string]*list.Element
}

func newPendingCache(ttl time.Duration, max int) *pendingCache {
	return &pendingCache{ttl: ttl, max: max, now: time.Now, order: list.New(), entries: map[string]*list.Element{}}
}

// put stores rec under its request ID, replacing an earlier record.
func (c *pendingCache) put(rec pendingRecord) {
	if rec.RequestID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	rec.Created = now
	if el, ok := c.entries[rec.RequestID]; ok {
		c.order.Remove(el)
		delete(c.entries, rec.RequestID)
	}
	c.entries[rec.RequestID] = c.order.PushBack(&rec)
	for c.order.Len() > 0 {
		front := c.order.Front()
		r := front.Value.(*pendingRecord)
		if c.order.Len() <= c.max && now.Sub(r.Created) < c.ttl {
			break
		}
		c.order.Remove(front)
		delete(c.entries, r.RequestID)
	}
}

// peek returns the record for a request and leaves it.
func (c *pendingCache) peek(requestID string) (pendingRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[requestID]
	if !ok {
		return pendingRecord{}, false
	}
	rec := *el.Value.(*pendingRecord)
	if c.now().Sub(rec.Created) >= c.ttl {
		return pendingRecord{}, false
	}
	return rec, true
}

// take returns and removes the record for a request.
func (c *pendingCache) take(requestID string) (pendingRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[requestID]
	if !ok {
		return pendingRecord{}, false
	}
	c.order.Remove(el)
	delete(c.entries, requestID)
	rec := *el.Value.(*pendingRecord)
	if c.now().Sub(rec.Created) >= c.ttl {
		return pendingRecord{}, false
	}
	return rec, true
}

func (c *pendingCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
