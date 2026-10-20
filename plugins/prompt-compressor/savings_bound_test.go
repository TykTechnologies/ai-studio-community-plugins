package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
)

// eac.61: a dashboard query is bounded and shared.

func TestBucketForRange(t *testing.T) {
	for hours, want := range map[int]int64{24: 1, 72: 1, 168: 6, 720: 6, 2160: 24} {
		if got := bucketFor(hours); got != want {
			t.Errorf("bucketFor(%d) = %d, want %d", hours, got, want)
		}
	}
}

func TestSavingsCacheSharesOneQuery(t *testing.T) {
	var c savingsCache
	var calls atomic.Int32
	release := make(chan struct{})
	query := func() ([]byte, error) {
		calls.Add(1)
		<-release
		return []byte(`{"ok":true}`), nil
	}
	var wg sync.WaitGroup
	results := make([][]byte, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = c.do("24/0/0", query)
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Errorf("%d queries for 8 concurrent viewers, want 1", calls.Load())
	}
	for _, r := range results {
		if string(r) != `{"ok":true}` {
			t.Errorf("a viewer got %q", r)
		}
	}

	// Within the TTL the answer comes from memory; after it, a new query
	now := time.Now()
	c.now = func() time.Time { return now }
	_, _ = c.do("24/0/0", query)
	c.now = func() time.Time { return now.Add(savingsCacheTTL + time.Second) }
	_, _ = c.do("24/0/0", func() ([]byte, error) { calls.Add(1); return []byte(`{}`), nil })
	if calls.Load() != 2 {
		t.Errorf("%d queries, want 2 (one cached answer, one after the TTL)", calls.Load())
	}

	// An error is not cached
	_, err := c.do("err", func() ([]byte, error) { return nil, errors.New("kv down") })
	if err == nil {
		t.Fatal("error swallowed")
	}
	if _, err := c.do("err", func() ([]byte, error) { return []byte(`{}`), nil }); err != nil {
		t.Error("an error was cached")
	}
}

// A 7-day view is bucketed by 6 hours, and still finds a cache drop at a
// policy change by reading the hours around it.
func TestBucketedViewKeepsTheCacheDropAlert(t *testing.T) {
	kv := &memKV{data: map[string][]byte{}}
	studio := studioPlugin(t, kv)
	studio.testStudio = &fakeStudio{}
	store := studio.telemetry.store
	ctx := context.Background()

	now := time.Now()
	change := records.HourOf(now) - 30
	apply := func(hour int64, hash string, cached int64) {
		agg := records.NewAggregator(nil)
		r := savingsRecord(1, 1000, 600, records.OutcomeApplied)
		r.At = time.Unix(hour*3600+60, 0)
		r.PolicyHash, r.InputTokens, r.CacheReadTokens = hash, 1000, cached
		agg.Add(r)
		for _, b := range agg.Flush() {
			if err := store.Apply(ctx, "", "edge-1", b); err != nil {
				t.Fatal(err)
			}
		}
	}
	oldHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for h := change - 8; h < change; h++ {
		apply(h, oldHash, 700)
	}
	for h := change; h <= change+2; h++ {
		apply(h, newHash, 100)
	}

	out, err := studio.HandleRPC("getSavings", []byte(`{"range_hours": 168}`))
	if err != nil {
		t.Fatal(err)
	}
	var v savingsView
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	if v.BucketHours != 6 {
		t.Errorf("bucket %d h, want 6", v.BucketHours)
	}
	for _, p := range v.Series {
		if p.Hour%6 != 0 {
			t.Errorf("series point %d not on a 6-hour bucket", p.Hour)
		}
	}
	if v.Totals.Requests != 11 || v.Totals.TokensSaved != 11*400 {
		t.Errorf("totals %d requests / %d saved, want 11 / %d", v.Totals.Requests, v.Totals.TokensSaved, 11*400)
	}
	if len(v.Alerts) != 1 || v.Alerts[0].Hour != change {
		t.Errorf("alerts = %+v, want the drop at %d", v.Alerts, change)
	}
	if v.Partial {
		t.Error("a small view reported partial")
	}
}
