package records

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// An edge chooses its hours, ids and hashes. These tests are eac.49: the
// store must stay bounded whatever an edge sends.

func batchOf(deltas ...*Delta) *Batch {
	return &Batch{Version: BatchVersion, ID: newBatchID(), Deltas: deltas}
}

func deltaAt(hour int64, app uint32, hash string) *Delta {
	d := NewDelta(Key{Hour: hour, AppID: app, LLMID: 1, PolicyHash: hash, Outcome: OutcomeApplied})
	d.AddRequest(req(time.Unix(hour*3600, 0), app, 1, hash, OutcomeApplied, 100, 60), Price{}, false)
	return d
}

func hexOf(i int) string { return fmt.Sprintf("%064x", i+1) }

func TestFutureHoursAreRefused(t *testing.T) {
	s, kv := newStore(base)
	ctx := context.Background()
	now := HourOf(base)
	b := batchOf(deltaAt(now+2, 1, hashA), deltaAt(now+1000, 1, hashA), deltaAt(now+1, 2, hashA), deltaAt(now, 3, hashA))
	if err := s.Apply(ctx, "", "e", b); err != nil {
		t.Fatal(err)
	}
	if st := s.Stats(); st.Future != 2 {
		t.Errorf("future deltas counted %d, want 2", st.Future)
	}
	var held []int64
	_, _ = s.read(ctx, hoursKey(EdgeKey("", "e")), &held)
	for _, h := range held {
		if h > now+1 {
			t.Errorf("hour %d (now %d) was indexed", h, now)
		}
	}
	if len(kv.keys("pc:e:"+EdgeKey("", "e")+":h:")) != 2 {
		t.Errorf("hour documents: %v", kv.keys("pc:e:"))
	}
	// The policy marker is not dated in the future either
	_, markers, _ := s.Query(ctx, 0, now+5000)
	if len(markers) != 1 || markers[0].LastSeen > now+1 {
		t.Errorf("markers = %+v", markers)
	}
}

func TestRowsPerHourAreCappedAndTotalsKept(t *testing.T) {
	s, _ := newStore(base)
	ctx := context.Background()
	hour := HourOf(base)
	var all []*Delta
	for app := 1; app <= MaxRowsPerHour+1; app++ {
		all = append(all, deltaAt(hour, uint32(app), hashA))
	}
	for i := 0; i < len(all); i += MaxBatchDeltas {
		end := i + MaxBatchDeltas
		if end > len(all) {
			end = len(all)
		}
		if err := s.Apply(ctx, "", "e", batchOf(all[i:end]...)); err != nil {
			t.Fatal(err)
		}
	}
	doc := hourDoc{}
	_, _ = s.read(ctx, hourKey(EdgeKey("", "e"), hour), &doc)
	if len(doc.Rows) != MaxRowsPerHour+1 {
		t.Fatalf("hour holds %d rows, want %d plus one overflow row", len(doc.Rows), MaxRowsPerHour)
	}
	over := doc.Rows[overflowRow(OutcomeApplied)]
	if over == nil || !over.Overflow || over.Requests != 1 || doc.Folded != 1 || s.Stats().Folded != 1 {
		t.Fatalf("overflow row %+v, folded %d / %d; want the 5,001st row folded and counted", over, doc.Folded, s.Stats().Folded)
	}
	// An existing row still merges in a full hour, outside the overflow row
	if err := s.Apply(ctx, "", "e", batchOf(deltaAt(hour, 7, hashA))); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := s.Query(ctx, hour, hour)
	requests, saved := total(rows)
	if requests != int64(MaxRowsPerHour+2) || saved != int64(MaxRowsPerHour+2)*40 {
		t.Errorf("totals %d requests / %d saved, want every request kept", requests, saved)
	}
}

func TestPolicyMarkersAreCappedAndExpire(t *testing.T) {
	s, kv := newStore(base)
	ctx := context.Background()
	hour := HourOf(base)
	var deltas []*Delta
	for i := 0; i < MaxPolicyMarkers+100; i++ {
		deltas = append(deltas, deltaAt(hour-int64(i%24), 1, hexOf(i)))
	}
	if err := s.Apply(ctx, "", "e", batchOf(deltas...)); err != nil {
		t.Fatal(err)
	}
	doc := policiesDoc{}
	_, _ = s.read(ctx, policiesKey, &doc)
	if len(doc.Policies) != MaxPolicyMarkers {
		t.Fatalf("%d markers, cap %d", len(doc.Policies), MaxPolicyMarkers)
	}
	if keys := kv.keys("pc:policy:"); len(keys) != 0 {
		t.Errorf("per-hash marker keys written: %d", len(keys))
	}
	// The least recently seen went first
	for _, m := range doc.Policies {
		if m.LastSeen < hour-23 {
			t.Fatalf("marker last seen %d kept", m.LastSeen)
		}
	}

	// After the retention, a batch with a new policy leaves only that marker
	later := base.Add(DefaultRetention + 48*time.Hour)
	s.now = func() time.Time { return later }
	if err := s.Apply(ctx, "", "e", batchOf(deltaAt(HourOf(later), 1, hashB))); err != nil {
		t.Fatal(err)
	}
	doc = policiesDoc{}
	_, _ = s.read(ctx, policiesKey, &doc)
	if len(doc.Policies) != 1 || doc.Policies[hashB] == nil {
		t.Errorf("after retention %d markers remain, want only the new one", len(doc.Policies))
	}
}

func TestLegitimateBatchAfterHostileOne(t *testing.T) {
	s, _ := newStore(base)
	ctx := context.Background()
	hour := HourOf(base)

	hostile := batchOf(deltaAt(hour+5000, 1, hashA), deltaAt(hour-24*365, 1, hashA))
	if err := s.Apply(ctx, "", "e", hostile); err != nil {
		t.Fatal(err)
	}
	bad := batchOf(deltaAt(hour, 1, hashA))
	bad.Deltas[0].Overflow = true
	if err := s.Apply(ctx, "", "e", bad); err == nil {
		t.Error("an edge-sent overflow row was accepted")
	}

	agg := NewAggregator(nil)
	agg.Add(req(base, 9, 1, hashA, OutcomeApplied, 500, 100))
	if err := s.Apply(ctx, "", "e", agg.Flush()[0]); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := s.Query(ctx, 0, hour+10000)
	if requests, saved := total(rows); requests != 1 || saved != 400 {
		t.Errorf("after a hostile batch: %d requests, %d saved; want only the legitimate 1 / 400", requests, saved)
	}
}

func TestStoreStaysBoundedUnderRandomHostileBatches(t *testing.T) {
	s, kv := newStore(base)
	s.retention = 72 * time.Hour
	s.rowsPerHour = 200
	s.policyMarkers = 50
	s.hourDocBytes = 64 << 10
	ctx := context.Background()
	rng := rand.New(rand.NewSource(49))
	now := base

	for round := 0; round < 300; round++ {
		now = now.Add(time.Duration(rng.Intn(90)) * time.Minute)
		s.now = func() time.Time { return now }
		cur := HourOf(now)
		var deltas []*Delta
		for i := 0; i < 50; i++ {
			var hour int64
			switch rng.Intn(4) {
			case 0:
				hour = cur + int64(rng.Intn(1_000_000)) // far future
			case 1:
				hour = cur - int64(rng.Intn(1_000_000)) - 1 // long past
				if hour <= 0 {
					hour = 1
				}
			default:
				hour = cur - int64(rng.Intn(72))
			}
			d := deltaAt(hour, uint32(rng.Uint32()%2000), hexOf(rng.Intn(1_000_000)))
			// Fresh strategy and currency names on every delta
			d.Strategies = map[string]StrategyStats{}
			for j := 0; j < 1+rng.Intn(MaxStrategiesPerRow); j++ {
				d.Strategies[randomName(rng, "abcdefghijklmnopqrstuvwxyz0123456789_", 1+rng.Intn(40))] = StrategyStats{Leaves: 1, BytesSaved: 10}
			}
			d.CostSaved = map[string]float64{}
			for j := 0; j < 1+rng.Intn(MaxCurrenciesPerRow); j++ {
				d.CostSaved[randomName(rng, "ABCDEFGHIJKLMNOPQRSTUVWXYZ", 3)] = 0.01
			}
			deltas = append(deltas, d)
		}
		edge := fmt.Sprintf("edge-%d", rng.Intn(5))
		if err := s.Apply(ctx, "", edge, batchOf(deltas...)); err != nil {
			t.Fatal(err)
		}
	}

	maxHours := int(s.retention/time.Hour) + 2
	for e := 0; e < 5; e++ {
		edge := EdgeKey("", fmt.Sprintf("edge-%d", e))
		var held []int64
		_, _ = s.read(ctx, hoursKey(edge), &held)
		if len(held) > maxHours {
			t.Errorf("edge %d indexes %d hours, retention allows %d", e, len(held), maxHours)
		}
		for _, h := range held {
			if h > HourOf(now)+1 {
				t.Errorf("edge %d indexes future hour %d", e, h)
			}
			doc := hourDoc{}
			_, _ = s.read(ctx, hourKey(edge, h), &doc)
			if len(doc.Rows) > s.rowsPerHour+len(validOutcomes) {
				t.Errorf("hour %d holds %d rows", h, len(doc.Rows))
			}
			for _, d := range doc.Rows {
				if len(d.Strategies) > MaxStrategiesPerRow || len(d.CostSaved) > MaxCurrenciesPerRow {
					t.Errorf("hour %d row holds %d strategies and %d currencies", h, len(d.Strategies), len(d.CostSaved))
				}
			}
			raw, _ := kv.Read(ctx, hourKey(edge, h))
			// The budget, plus one batch's "other" names in existing rows
			if limit := s.hourDocBytes + (s.rowsPerHour+len(validOutcomes))*200; len(raw) > limit {
				t.Errorf("hour %d document is %d bytes, bound %d", h, len(raw), limit)
			}
		}
		if docs := kv.keys("pc:e:" + edge + ":h:"); len(docs) > len(held) {
			t.Errorf("edge %d has %d hour documents for %d indexed hours", e, len(docs), len(held))
		}
	}
	doc := policiesDoc{}
	_, _ = s.read(ctx, policiesKey, &doc)
	if len(doc.Policies) > s.policyMarkers {
		t.Errorf("%d policy markers, cap %d", len(doc.Policies), s.policyMarkers)
	}
	edges := edgesDoc{}
	_, _ = s.read(ctx, "pc:edges", &edges)
	if len(edges.Edges) > 5 {
		t.Errorf("%d edges indexed", len(edges.Edges))
	}
}

func randomName(rng *rand.Rand, alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rng.Intn(len(alphabet))]
	}
	return string(b)
}

// A row merging fresh strategy and currency names from many batches keeps
// at most MaxStrategiesPerRow and MaxCurrenciesPerRow, the rest under
// "other" and "XXX", with nothing lost from the totals.
func TestRowMapsAreBounded(t *testing.T) {
	s, _ := newStore(base)
	ctx := context.Background()
	hour := HourOf(base)
	rng := rand.New(rand.NewSource(1))
	var leaves int64
	var cost float64
	for i := 0; i < 50; i++ {
		d := deltaAt(hour, 1, hashA)
		d.Strategies = map[string]StrategyStats{}
		for j := 0; j < 32; j++ {
			d.Strategies[randomName(rng, "abcdefghij", 12)] = StrategyStats{Leaves: 1}
			leaves++
		}
		d.CostSaved = map[string]float64{randomName(rng, "ABCDEFGHIJKLMNOPQRSTUVWXYZ", 3): 1}
		cost++
		if err := s.Apply(ctx, "", "e", batchOf(d)); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, _ := s.Query(ctx, hour, hour)
	if len(rows) != 1 {
		t.Fatalf("%d rows", len(rows))
	}
	r := rows[0]
	var gotLeaves int64
	for _, st := range r.Strategies {
		gotLeaves += st.Leaves
	}
	var gotCost float64
	for _, v := range r.CostSaved {
		gotCost += v
	}
	if len(r.Strategies) > MaxStrategiesPerRow || r.Strategies[OtherStrategy].Leaves == 0 || gotLeaves != leaves {
		t.Errorf("%d strategies, other %d, leaves %d of %d", len(r.Strategies), r.Strategies[OtherStrategy].Leaves, gotLeaves, leaves)
	}
	if len(r.CostSaved) > MaxCurrenciesPerRow || r.CostSaved[OtherCurrency] == 0 || gotCost != cost {
		t.Errorf("%d currencies, other %v, cost %v of %v", len(r.CostSaved), r.CostSaved[OtherCurrency], gotCost, cost)
	}

	// Names outside the allowed shapes are refused by Validate
	bad := batchOf(deltaAt(hour, 1, hashA))
	bad.Deltas[0].Strategies = map[string]StrategyStats{"Not A Name": {Leaves: 1}}
	if bad.Validate() == nil {
		t.Error("a strategy name with spaces was accepted")
	}
	bad = batchOf(deltaAt(hour, 1, hashA))
	bad.Deltas[0].CostSaved = map[string]float64{"usd": 1}
	if bad.Validate() == nil {
		t.Error("a lower-case currency was accepted")
	}
}

// Once an hour document reaches its byte budget it takes no new rows or
// names, and the totals keep adding up.
func TestHourDocumentByteBudget(t *testing.T) {
	s, kv := newStore(base)
	s.hourDocBytes = 32 << 10
	ctx := context.Background()
	hour := HourOf(base)
	var want int64
	for i := 0; i < 400; i++ {
		d := deltaAt(hour, uint32(i+1), hashA)
		want += d.Requests
		if err := s.Apply(ctx, "", "e", batchOf(d)); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := kv.Read(ctx, hourKey(EdgeKey("", "e"), hour))
	// Past the budget only the bounded batch-id list (35 bytes an id) and
	// the overflow row still grow
	doc := hourDoc{}
	_ = json.Unmarshal(raw, &doc)
	if limit := s.hourDocBytes + len(doc.Batches)*36 + 8<<10; len(raw) > limit {
		t.Errorf("hour document is %d bytes with a %d budget (bound %d)", len(raw), s.hourDocBytes, limit)
	}
	if len(doc.Rows) >= 400 {
		t.Errorf("%d rows: no row was folded at the budget", len(doc.Rows))
	}
	if s.Stats().Folded == 0 {
		t.Error("nothing folded at the byte budget")
	}
	rows, _, _ := s.Query(ctx, hour, hour)
	if requests, _ := total(rows); requests != want {
		t.Errorf("%d requests kept of %d", requests, want)
	}
}
