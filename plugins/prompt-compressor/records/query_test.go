package records

import (
	"context"
	"testing"
	"time"
)

// eac.61: a dashboard query is bounded in what it reads and holds.
func TestQueryWithBucketsBoundsAndFilters(t *testing.T) {
	now := base.Add(48 * time.Hour)
	s, _ := newStore(now)
	ctx := context.Background()
	// 48 hours, two Apps, one row each per hour
	for h := 0; h < 48; h++ {
		at := base.Add(time.Duration(h) * time.Hour)
		agg := NewAggregator(nil)
		agg.Add(req(at, 1, 1, hashA, OutcomeApplied, 100, 60))
		agg.Add(req(at, 2, 1, hashA, OutcomeApplied, 100, 60))
		for _, b := range agg.Flush() {
			if err := s.Apply(ctx, "", "e", b); err != nil {
				t.Fatal(err)
			}
		}
	}
	from, to := HourOf(base), HourOf(base)+47

	// Daily buckets: at most 3 bucket times, totals unchanged
	res, err := s.QueryWith(ctx, QueryOptions{From: from, To: to, Bucket: 24})
	if err != nil {
		t.Fatal(err)
	}
	times := map[int64]bool{}
	var requests, saved int64
	for _, d := range res.Rows {
		times[d.Hour] = true
		requests += d.Requests
		saved += d.TokensSaved
		if d.Hour%24 != 0 {
			t.Errorf("bucket time %d is not aligned to 24 h", d.Hour)
		}
	}
	if len(times) > 3 || requests != 96 || saved != 96*40 || res.Partial {
		t.Errorf("daily: %d bucket times, %d requests, %d saved, partial %v", len(times), requests, saved, res.Partial)
	}

	// A filter keeps one App
	res, _ = s.QueryWith(ctx, QueryOptions{From: from, To: to, AppID: 2})
	if requests, _ = total(res.Rows); requests != 48 {
		t.Errorf("App 2: %d requests, want 48", requests)
	}

	// A byte cap stops the read, newest hours first, and says so
	res, _ = s.QueryWith(ctx, QueryOptions{From: from, To: to, MaxBytes: 1})
	if !res.Partial || res.ReadFrom != to {
		t.Errorf("byte cap: partial %v, read from %d (newest %d)", res.Partial, res.ReadFrom, to)
	}
	for _, d := range res.Rows {
		if d.Hour != to {
			t.Errorf("byte-capped read holds hour %d, want only the newest", d.Hour)
		}
	}

	// A row cap folds the rest into overflow rows, totals kept
	res, _ = s.QueryWith(ctx, QueryOptions{From: from, To: to, MaxRows: 10})
	requests, saved = total(res.Rows)
	overflow := 0
	for _, d := range res.Rows {
		if d.Overflow {
			overflow++
		}
	}
	if requests != 96 || saved != 96*40 || res.Folded == 0 || len(res.Rows)-overflow > 10 {
		t.Errorf("row cap: %d rows (%d overflow), %d requests, %d saved, folded %d", len(res.Rows), overflow, requests, saved, res.Folded)
	}
}
