package main

import (
	"sync/atomic"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

// ReasonOverBudget: compressing the request would take the body bytes in
// flight over max_in_flight_bytes, so it is forwarded unchanged.
const ReasonOverBudget = "over_budget"

// byteBudget counts the body bytes being parsed or compressed, all requests
// together. Compressing a body allocates several times its size, and
// concurrency multiplies that, so the request path takes a share before it
// parses a body and skips the body when there is none. It never waits.
type byteBudget struct {
	inFlight atomic.Int64
}

// acquire takes n bytes if the total stays within limit (0 means
// config.DefaultMaxInFlightBytes), and reports whether it did. A taken share
// is given back with release.
func (b *byteBudget) acquire(n, limit int) bool {
	if limit <= 0 {
		limit = config.DefaultMaxInFlightBytes
	}
	for {
		cur := b.inFlight.Load()
		if cur+int64(n) > int64(limit) {
			return false
		}
		if b.inFlight.CompareAndSwap(cur, cur+int64(n)) {
			return true
		}
	}
}

func (b *byteBudget) release(n int) {
	b.inFlight.Add(-int64(n))
}
