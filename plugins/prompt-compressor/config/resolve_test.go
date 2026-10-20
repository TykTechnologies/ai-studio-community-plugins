package config

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppPolicyFromMetadata(t *testing.T) {
	for metadata, want := range map[string]string{
		``:                                    "",
		`not json`:                            "",
		`{}`:                                  "",
		`{"prompt_compressor.policy": "max"}`: "max",
		`{"prompt_compressor": {"policy": "off"}}`: "off",
		`{"prompt_compressor.policy": 3}`:          "",
		`{"prompt_compressor": "max"}`:             "",
		`{"other": 1, "prompt_compressor.policy": "safe", "prompt_compressor": {"policy": "max"}}`: "safe",
	} {
		if got := AppPolicyFromMetadata(metadata); got != want {
			t.Errorf("AppPolicyFromMetadata(%s) = %q, want %q", metadata, got, want)
		}
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func newTestCache(fetch AppMetadataFetcher) (*AppPolicyCache, *fakeClock) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	c := NewAppPolicyCache(fetch)
	c.now = clock.Now
	return c, clock
}

func TestAppPolicyCacheCachesForTheTTL(t *testing.T) {
	var calls atomic.Int32
	metadata := `{"prompt_compressor.policy": "max"}`
	c, clock := newTestCache(func(ctx context.Context, appID uint32) (string, error) {
		calls.Add(1)
		return metadata, nil
	})
	ctx := context.Background()

	if got := c.Policy(ctx, 7); got != "max" {
		t.Fatalf("Policy = %q", got)
	}
	c.Policy(ctx, 7)
	if calls.Load() != 1 {
		t.Errorf("%d fetches within the TTL, want 1", calls.Load())
	}
	metadata = `{"prompt_compressor.policy": "off"}`
	clock.Advance(DefaultAppPolicyTTL)
	if got := c.Policy(ctx, 7); got != "off" || calls.Load() != 2 {
		t.Errorf("after the TTL Policy = %q with %d fetches, want off with 2", got, calls.Load())
	}
	if got := c.Policy(ctx, 0); got != "" || calls.Load() != 2 {
		t.Error("App 0 was looked up")
	}
}

func TestAppPolicyCacheErrorFallsBackBriefly(t *testing.T) {
	var calls atomic.Int32
	fail := true
	c, clock := newTestCache(func(ctx context.Context, appID uint32) (string, error) {
		calls.Add(1)
		if fail {
			return "", errors.New("rpc error: code = PermissionDenied")
		}
		return `{"prompt_compressor.policy": "max"}`, nil
	})
	ctx := context.Background()

	if got := c.Policy(ctx, 7); got != "" {
		t.Fatalf("a failed fetch gave %q, want no override", got)
	}
	c.Policy(ctx, 7)
	if calls.Load() != 1 {
		t.Errorf("failed fetch retried within the error TTL (%d calls)", calls.Load())
	}
	fail = false
	clock.Advance(DefaultAppPolicyErrorTTL)
	if got := c.Policy(ctx, 7); got != "max" {
		t.Errorf("after the error TTL Policy = %q, want max", got)
	}
}

func TestAppPolicyCacheIsBounded(t *testing.T) {
	c, clock := newTestCache(func(ctx context.Context, appID uint32) (string, error) { return `{}`, nil })
	c.max = 100
	ctx := context.Background()
	for id := uint32(1); id <= 1000; id++ {
		c.Policy(ctx, id)
		if id%250 == 0 {
			clock.Advance(time.Second)
		}
		if c.Len() > 100 {
			t.Fatalf("cache holds %d Apps, max 100", c.Len())
		}
	}
}

func TestAppPolicyCacheConcurrentUse(t *testing.T) {
	c := NewAppPolicyCache(func(ctx context.Context, appID uint32) (string, error) {
		return `{"prompt_compressor.policy": "max"}`, nil
	})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				if got := c.Policy(context.Background(), uint32(i%50+1)); got != "max" {
					t.Errorf("Policy = %q", got)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestNilAppPolicyCache(t *testing.T) {
	var c *AppPolicyCache
	if c.Policy(context.Background(), 1) != "" {
		t.Error("nil cache returned an override")
	}
}

// The App metadata override is resolved end to end: a GetApp error never
// fails the request, it falls back to the bindings.
func TestAppMetadataOverrideWithFetchErrors(t *testing.T) {
	cfg := mustParse(t, `{"policies": {"max": {"level": 3}}, "bindings": {"llm:claude": "max"}}`)
	cache, _ := newTestCache(func(ctx context.Context, appID uint32) (string, error) {
		switch appID {
		case 1:
			return `{"prompt_compressor.policy": "off"}`, nil
		case 2:
			return `{"prompt_compressor.policy": "no-such-policy"}`, nil
		default:
			return "", errors.New("unavailable")
		}
	})
	ctx := context.Background()
	for _, tc := range []struct {
		app        uint32
		wantPolicy string
		wantSource Source
	}{
		{1, "off", SourceAppMetadata},
		{2, "max", SourceLLMBinding},
		{3, "max", SourceLLMBinding},
	} {
		r := cfg.Resolve(Target{AppID: tc.app, LLMSlug: "claude"}, cache.Policy(ctx, tc.app))
		if r.Name != tc.wantPolicy || r.Source != tc.wantSource {
			t.Errorf("App %d: %s from %s, want %s from %s", tc.app, r.Name, r.Source, tc.wantPolicy, tc.wantSource)
		}
	}
}
