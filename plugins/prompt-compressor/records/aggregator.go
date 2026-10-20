package records

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
)

// PriceFunc returns a model's prices per token; ok is false when the edge
// has no input price for it.
type PriceFunc func(vendor, model string) (p Price, ok bool)

// Aggregator is the edge's Recorder: it merges requests into deltas by key
// until Flush. It holds at most MaxKeys keys; past that, Add drops the
// request and counts it in Dropped rather than growing without bound
// (Full tells the caller to flush early).
type Aggregator struct {
	price   PriceFunc
	maxKeys int

	mu      sync.Mutex
	deltas  map[Key]*Delta
	dropped int64
}

// DefaultMaxKeys bounds the keys an aggregator holds between flushes.
const DefaultMaxKeys = 20000

// NewAggregator returns an aggregator pricing requests with price (nil
// leaves every saving unpriced).
func NewAggregator(price PriceFunc) *Aggregator {
	return &Aggregator{price: price, maxKeys: DefaultMaxKeys, deltas: map[Key]*Delta{}}
}

// Add implements Recorder. Requests with an invalid key (an unknown
// outcome, a malformed hash) are dropped.
func (a *Aggregator) Add(r Request) {
	k := KeyOf(r)
	if k.Valid() != nil {
		a.mu.Lock()
		a.dropped++
		a.mu.Unlock()
		return
	}
	var (
		price  Price
		priced bool
	)
	if a.price != nil && r.TokensSaved() > 0 && r.Model != "" {
		price, priced = a.price(r.Vendor, r.Model)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.deltas[k]
	if d == nil {
		if len(a.deltas) >= a.maxKeys {
			a.dropped++
			return
		}
		d = NewDelta(k)
		a.deltas[k] = d
	}
	d.AddRequest(r, price, priced)
}

// Full reports whether the aggregator holds its maximum number of keys.
func (a *Aggregator) Full() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.deltas) >= a.maxKeys
}

// Dropped returns and resets the count of requests dropped since the last
// call.
func (a *Aggregator) Dropped() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.dropped
	a.dropped = 0
	return n
}

// Flush takes everything aggregated so far and returns it as batches, each
// under MaxBatchDeltas and MaxBatchBytes, with fresh batch IDs. The caller
// sends them; a batch that cannot be sent should be given back with
// Restore, so nothing is lost.
func (a *Aggregator) Flush() []*Batch {
	a.mu.Lock()
	deltas := a.deltas
	a.deltas = map[Key]*Delta{}
	a.mu.Unlock()
	if len(deltas) == 0 {
		return nil
	}

	list := make([]*Delta, 0, len(deltas))
	for _, d := range deltas {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Row() < list[j].Row() })

	var batches []*Batch
	cur := &Batch{Version: BatchVersion, ID: newBatchID()}
	size := 64
	for _, d := range list {
		b, err := json.Marshal(d)
		if err != nil {
			continue
		}
		if len(cur.Deltas) > 0 && (len(cur.Deltas) >= MaxBatchDeltas || size+len(b)+1 > MaxBatchBytes) {
			batches = append(batches, cur)
			cur = &Batch{Version: BatchVersion, ID: newBatchID()}
			size = 64
		}
		cur.Deltas = append(cur.Deltas, d)
		size += len(b) + 1
	}
	return append(batches, cur)
}

// Restore merges an unsent batch back, to go out with the next flush (with
// a new batch ID: Studio never applied the old one).
//
// The key bound holds here too: a restored key the aggregator has no room
// for is dropped and its requests counted in Dropped, so repeated failed
// sends cannot grow it past MaxKeys.
func (a *Aggregator) Restore(b *Batch) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range b.Deltas {
		if cur := a.deltas[d.Key]; cur != nil {
			cur.Merge(d)
			continue
		}
		if len(a.deltas) >= a.maxKeys {
			a.dropped += d.Requests
			continue
		}
		a.deltas[d.Key] = d
	}
}

func newBatchID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("records: no randomness for batch ids: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
