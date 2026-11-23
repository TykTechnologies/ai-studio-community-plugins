package main

import (
	"net/http"
	"time"
)

// The rate limit headers of an allowed request belong on the response the
// client gets. post_auth can only change the request that goes to the vendor,
// so returning them from there sent X-RateLimit-* upstream (the vendor
// learned the customer's limits) and the client saw them only on a 429. They
// are now kept here, by request ID, from post_auth until the response's
// header hook adds them to the response. The response phase runs in the same
// plugin process as post_auth, whatever the counter backend is, so this is
// in memory.

const (
	// pendingHeadersMaxAge is how long a request's headers wait for its
	// response. A request that never gets one (the client went away, a later
	// plugin or a filter answered it) has its entry swept after this.
	pendingHeadersMaxAge = inflightMaxAge

	// pendingHeadersMax bounds the entries kept. Over it, new requests get
	// no rate limit headers (they are still counted and limited) until the
	// sweep or their responses have made room.
	pendingHeadersMax = 100_000
)

// pendingHeaders are the response headers one allowed request is owed.
type pendingHeaders struct {
	headers map[string]string
	stored  time.Time
}

// holdHeaders keeps the headers for requestID's response.
func (p *RateLimiterPlugin) holdHeaders(requestID string, headers map[string]string, now time.Time) {
	if requestID == "" || len(headers) == 0 {
		return
	}
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	if p.pending == nil {
		p.pending = make(map[string]pendingHeaders)
	}
	if len(p.pending) >= pendingHeadersMax {
		// A full table is swept at most once per sweepInterval: a sweep
		// walks every entry under the lock post_auth takes, and a client
		// that keeps the table full must not make every request pay for one.
		if now.Sub(p.pendingFullSweep) < sweepInterval {
			return
		}
		p.pendingFullSweep = now
		p.sweepHeadersLocked(now)
		if len(p.pending) >= pendingHeadersMax {
			return
		}
	}
	p.pending[requestID] = pendingHeaders{headers: headers, stored: now}
}

// heldHeaders returns the headers requestID's response is owed, if any.
// They stay held until dropHeaders: a response's header hook and its body
// hook may both ask.
func (p *RateLimiterPlugin) heldHeaders(requestID string) map[string]string {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	return p.pending[requestID].headers
}

// dropHeaders forgets requestID's headers once its response is done.
func (p *RateLimiterPlugin) dropHeaders(requestID string) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	delete(p.pending, requestID)
}

// sweepHeaders forgets the headers of requests whose response never came.
// It runs with the slot sweep, at most once per sweepInterval.
func (p *RateLimiterPlugin) sweepHeaders(now time.Time) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	p.sweepHeadersLocked(now)
}

func (p *RateLimiterPlugin) sweepHeadersLocked(now time.Time) {
	for id, held := range p.pending {
		if now.Sub(held.stored) > pendingHeadersMaxAge {
			delete(p.pending, id)
		}
	}
}

// withHeldHeaders returns the response headers with the request's rate limit
// headers set, and whether there were any. A header of the same name the
// vendor sent, in any case, is replaced: the limit the client is held to
// here is the gateway's. The vendor's own differently named headers (OpenAI's
// X-Ratelimit-Limit-Requests and the like) are left as they are.
func withHeldHeaders(response, held map[string]string) (map[string]string, bool) {
	if len(held) == 0 {
		return response, false
	}
	ours := make(map[string]bool, len(held))
	for name := range held {
		ours[http.CanonicalHeaderKey(name)] = true
	}
	out := make(map[string]string, len(response)+len(held))
	for name, value := range response {
		if !ours[http.CanonicalHeaderKey(name)] {
			out[name] = value
		}
	}
	for name, value := range held {
		out[name] = value
	}
	return out, true
}
