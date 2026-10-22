package main

import (
	"context"
	"testing"
	"time"
)

func concurrentRule() []Rule {
	return []Rule{{
		ID: "conc", Name: "concurrent", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "concurrent", Value: 2}, Action: "enforce", Enabled: true,
	}}
}

func concurrentCount(p *RateLimiterPlugin) int {
	return ReadConcurrentState(context.Background(), p.store, ConcurrentKey("conc", "app_id:1")).Count
}

// A request answered before the upstream (a later plugin's block, a cache
// hit, a request filter) never reaches the response hooks. Its slot is given
// back once it is older than inflightMaxAge.
func TestSweepReleasesSlotsOfRequestsThatNeverCompleted(t *testing.T) {
	p := newTestPlugin(concurrentRule())
	ctx := testCtx()

	for _, id := range []string{"lost-1", "lost-2"} {
		if resp, _ := p.HandlePostAuth(ctx, testEnrichedRequest(1, 0, 0, "", id)); resp.Block {
			t.Fatalf("%s blocked", id)
		}
	}
	if resp, _ := p.HandlePostAuth(ctx, testEnrichedRequest(1, 0, 0, "", "third")); !resp.Block {
		t.Fatal("expected the third request to hit the concurrency limit")
	}

	// Within inflightMaxAge nothing is released.
	p.sweepSlots(context.Background(), time.Now().Add(inflightMaxAge/2))
	if got := concurrentCount(p); got != 2 {
		t.Fatalf("concurrent = %d before inflightMaxAge, want 2", got)
	}

	// Past it, the slots come back and requests pass again.
	p.sweepSlots(context.Background(), time.Now().Add(inflightMaxAge+time.Minute))
	if got := concurrentCount(p); got != 0 {
		t.Fatalf("concurrent = %d after the sweep, want 0", got)
	}
	if resp, _ := p.HandlePostAuth(ctx, testEnrichedRequest(1, 0, 0, "", "after")); resp.Block {
		t.Error("request blocked after the leaked slots were released")
	}

	// A late response for a swept request gives nothing back twice.
	p.OnBeforeWrite(ctx, testResponseWriteRequest("lost-1", []byte(`{}`), false))
	if got := concurrentCount(p); got != 1 {
		t.Errorf("concurrent = %d after a late response, want 1 (the 'after' request)", got)
	}
}

// The sweep runs at most once per sweepInterval.
func TestSweepIsRateLimited(t *testing.T) {
	p := newTestPlugin(concurrentRule())
	now := time.Now()
	p.sweepSlots(context.Background(), now)
	p.trackSlots("old", []string{ConcurrentKey("conc", "app_id:1")}, now.Add(-2*inflightMaxAge))

	p.sweepSlots(context.Background(), now.Add(sweepInterval/2))
	if _, ok := p.takeSlots("old"); !ok {
		t.Fatal("swept again within sweepInterval")
	}
}

// A slot is released once whether the response hooks or the sweep get there
// first, and a response hook for a request whose record a restart lost
// still releases its slot from the request state.
func TestResponseReleasesSlotOnce(t *testing.T) {
	p := newTestPlugin(concurrentRule())
	ctx := testCtx()

	if resp, _ := p.HandlePostAuth(ctx, testEnrichedRequest(1, 0, 0, "", "r1")); resp.Block {
		t.Fatal("blocked")
	}
	p.OnBeforeWrite(ctx, testResponseWriteRequest("r1", []byte(`{}`), false))
	p.sweepSlots(context.Background(), time.Now().Add(inflightMaxAge+time.Minute))
	if got := concurrentCount(p); got != 0 {
		t.Fatalf("concurrent = %d, want 0", got)
	}

	// Restart: the in-memory record is gone, the request state is not.
	if resp, _ := p.HandlePostAuth(ctx, testEnrichedRequest(1, 0, 0, "", "r2")); resp.Block {
		t.Fatal("blocked")
	}
	p.inflightMu.Lock()
	p.inflight = nil
	p.inflightMu.Unlock()
	p.OnBeforeWrite(ctx, testResponseWriteRequest("r2", []byte(`{}`), false))
	if got := concurrentCount(p); got != 0 {
		t.Errorf("concurrent = %d after a response following a restart, want 0", got)
	}
}
