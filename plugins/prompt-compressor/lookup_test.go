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
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// hangGuard is how long a call that must not wait on a hung host may take
// before the test calls it a hang. The hung host only answers after the
// call returns, so returning at all is the assertion; the guard only stops
// a broken build from hanging the suite.
const hangGuard = 5 * time.Second

// returns runs fn and fails the test if it has not returned within
// hangGuard.
func returns(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(hangGuard):
		t.Fatalf("%s waited for a hung host", what)
	}
}

// A fetch that hangs does not hold the caller, and later callers share the
// one fetch instead of starting more.
func TestLookupHungFetchIsBounded(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	c := newAsyncLookup(time.Minute, time.Second, 8, func(k int) (string, error) {
		calls.Add(1)
		<-release
		return "v", nil
	})
	returns(t, "getWait", func() {
		for i := 0; i < 5; i++ {
			if _, ok := c.getWait(1, 10*time.Millisecond); ok {
				t.Error("a value before the fetch returned")
			}
		}
	})
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

// A host that never answers (it is released only after the test) costs a
// request the lookup wait, not the broker's retries. Without an App the
// request is still compressed, by the vendor's format and the plugin's own
// policy; an App's request is forwarded unchanged, since the App may have
// opted out.
func TestPostAuthDoesNotWaitForAHungHost(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"enforce"}`)
	gw := &hangingGateway{llmGateway: ctx.Services.(*llmBroker).gw, release: make(chan struct{})}
	t.Cleanup(func() { close(gw.release); p.llms.inflight.Wait(); p.appMeta.inflight.Wait() })
	ctx.Services = &hangingBroker{llmBroker: ctx.Services.(*llmBroker), gw: gw}
	p.mu.Lock()
	p.services = ctx.Services
	p.mu.Unlock()

	for i, tc := range []struct {
		app      uint32
		modified bool
	}{
		{0, true},
		{0, true},
		{1, false},
	} {
		ctx.RequestID, ctx.AppID = fmt.Sprintf("req-%d", i), tc.app
		body := noisyBody(t)
		var resp *pb.PluginResponse
		var err error
		returns(t, fmt.Sprintf("request %d", i), func() { resp, err = p.HandlePostAuth(ctx, request(body, nil)) })
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
