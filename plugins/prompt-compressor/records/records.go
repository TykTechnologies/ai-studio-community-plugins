// Package records carries the Prompt Compressor's savings from the edges
// to Studio: one Request per completed request at the edge, merged into
// hourly Deltas, sent as Batches with SendToControlJSON, and rolled up per
// hour, App, LLM, policy and outcome in Studio's KV.
//
// Records hold counts and ids only. No prompt, tool or model text leaves
// the edge; the model name is used at the edge to look up its price and is
// dropped there.
//
// The package has no SDK dependency.
package records

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"sync/atomic"
	"time"
)

// MetricType is the metadata value that marks a payload as savings
// records (metadata key "metric_type").
const MetricType = "prompt_compressor_savings"

// BatchVersion is the payload schema version.
const BatchVersion = 1

// Outcomes, as in the X-Tyk-Compression response header.
const (
	OutcomeApplied    = "applied"     // enforce: the compressed body was sent
	OutcomeAudit      = "audit"       // audit: computed, the original was sent
	OutcomeBypassed   = "bypassed"    // the client asked for no compression
	OutcomeSkipped    = "skipped"     // nothing to do (policy off, format, size…)
	OutcomeRejected   = "rejected"    // a rewrite failed its post-check
	OutcomeFailedOpen = "failed_open" // an error or panic; the original was sent
)

var validOutcomes = map[string]bool{
	OutcomeApplied: true, OutcomeAudit: true, OutcomeBypassed: true,
	OutcomeSkipped: true, OutcomeRejected: true, OutcomeFailedOpen: true,
}

// StrategyStats counts one strategy's work.
type StrategyStats struct {
	Leaves     int64 `json:"leaves"`
	BytesSaved int64 `json:"bytes_saved"`
}

// Request is one completed request at the edge.
type Request struct {
	At     time.Time
	AppID  uint32
	LLMID  uint32
	Vendor string
	// Model is the body's model name, used only to price the request at
	// the edge. It is never sent.
	Model string

	PolicyName string
	PolicyHash string
	Mode       string
	Level      int
	Outcome    string
	Reason     string

	Strategies      map[string]StrategyStats
	UnitsSeen       int
	UnitsSelected   int
	UnitsCompressed int
	BytesBefore     int
	BytesAfter      int
	// TokensBefore and TokensAfter are estimates over the changed leaves.
	TokensBefore int
	TokensAfter  int

	// CompressMicros is the time the Compress call took.
	CompressMicros int64

	// Usage from the vendor's response. UsageMissing means the response
	// had no usage block (counted, never guessed as zero).
	InputTokens      int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	UsageMissing     bool
	Streaming        bool
}

// TokensSaved is the estimated saving (zero when nothing was saved).
func (r *Request) TokensSaved() int64 {
	if s := int64(r.TokensBefore - r.TokensAfter); s > 0 {
		return s
	}
	return 0
}

// Recorder takes completed requests. The edge's Aggregator is one; a nil
// recorder is never passed around: use Nop.
type Recorder interface {
	Add(Request)
}

// Nop discards records.
type Nop struct{}

// Add implements Recorder.
func (Nop) Add(Request) {}

// Switch is a Recorder whose target can be set while requests are being
// recorded: the plugin holds one from the start, and the edge's
// aggregator is plugged in once the runtime is known. Until then, and
// after Set(nil), records are discarded.
type Switch struct {
	target atomic.Pointer[Recorder]
}

// Set routes records to r (nil discards them).
func (s *Switch) Set(r Recorder) {
	if r == nil {
		s.target.Store(nil)
		return
	}
	s.target.Store(&r)
}

// Add implements Recorder.
func (s *Switch) Add(r Request) {
	if t := s.target.Load(); t != nil {
		(*t).Add(r)
	}
}

// Key identifies one rollup row. Every part is an integer, a hex hash or
// an outcome constant, so it is safe in a KV key.
type Key struct {
	Hour       int64  `json:"hour"` // Unix hour (seconds / 3600)
	AppID      uint32 `json:"app_id"`
	LLMID      uint32 `json:"llm_id"`
	PolicyHash string `json:"policy_hash"`
	Outcome    string `json:"outcome"`
}

var hexHash = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

// Valid reports whether every part of the key is well formed.
func (k Key) Valid() error {
	if k.Hour <= 0 {
		return fmt.Errorf("hour %d out of range", k.Hour)
	}
	if k.PolicyHash != "" && !hexHash.MatchString(k.PolicyHash) {
		return fmt.Errorf("policy hash is not hex")
	}
	if !validOutcomes[k.Outcome] {
		return fmt.Errorf("unknown outcome")
	}
	return nil
}

// Row is the key as a KV-safe string.
func (k Key) Row() string {
	return fmt.Sprintf("%d:%d:%d:%s:%s", k.Hour, k.AppID, k.LLMID, k.PolicyHash, k.Outcome)
}

// HourOf is the Unix hour bucket of t.
func HourOf(t time.Time) int64 { return t.Unix() / 3600 }

// LatencyBoundsMicros are the upper bounds of the compression-time
// histogram buckets; the last bucket is everything above. Fixed bounds make
// histograms from different edges and hours add up.
var LatencyBoundsMicros = []int64{50, 100, 250, 500, 1000, 2500, 5000, 10000, 25000, 50000, 100000}

// Delta is the sum of the requests with one Key. Deltas add: merging two
// gives the same totals as one delta over both sets of requests.
type Delta struct {
	Key
	// PolicyName, Mode and Level describe the policy for display; a hash
	// names one policy version, so they agree within a key.
	PolicyName string `json:"policy_name,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Level      int    `json:"level"`
	// Overflow marks Studio's row for an hour's rows past MaxRowsPerHour:
	// its App, LLM and policy are mixed. Edges never send one.
	Overflow bool `json:"overflow,omitempty"`

	Requests        int64 `json:"requests"`
	UnitsSeen       int64 `json:"units_seen"`
	UnitsSelected   int64 `json:"units_selected"`
	UnitsCompressed int64 `json:"units_compressed"`
	BytesBefore     int64 `json:"bytes_before"`
	BytesAfter      int64 `json:"bytes_after"`
	TokensBefore    int64 `json:"tokens_before"`
	TokensAfter     int64 `json:"tokens_after"`
	TokensSaved     int64 `json:"tokens_saved"`

	// CostSaved is TokensSaved priced at each request's model input price,
	// by currency: an upper bound, since a saved token that would have been
	// a cache read was only worth the cache-read price. Requests without a
	// price add to UnpricedTokensSaved.
	CostSaved           map[string]float64 `json:"cost_saved,omitempty"`
	UnpricedTokensSaved int64              `json:"unpriced_tokens_saved"`
	// CostSavedAtCacheRead prices the same saved tokens at the model's
	// cache-read price, for the cache-adjusted lower bound (with the
	// cache-read share). Priced requests whose model has no cache-read
	// price add to NoCacheReadPriceTokensSaved instead.
	CostSavedAtCacheRead        map[string]float64 `json:"cost_saved_at_cache_read,omitempty"`
	NoCacheReadPriceTokensSaved int64              `json:"no_cache_read_price_tokens_saved"`

	InputTokens      int64 `json:"input_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	UsageMissing     int64 `json:"usage_missing"`
	Streaming        int64 `json:"streaming"`

	Strategies map[string]StrategyStats `json:"strategies,omitempty"`
	// Reasons counts requests by skip or reject reason (body_too_large,
	// client_bypass, app_scope_missing…), at most MaxReasonsPerRow names.
	Reasons map[string]int64 `json:"reasons,omitempty"`
	// LatencyBuckets counts Compress times per LatencyBoundsMicros bucket
	// (len(LatencyBoundsMicros)+1 entries).
	LatencyBuckets []int64 `json:"latency_buckets"`
	LatencyMicros  int64   `json:"latency_micros"`
}

// NewDelta returns an empty delta for k.
func NewDelta(k Key) *Delta {
	return &Delta{Key: k, LatencyBuckets: make([]int64, len(LatencyBoundsMicros)+1)}
}

// KeyOf is the rollup key of a request.
func KeyOf(r Request) Key {
	return Key{Hour: HourOf(r.At), AppID: r.AppID, LLMID: r.LLMID, PolicyHash: r.PolicyHash, Outcome: r.Outcome}
}

// Price is a model's price per token from the edge's price list.
type Price struct {
	Input float64
	// CacheRead is zero when the price list has no cache-read price.
	CacheRead float64
	Currency  string
}

// AddRequest adds one request priced at p; priced false leaves the saving
// unpriced.
func (d *Delta) AddRequest(r Request, p Price, priced bool) {
	d.PolicyName, d.Mode, d.Level = r.PolicyName, r.Mode, r.Level
	d.Requests++
	d.UnitsSeen += int64(r.UnitsSeen)
	d.UnitsSelected += int64(r.UnitsSelected)
	d.UnitsCompressed += int64(r.UnitsCompressed)
	d.BytesBefore += int64(r.BytesBefore)
	d.BytesAfter += int64(r.BytesAfter)
	d.TokensBefore += int64(r.TokensBefore)
	d.TokensAfter += int64(r.TokensAfter)
	saved := r.TokensSaved()
	d.TokensSaved += saved
	if saved > 0 {
		if priced {
			d.CostSaved = addCost(d.CostSaved, p.Currency, float64(saved)*p.Input, true)
			if p.CacheRead > 0 {
				d.CostSavedAtCacheRead = addCost(d.CostSavedAtCacheRead, p.Currency, float64(saved)*p.CacheRead, true)
			} else {
				d.NoCacheReadPriceTokensSaved += saved
			}
		} else {
			d.UnpricedTokensSaved += saved
		}
	}
	d.InputTokens += r.InputTokens
	d.CacheReadTokens += r.CacheReadTokens
	d.CacheWriteTokens += r.CacheWriteTokens
	if r.UsageMissing {
		d.UsageMissing++
	}
	if r.Streaming {
		d.Streaming++
	}
	for name, s := range r.Strategies {
		d.Strategies = addStrategy(d.Strategies, name, s, true)
	}
	if r.Reason != "" {
		d.Reasons = addReason(d.Reasons, r.Reason, 1, true)
	}
	d.observeLatency(r.CompressMicros)
}

// MaxReasonsPerRow bounds a row's reason names; further names add to
// OtherStrategy ("other").
const MaxReasonsPerRow = 16

func addReason(m map[string]int64, name string, n int64, allowNew bool) map[string]int64 {
	if m == nil {
		m = map[string]int64{}
	}
	if !strategyName.MatchString(name) {
		name = OtherStrategy
	}
	if _, ok := m[name]; !ok && name != OtherStrategy && (!allowNew || len(m) >= MaxReasonsPerRow-1) {
		name = OtherStrategy
	}
	m[name] += n
	return m
}

func (d *Delta) observeLatency(micros int64) {
	if len(d.LatencyBuckets) != len(LatencyBoundsMicros)+1 {
		d.LatencyBuckets = make([]int64, len(LatencyBoundsMicros)+1)
	}
	i := sort.Search(len(LatencyBoundsMicros), func(i int) bool { return micros <= LatencyBoundsMicros[i] })
	d.LatencyBuckets[i]++
	d.LatencyMicros += micros
}

// Bounds on a row's maps, whatever its deltas name: a strategy or currency
// past the bound is added to OtherStrategy or OtherCurrency.
const (
	MaxStrategiesPerRow = 64
	MaxCurrenciesPerRow = 8
	OtherStrategy       = "other"
	// OtherCurrency is ISO 4217's code for "no currency".
	OtherCurrency = "XXX"
)

// addStrategy adds s under name. With allowNew false, a name the map does
// not hold yet goes to OtherStrategy.
func addStrategy(m map[string]StrategyStats, name string, s StrategyStats, allowNew bool) map[string]StrategyStats {
	if m == nil {
		m = map[string]StrategyStats{}
	}
	if _, ok := m[name]; !ok && name != OtherStrategy && (!allowNew || len(m) >= MaxStrategiesPerRow-1) {
		name = OtherStrategy
	}
	cur := m[name]
	cur.Leaves += s.Leaves
	cur.BytesSaved += s.BytesSaved
	m[name] = cur
	return m
}

// addCost adds v under currency. With allowNew false, a currency the map
// does not hold yet goes to OtherCurrency.
func addCost(m map[string]float64, currency string, v float64, allowNew bool) map[string]float64 {
	if m == nil {
		m = map[string]float64{}
	}
	if !currencyCode.MatchString(currency) {
		currency = OtherCurrency
	}
	if _, ok := m[currency]; !ok && currency != OtherCurrency && (!allowNew || len(m) >= MaxCurrenciesPerRow-1) {
		currency = OtherCurrency
	}
	m[currency] += v
	return m
}

var (
	currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
	strategyName = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
)

// Merge adds o into d. The keys must match.
func (d *Delta) Merge(o *Delta) { d.merge(o, true) }

// mergeKnown adds o into d without adding strategy or currency names d
// does not hold: those go to OtherStrategy and OtherCurrency. Studio uses
// it once an hour document is at its size budget.
func (d *Delta) mergeKnown(o *Delta) { d.merge(o, false) }

func (d *Delta) merge(o *Delta, allowNew bool) {
	if o.PolicyName != "" {
		d.PolicyName, d.Mode, d.Level = o.PolicyName, o.Mode, o.Level
	}
	d.Requests += o.Requests
	d.UnitsSeen += o.UnitsSeen
	d.UnitsSelected += o.UnitsSelected
	d.UnitsCompressed += o.UnitsCompressed
	d.BytesBefore += o.BytesBefore
	d.BytesAfter += o.BytesAfter
	d.TokensBefore += o.TokensBefore
	d.TokensAfter += o.TokensAfter
	d.TokensSaved += o.TokensSaved
	for c, v := range o.CostSaved {
		d.CostSaved = addCost(d.CostSaved, c, v, allowNew)
	}
	d.UnpricedTokensSaved += o.UnpricedTokensSaved
	for c, v := range o.CostSavedAtCacheRead {
		d.CostSavedAtCacheRead = addCost(d.CostSavedAtCacheRead, c, v, allowNew)
	}
	d.NoCacheReadPriceTokensSaved += o.NoCacheReadPriceTokensSaved
	d.InputTokens += o.InputTokens
	d.CacheReadTokens += o.CacheReadTokens
	d.CacheWriteTokens += o.CacheWriteTokens
	d.UsageMissing += o.UsageMissing
	d.Streaming += o.Streaming
	for name, s := range o.Strategies {
		d.Strategies = addStrategy(d.Strategies, name, s, allowNew)
	}
	for name, n := range o.Reasons {
		d.Reasons = addReason(d.Reasons, name, n, allowNew)
	}
	if len(d.LatencyBuckets) != len(LatencyBoundsMicros)+1 {
		d.LatencyBuckets = make([]int64, len(LatencyBoundsMicros)+1)
	}
	for i := range d.LatencyBuckets {
		if i < len(o.LatencyBuckets) {
			d.LatencyBuckets[i] += o.LatencyBuckets[i]
		}
	}
	d.LatencyMicros += o.LatencyMicros
}

// Batch is one payload from an edge.
type Batch struct {
	Version int `json:"v"`
	// ID makes delivery idempotent: Studio applies a batch ID once.
	ID     string   `json:"id"`
	Deltas []*Delta `json:"deltas"`
}

var batchID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Validate checks a batch received from an edge before anything is stored.
func (b *Batch) Validate() error {
	if b.Version != BatchVersion {
		return fmt.Errorf("unsupported batch version %d", b.Version)
	}
	if !batchID.MatchString(b.ID) {
		return fmt.Errorf("batch id is not 32 hex digits")
	}
	if len(b.Deltas) > MaxBatchDeltas {
		return fmt.Errorf("%d deltas, at most %d", len(b.Deltas), MaxBatchDeltas)
	}
	for i, d := range b.Deltas {
		if d == nil {
			return fmt.Errorf("delta %d is empty", i)
		}
		if err := d.Key.Valid(); err != nil {
			return fmt.Errorf("delta %d: %w", i, err)
		}
		if d.Overflow {
			return fmt.Errorf("delta %d: overflow rows are Studio's", i)
		}
		if len(d.PolicyName) > 64 || len(d.Mode) > 16 {
			return fmt.Errorf("delta %d: policy name or mode too long", i)
		}
		if len(d.Strategies) > MaxStrategiesPerRow || len(d.CostSaved) > MaxCurrenciesPerRow || len(d.CostSavedAtCacheRead) > MaxCurrenciesPerRow || len(d.LatencyBuckets) > len(LatencyBoundsMicros)+1 {
			return fmt.Errorf("delta %d: too many strategies, currencies or latency buckets", i)
		}
		for name, s := range d.Strategies {
			if !strategyName.MatchString(name) || s.Leaves < 0 || s.BytesSaved < 0 {
				return fmt.Errorf("delta %d: bad strategy entry", i)
			}
		}
		if len(d.Reasons) > MaxReasonsPerRow {
			return fmt.Errorf("delta %d: too many reasons", i)
		}
		for name, n := range d.Reasons {
			if !strategyName.MatchString(name) || n < 0 {
				return fmt.Errorf("delta %d: bad reason entry", i)
			}
		}
		for _, costs := range []map[string]float64{d.CostSaved, d.CostSavedAtCacheRead} {
			for c, v := range costs {
				if !currencyCode.MatchString(c) || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
					return fmt.Errorf("delta %d: bad cost entry", i)
				}
			}
		}
		for _, n := range append([]int64{d.Requests, d.UnitsSeen, d.UnitsSelected, d.UnitsCompressed,
			d.BytesBefore, d.BytesAfter, d.TokensBefore, d.TokensAfter, d.TokensSaved, d.UnpricedTokensSaved, d.NoCacheReadPriceTokensSaved,
			d.InputTokens, d.CacheReadTokens, d.CacheWriteTokens, d.UsageMissing, d.Streaming, d.LatencyMicros},
			d.LatencyBuckets...) {
			if n < 0 {
				return fmt.Errorf("delta %d: negative count", i)
			}
		}
	}
	return nil
}

// Batch bounds. The edge queue takes payloads up to 1 MB; the aggregator
// splits a flush into batches under MaxBatchBytes.
const (
	MaxBatchDeltas = 2000
	MaxBatchBytes  = 900 << 10
)
