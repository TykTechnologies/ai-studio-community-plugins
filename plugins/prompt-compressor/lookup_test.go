package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
)

// A fetch that hangs costs the caller at most its wait, and later callers
// share the one fetch instead of starting more.
func TestLookupHungFetchIsBounded(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	c := newAsyncLookup(time.Minute, time.Second, 8, func(k int) (string, error) {
		calls.Add(1)
		<-release
		return "v", nil
	})
	start := time.Now()
	for i := 0; i < 5; i++ {
		if _, ok := c.getWait(1, 10*time.Millisecond); ok {
			t.Fatal("a value before the fetch returned")
		}
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("five lookups took %v", d)
	}
	close(release)
	c.inflight.Wait()
	if v, ok := c.get(1); !ok || v != "v" {
		t.Errorf("after the fetch got %q %v", v, ok)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("%d fetches, want 1", n)
	}
}

// A fast fetch is seen by the first caller.
func TestLookupWaitSeesFastFetch(t *testing.T) {
	c := newAsyncLookup(time.Minute, time.Second, 8, func(k int) (int, error) { return k * 2, nil })
	if v, ok := c.getWait(21, time.Second); !ok || v != 42 {
		t.Errorf("got %d %v", v, ok)
	}
}

// A stale value is served while it is refreshed; a failed fetch is retried
// only after errTTL, and a panic is a failure.
func TestLookupStaleAndRetry(t *testing.T) {
	now := time.Unix(1000, 0)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }

	var calls atomic.Int32
	var fail atomic.Int32 // 1 error, 2 panic
	c := newAsyncLookup(time.Minute, 10*time.Second, 8, func(k int) (int, error) {
		n := calls.Add(1)
		switch fail.Load() {
		case 1:
			return 0, errors.New("broker down")
		case 2:
			panic("boom")
		}
		return int(n), nil
	})
	c.now = clock

	c.get(1)
	c.inflight.Wait()
	if v, ok := c.get(1); !ok || v != 1 {
		t.Fatalf("got %d %v", v, ok)
	}

	advance(2 * time.Minute)
	fail.Store(1)
	if v, ok := c.get(1); !ok || v != 1 {
		t.Errorf("stale value not served: %d %v", v, ok)
	}
	c.inflight.Wait()
	if v, ok := c.get(1); !ok || v != 1 {
		t.Errorf("a failed refresh dropped the value: %d %v", v, ok)
	}
	c.inflight.Wait()
	if n := calls.Load(); n != 2 {
		t.Errorf("%d fetches before errTTL, want 2", n)
	}

	fail.Store(2)
	advance(11 * time.Second)
	c.get(1)
	c.inflight.Wait()
	if n := calls.Load(); n != 3 {
		t.Errorf("%d fetches after errTTL, want 3", n)
	}

	fail.Store(0)
	advance(11 * time.Second)
	c.get(1)
	c.inflight.Wait()
	if v, ok := c.get(1); !ok || v != 4 {
		t.Errorf("after recovery got %d %v", v, ok)
	}
}

// The cache stays bounded.
func TestLookupBounded(t *testing.T) {
	c := newAsyncLookup(time.Minute, time.Second, 4, func(k int) (int, error) { return k, nil })
	for i := 0; i < 100; i++ {
		c.get(i)
		c.inflight.Wait()
	}
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n > 4 {
		t.Errorf("%d entries, max 4", n)
	}
}

// hangingGateway's GetLLM and GetApp block until the test ends, like a
// broker whose dial never completes.
type hangingGateway struct {
	*llmGateway
	release chan struct{}
}

func (g *hangingGateway) GetLLM(ctx context.Context, llmID uint32) (interface{}, error) {
	<-g.release
	return nil, errors.New("timeout waiting for connection info")
}

func (g *hangingGateway) GetApp(ctx context.Context, appID uint32) (interface{}, error) {
	<-g.release
	return nil, errors.New("timeout waiting for connection info")
}

// A host that never answers costs a request the lookup wait, not the
// broker's retries. Without an App the request is still compressed, by the
// vendor's format and the plugin's own policy; an App's request is
// forwarded unchanged, since the App may have opted out.
func TestPostAuthDoesNotWaitForAHungHost(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"enforce"}`)
	gw := &hangingGateway{llmGateway: ctx.Services.(*llmBroker).gw, release: make(chan struct{})}
	t.Cleanup(func() { close(gw.release); p.llms.inflight.Wait(); p.appMeta.inflight.Wait() })
	ctx.Services = &hangingBroker{llmBroker: ctx.Services.(*llmBroker), gw: gw}
	p.mu.Lock()
	p.services = ctx.Services
	p.mu.Unlock()

	// The first request also loads the tokenizer, which takes about a
	// second under -race; the later ones show what the hung lookups cost
	// (the broker's own retries take 25 s, a hung fetch forever).
	for i, tc := range []struct {
		app      uint32
		limit    time.Duration
		modified bool
	}{
		{0, 10 * time.Second, true},
		{0, 2 * time.Second, true},
		{1, 2 * time.Second, false},
	} {
		ctx.RequestID, ctx.AppID = fmt.Sprintf("req-%d", i), tc.app
		body := noisyBody(t)
		start := time.Now()
		resp, err := p.HandlePostAuth(ctx, request(body, nil))
		if d := time.Since(start); d > tc.limit {
			t.Errorf("request %d took %v with a hung host", i, d)
		}
		if err != nil || resp.Modified != tc.modified {
			t.Errorf("request %d: resp %+v, err %v", i, resp, err)
		}
		rec, _ := p.pending.take(ctx.RequestID)
		if !tc.modified && (rec.Outcome != "skipped" || rec.Report.Reason != ReasonAppPolicyUnknown) {
			t.Errorf("request %d: record %+v", i, rec)
		}
	}
}

type hangingBroker struct {
	*llmBroker
	gw *hangingGateway
}

func (b *hangingBroker) Gateway() plugin_sdk.GatewayServices { return b.gw }
