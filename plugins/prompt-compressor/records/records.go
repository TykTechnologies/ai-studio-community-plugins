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
	"regexp"
	"sort"
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
	// by currency. Requests without a price add to UnpricedTokensSaved.
	CostSaved           map[string]float64 `json:"cost_saved,omitempty"`
	UnpricedTokensSaved int64              `json:"unpriced_tokens_saved"`

	InputTokens      int64 `json:"input_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	UsageMissing     int64 `json:"usage_missing"`
	Streaming        int64 `json:"streaming"`

	Strategies map[string]StrategyStats `json:"strategies,omitempty"`
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

// AddRequest adds one request. price is its model's input price per token
// and currency; ok false leaves the saving unpriced.
func (d *Delta) AddRequest(r Request, inputPrice float64, currency string, priced bool) {
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
			if d.CostSaved == nil {
				d.CostSaved = map[string]float64{}
			}
			d.CostSaved[currency] += float64(saved) * inputPrice
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
		if d.Strategies == nil {
			d.Strategies = map[string]StrategyStats{}
		}
		cur := d.Strategies[name]
		cur.Leaves += s.Leaves
		cur.BytesSaved += s.BytesSaved
		d.Strategies[name] = cur
	}
	d.observeLatency(r.CompressMicros)
}

func (d *Delta) observeLatency(micros int64) {
	if len(d.LatencyBuckets) != len(LatencyBoundsMicros)+1 {
		d.LatencyBuckets = make([]int64, len(LatencyBoundsMicros)+1)
	}
	i := sort.Search(len(LatencyBoundsMicros), func(i int) bool { return micros <= LatencyBoundsMicros[i] })
	d.LatencyBuckets[i]++
	d.LatencyMicros += micros
}

// Merge adds o into d. The keys must match.
func (d *Delta) Merge(o *Delta) {
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
		if d.CostSaved == nil {
			d.CostSaved = map[string]float64{}
		}
		d.CostSaved[c] += v
	}
	d.UnpricedTokensSaved += o.UnpricedTokensSaved
	d.InputTokens += o.InputTokens
	d.CacheReadTokens += o.CacheReadTokens
	d.CacheWriteTokens += o.CacheWriteTokens
	d.UsageMissing += o.UsageMissing
	d.Streaming += o.Streaming
	for name, s := range o.Strategies {
		if d.Strategies == nil {
			d.Strategies = map[string]StrategyStats{}
		}
		cur := d.Strategies[name]
		cur.Leaves += s.Leaves
		cur.BytesSaved += s.BytesSaved
		d.Strategies[name] = cur
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
		if len(d.PolicyName) > 64 || len(d.Mode) > 16 {
			return fmt.Errorf("delta %d: policy name or mode too long", i)
		}
		if len(d.Strategies) > 32 || len(d.CostSaved) > 4 || len(d.LatencyBuckets) > len(LatencyBoundsMicros)+1 {
			return fmt.Errorf("delta %d: too many strategies, currencies or latency buckets", i)
		}
		for name, s := range d.Strategies {
			if len(name) > 64 || s.Leaves < 0 || s.BytesSaved < 0 {
				return fmt.Errorf("delta %d: bad strategy entry", i)
			}
		}
		for c, v := range d.CostSaved {
			if len(c) > 8 || v < 0 {
				return fmt.Errorf("delta %d: bad cost entry", i)
			}
		}
		for _, n := range append([]int64{d.Requests, d.UnitsSeen, d.UnitsSelected, d.UnitsCompressed,
			d.BytesBefore, d.BytesAfter, d.TokensBefore, d.TokensAfter, d.TokensSaved, d.UnpricedTokensSaved,
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
