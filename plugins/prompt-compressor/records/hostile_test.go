package records

import (
	"context"
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
			deltas = append(deltas, deltaAt(hour, uint32(rng.Uint32()), hexOf(rng.Intn(1_000_000))))
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
