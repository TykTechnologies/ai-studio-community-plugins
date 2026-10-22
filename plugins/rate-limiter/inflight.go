package main

import (
	"context"
	"log"
	"time"
)

// A concurrent slot is taken in post_auth and given back by the response
// hooks. Those never run for a request answered before the upstream: a block
// by a later post_auth plugin (a cache hit), a request filter or guardrail
// block, or a stream the gateway ends without completing it. The counters'
// TTL is refreshed by every request that touches them, so such a slot would
// never expire, and the limit would creep down until every request gets a
// 429. The plugin therefore remembers the slots each request took and gives
// back those of requests older than inflightMaxAge.

const (
	// inflightMaxAge is how long a request may hold its concurrent slots
	// without its response hooks running. It is longer than the request
	// state's TTL (requestStateTTL), so a request whose slots were swept has
	// no state left for a late response to release them again from.
	inflightMaxAge = 10 * time.Minute

	// sweepInterval is how often HandlePostAuth looks for such requests.
	sweepInterval = time.Minute

	// requestStateTTL is how long the state linking post_auth to the
	// response phase is kept.
	requestStateTTL = 5 * time.Minute
)

// inflightSlots are the concurrent counter keys one request incremented.
type inflightSlots struct {
	keys    []string
	started time.Time
}

// trackSlots remembers the concurrent slots requestID took.
func (p *RateLimiterPlugin) trackSlots(requestID string, keys []string, started time.Time) {
	p.inflightMu.Lock()
	defer p.inflightMu.Unlock()
	if p.inflight == nil {
		p.inflight = make(map[string]inflightSlots)
	}
	p.inflight[requestID] = inflightSlots{keys: keys, started: started}
}

// takeSlots forgets requestID's slots and returns them for release. Only the
// caller that takes them releases them, so a slot is never given back twice.
func (p *RateLimiterPlugin) takeSlots(requestID string) ([]string, bool) {
	p.inflightMu.Lock()
	defer p.inflightMu.Unlock()
	slots, ok := p.inflight[requestID]
	if ok {
		delete(p.inflight, requestID)
	}
	return slots.keys, ok
}

// releaseSlots decrements the given concurrent counters.
func (p *RateLimiterPlugin) releaseSlots(ctx context.Context, keys []string) {
	for _, concKey := range keys {
		lock := p.getLock(concKey)
		lock.Lock()
		_, err := p.store.DecrementCounter(ctx, concKey, 5*time.Minute)
		if err != nil {
			log.Printf("%s: failed to decrement concurrent: %v", PluginName, err)
		}
		lock.Unlock()
	}
}

// sweepSlots gives back the slots of requests older than inflightMaxAge, at
// most once per sweepInterval.
func (p *RateLimiterPlugin) sweepSlots(ctx context.Context, now time.Time) {
	p.inflightMu.Lock()
	if now.Sub(p.lastSweep) < sweepInterval {
		p.inflightMu.Unlock()
		return
	}
	p.lastSweep = now
	stale := make(map[string][]string)
	for id, slots := range p.inflight {
		if now.Sub(slots.started) > inflightMaxAge {
			stale[id] = slots.keys
			delete(p.inflight, id)
		}
	}
	p.inflightMu.Unlock()

	if len(stale) > 0 {
		log.Printf("%s: releasing the concurrent slots of %d requests that never completed", PluginName, len(stale))
	}
	for id, keys := range stale {
		// Without its state, a late response cannot release the slots again
		_ = p.store.Delete(ctx, RequestStateKey(id))
		p.releaseSlots(ctx, keys)
	}
}
