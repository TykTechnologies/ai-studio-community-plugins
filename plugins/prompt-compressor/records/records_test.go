package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

var errNotFound = errors.New("not found")

// memKV is an in-memory KV with the host's not-found behaviour.
type memKV struct {
	mu     sync.Mutex
	data   map[string][]byte
	writes int
	failOn string
}

func newMemKV() *memKV { return &memKV{data: map[string][]byte{}} }

func (m *memKV) Read(ctx context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	if !ok {
		return nil, errNotFound
	}
	return append([]byte(nil), v...), nil
}

func (m *memKV) Write(ctx context.Context, key string, value []byte, expireAt *time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn != "" && strings.Contains(key, m.failOn) {
		return false, errors.New("kv write failed")
	}
	_, existed := m.data[key]
	m.data[key] = append([]byte(nil), value...)
	m.writes++
	return !existed, nil
}

func (m *memKV) Delete(ctx context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.data[key]
	delete(m.data, key)
	return ok, nil
}

func (m *memKV) keys(prefix string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out
}

const hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

var base = time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)

func req(at time.Time, app, llm uint32, hash, outcome string, before, after int) Request {
	return Request{
		At: at, AppID: app, LLMID: llm, Vendor: "anthropic", Model: "claude-x",
		PolicyName: "agents", PolicyHash: hash, Mode: "enforce", Level: 2, Outcome: outcome,
		Strategies: map[string]StrategyStats{"json_compact": {Leaves: 1, BytesSaved: int64(before - after)}},
		UnitsSeen:  3, UnitsSelected: 2, UnitsCompressed: 1,
		TokensBefore: before, TokensAfter: after, BytesBefore: before * 4, BytesAfter: after * 4,
		CompressMicros: 300, InputTokens: 10000, CacheReadTokens: 6000,
	}
}

func price(vendor, model string) (float64, string, bool) {
	if model == "claude-x" {
		return 0.000003, "USD", true
	}
	return 0, "", false
}

func newStore(now time.Time) (*Store, *memKV) {
	kv := newMemKV()
	s := NewStore(kv, func(err error) bool { return errors.Is(err, errNotFound) })
	s.now = func() time.Time { return now }
	return s, kv
}

func total(rows []*Delta) (requests, saved int64) {
	for _, d := range rows {
		requests += d.Requests
		saved += d.TokensSaved
	}
	return
}

func TestDeltasMergeAcrossEdgesAndDuplicateBatches(t *testing.T) {
	s, _ := newStore(base)
	ctx := context.Background()

	edge1, edge2 := NewAggregator(price), NewAggregator(price)
	var wantSaved int64
	for i := 0; i < 50; i++ {
		r := req(base.Add(time.Duration(i)*time.Minute), 7, 3, hashA, OutcomeApplied, 1000, 400)
		edge1.Add(r)
		wantSaved += r.TokensSaved()
		r2 := req(base.Add(time.Duration(i)*time.Minute), 7, 3, hashA, OutcomeApplied, 500, 450)
		edge2.Add(r2)
		wantSaved += r2.TokensSaved()
	}
	b1, b2 := edge1.Flush(), edge2.Flush()
	for _, b := range b1 {
		if err := s.Apply(ctx, "", "edge-1", b); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range b2 {
		if err := s.Apply(ctx, "eu", "edge-2", b); err != nil {
			t.Fatal(err)
		}
	}
	// Redelivered batches change nothing
	for _, b := range b1 {
		if err := s.Apply(ctx, "", "edge-1", b); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range b2 {
		if err := s.Apply(ctx, "eu", "edge-2", b); err != nil {
			t.Fatal(err)
		}
	}

	rows, markers, err := s.Query(ctx, HourOf(base)-1, HourOf(base)+2)
	if err != nil {
		t.Fatal(err)
	}
	requests, saved := total(rows)
	if requests != 100 || saved != wantSaved {
		t.Fatalf("totals = %d requests, %d tokens saved; want 100 and %d", requests, saved, wantSaved)
	}
	var cost float64
	for _, d := range rows {
		cost += d.CostSaved["USD"]
	}
	if diff := cost - float64(wantSaved)*0.000003; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost saved %v, want %v", cost, float64(wantSaved)*0.000003)
	}
	if len(markers) != 1 || markers[0].Hash != hashA || markers[0].FirstSeen != HourOf(base) {
		t.Errorf("markers = %+v", markers)
	}
}

// Edge redelivery after a hub restart: the store holds nothing in memory,
// so a fresh store over the same KV is the restarted hub.
func TestRedeliveryAfterRestartIsIdempotent(t *testing.T) {
	s, kv := newStore(base)
	ctx := context.Background()
	agg := NewAggregator(nil)
	for i := 0; i < 10; i++ {
		agg.Add(req(base, 1, 1, hashA, OutcomeAudit, 100, 50))
	}
	batches := agg.Flush()
	if err := s.Apply(ctx, "", "e", batches[0]); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(kv, s.notFound)
	restarted.now = s.now
	if err := restarted.Apply(ctx, "", "e", batches[0]); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := restarted.Query(ctx, 0, HourOf(base)+1)
	if requests, saved := total(rows); requests != 10 || saved != 500 {
		t.Errorf("after redelivery: %d requests, %d saved; want 10, 500", requests, saved)
	}
}

func TestOutOfOrderHours(t *testing.T) {
	s, _ := newStore(base.Add(5 * time.Hour))
	ctx := context.Background()
	for _, offset := range []int{4, 1, 3, 0, 2} {
		agg := NewAggregator(nil)
		agg.Add(req(base.Add(time.Duration(offset)*time.Hour), 1, 1, hashA, OutcomeApplied, 100, 90))
		for _, b := range agg.Flush() {
			if err := s.Apply(ctx, "", "e", b); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, _, _ := s.Query(ctx, HourOf(base)+1, HourOf(base)+3)
	if len(rows) != 3 {
		t.Fatalf("hours 1..3 gave %d rows, want 3", len(rows))
	}
	for i, d := range rows {
		if d.Hour != HourOf(base)+int64(i)+1 {
			t.Errorf("row %d is hour %d", i, d.Hour)
		}
	}
	var held []int64
	_, _ = s.read(ctx, hoursKey(EdgeKey("", "e")), &held)
	for i := 1; i < len(held); i++ {
		if held[i] <= held[i-1] {
			t.Fatalf("hour index not sorted: %v", held)
		}
	}
}

func TestRetentionRemovesOldBucketsAndBoundsTheIndex(t *testing.T) {
	now := base
	s, kv := newStore(now)
	s.retention = 48 * time.Hour
	ctx := context.Background()

	// A week of hourly batches, applied as time passes
	for h := 0; h < 7*24; h++ {
		at := base.Add(time.Duration(h) * time.Hour)
		s.now = func() time.Time { return at }
		agg := NewAggregator(nil)
		agg.Add(req(at, 1, 1, hashA, OutcomeApplied, 100, 90))
		for _, b := range agg.Flush() {
			if err := s.Apply(ctx, "", "e", b); err != nil {
				t.Fatal(err)
			}
		}
	}
	var held []int64
	_, _ = s.read(ctx, hoursKey(EdgeKey("", "e")), &held)
	if len(held) > 49 {
		t.Errorf("index holds %d hours with 48h retention", len(held))
	}
	if docs := kv.keys("pc:e:" + EdgeKey("", "e") + ":h:"); len(docs) != len(held) {
		t.Errorf("%d hour documents for %d indexed hours: expired documents were not deleted", len(docs), len(held))
	}
	// A late batch for an hour past retention is not stored
	agg := NewAggregator(nil)
	agg.Add(req(base, 1, 1, hashA, OutcomeApplied, 100, 90))
	for _, b := range agg.Flush() {
		if err := s.Apply(ctx, "", "e", b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := kv.Read(ctx, hourKey(EdgeKey("", "e"), HourOf(base))); err == nil {
		t.Error("a batch past retention was stored")
	}
}

func TestBatchIDListInAnHourIsBounded(t *testing.T) {
	s, _ := newStore(base)
	s.batchIDsPerHour = 100
	ctx := context.Background()
	for i := 0; i < s.batchIDsPerHour+50; i++ {
		agg := NewAggregator(nil)
		agg.Add(req(base, 1, 1, hashA, OutcomeApplied, 10, 9))
		for _, b := range agg.Flush() {
			if err := s.Apply(ctx, "", "e", b); err != nil {
				t.Fatal(err)
			}
		}
	}
	doc := hourDoc{}
	_, _ = s.read(ctx, hourKey(EdgeKey("", "e"), HourOf(base)), &doc)
	if len(doc.Batches) != s.batchIDsPerHour {
		t.Errorf("hour holds %d batch ids, want %d", len(doc.Batches), s.batchIDsPerHour)
	}
	if doc.Rows[KeyOf(req(base, 1, 1, hashA, OutcomeApplied, 10, 9)).Row()].Requests != int64(s.batchIDsPerHour+50) {
		t.Error("requests were lost while trimming batch ids")
	}
}

func TestInvalidBatchesStoreNothing(t *testing.T) {
	s, kv := newStore(base)
	ctx := context.Background()
	good := func() *Batch {
		agg := NewAggregator(nil)
		agg.Add(req(base, 1, 1, hashA, OutcomeApplied, 10, 9))
		return agg.Flush()[0]
	}
	cases := map[string]func(b *Batch){
		"version":       func(b *Batch) { b.Version = 2 },
		"id":            func(b *Batch) { b.ID = "../../pc:edges" },
		"outcome":       func(b *Batch) { b.Deltas[0].Outcome = "x:y" },
		"hash":          func(b *Batch) { b.Deltas[0].PolicyHash = "not-hex" },
		"hour":          func(b *Batch) { b.Deltas[0].Hour = 0 },
		"negative":      func(b *Batch) { b.Deltas[0].TokensSaved = -5 },
		"long name":     func(b *Batch) { b.Deltas[0].PolicyName = strings.Repeat("x", 65) },
		"nil delta":     func(b *Batch) { b.Deltas = append(b.Deltas, nil) },
		"many buckets":  func(b *Batch) { b.Deltas[0].LatencyBuckets = make([]int64, 50) },
		"bad currency":  func(b *Batch) { b.Deltas[0].CostSaved = map[string]float64{"USD": -1} },
		"bad strategy":  func(b *Batch) { b.Deltas[0].Strategies = map[string]StrategyStats{"x": {Leaves: -1}} },
		"too many rows": func(b *Batch) { b.Deltas = make([]*Delta, MaxBatchDeltas+1) },
	}
	for name, mutate := range cases {
		b := good()
		mutate(b)
		if err := s.Apply(ctx, "", "e", b); err == nil {
			t.Errorf("%s: an invalid batch was applied", name)
		}
	}
	if kv.writes != 0 {
		t.Errorf("%d KV writes for invalid batches", kv.writes)
	}
}

func TestAggregatorSplitsLargeFlushesAndRestores(t *testing.T) {
	agg := NewAggregator(price)
	for i := 0; i < 3000; i++ {
		agg.Add(req(base, uint32(i+1), 1, hashA, OutcomeApplied, 100, 50))
	}
	batches := agg.Flush()
	if len(batches) < 2 {
		t.Fatalf("3000 rows in %d batch(es)", len(batches))
	}
	ids := map[string]bool{}
	var rows int
	for _, b := range batches {
		raw, _ := json.Marshal(b)
		if len(raw) > 1<<20 {
			t.Errorf("batch of %d bytes exceeds the 1 MB payload limit", len(raw))
		}
		if err := b.Validate(); err != nil {
			t.Errorf("flushed batch invalid: %v", err)
		}
		if ids[b.ID] {
			t.Error("two batches share an id")
		}
		ids[b.ID] = true
		rows += len(b.Deltas)
	}
	if rows != 3000 {
		t.Errorf("%d rows across batches, want 3000", rows)
	}
	if agg.Flush() != nil {
		t.Error("a second flush returned data")
	}

	// An unsent batch comes back with the next flush
	agg.Restore(batches[0])
	agg.Add(req(base, 1, 1, hashA, OutcomeApplied, 100, 50))
	again := agg.Flush()
	var requests int64
	for _, b := range again {
		for _, d := range b.Deltas {
			requests += d.Requests
		}
	}
	if want := int64(len(batches[0].Deltas)) + 1; requests != want {
		t.Errorf("after restore %d requests, want %d", requests, want)
	}
	if again[0].ID == batches[0].ID {
		t.Error("a restored batch kept its old id")
	}
}

func TestAggregatorIsBoundedAndDropsInvalidRequests(t *testing.T) {
	agg := NewAggregator(nil)
	agg.maxKeys = 10
	for i := 0; i < 15; i++ {
		agg.Add(req(base, uint32(i+1), 1, hashA, OutcomeApplied, 10, 9))
	}
	agg.Add(req(base, 1, 1, hashA, "bogus", 10, 9))
	agg.Add(req(base, 1, 1, "XYZ", OutcomeApplied, 10, 9))
	if !agg.Full() {
		t.Error("Full() false at the key limit")
	}
	if got := agg.Dropped(); got != 7 {
		t.Errorf("dropped %d, want 7 (5 over the limit, 2 invalid)", got)
	}
	// An existing key still counts at the limit
	agg.Add(req(base, 1, 1, hashA, OutcomeApplied, 10, 9))
	b := agg.Flush()
	if len(b[0].Deltas) != 10 {
		t.Errorf("%d keys, want 10", len(b[0].Deltas))
	}
}

func TestDeltaAddAndMergeAgree(t *testing.T) {
	one := NewDelta(KeyOf(req(base, 1, 1, hashA, OutcomeApplied, 0, 0)))
	a, b := NewDelta(one.Key), NewDelta(one.Key)
	for i := 0; i < 20; i++ {
		r := req(base, 1, 1, hashA, OutcomeApplied, 100+i, 50)
		r.CompressMicros = int64(i * 1000)
		r.UsageMissing = i%3 == 0
		one.AddRequest(r, 0.000003, "USD", true)
		if i%2 == 0 {
			a.AddRequest(r, 0.000003, "USD", true)
		} else {
			b.AddRequest(r, 0.000003, "USD", true)
		}
	}
	a.Merge(b)
	ja, _ := json.Marshal(a)
	jo, _ := json.Marshal(one)
	if string(ja) != string(jo) {
		t.Errorf("merge differs from adding everything:\n%s\n%s", ja, jo)
	}
	var n int64
	for _, c := range one.LatencyBuckets {
		n += c
	}
	if n != 20 {
		t.Errorf("latency histogram counts %d, want 20", n)
	}
}

func TestUnpricedSavings(t *testing.T) {
	agg := NewAggregator(price)
	r := req(base, 1, 1, hashA, OutcomeApplied, 100, 40)
	r.Model = "unknown-model"
	agg.Add(r)
	d := agg.Flush()[0].Deltas[0]
	if d.UnpricedTokensSaved != 60 || len(d.CostSaved) != 0 {
		t.Errorf("unpriced = %d, cost = %v", d.UnpricedTokensSaved, d.CostSaved)
	}
}

func TestKVWriteFailureIsReturned(t *testing.T) {
	s, kv := newStore(base)
	kv.failOn = ":h:"
	agg := NewAggregator(nil)
	agg.Add(req(base, 1, 1, hashB, OutcomeApplied, 10, 9))
	if err := s.Apply(context.Background(), "", "e", agg.Flush()[0]); err == nil {
		t.Error("a failed KV write was not reported (the edge must redeliver)")
	}
	_ = fmt.Sprint()
}
