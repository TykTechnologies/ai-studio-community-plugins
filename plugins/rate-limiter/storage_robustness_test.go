package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var errKVUnavailable = status.Error(codes.Unavailable, "kv unavailable")

// An unreadable store is a storage error, not "no usage": fail_open decides.
func TestPostAuth_UnreadableStoreFollowsFailOpen(t *testing.T) {
	rule := Rule{
		ID: "r1", Name: "outage", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 10}, Action: "enforce", Enabled: true,
	}
	for _, failOpen := range []bool{true, false} {
		kv := newMemKV()
		kv.readErr = errKVUnavailable
		p := newTestPlugin(nil, func(c *Config) { c.FailOpen = failOpen })
		p.store = newKVStore(kv)
		p.rulesCache = &RuleSet{Rules: []Rule{rule}}
		p.rulesTTL = time.Now().Add(time.Hour)

		resp, err := p.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", "req-outage"))
		if err != nil {
			t.Fatal(err)
		}
		if failOpen && (resp.Block || resp.Headers["X-RateLimit-Error"] != "storage_unavailable") {
			t.Errorf("fail_open=true: block=%v headers=%v, want allowed with X-RateLimit-Error", resp.Block, resp.Headers)
		}
		if !failOpen && (!resp.Block || resp.StatusCode != 503) {
			t.Errorf("fail_open=false: block=%v status=%d, want 503", resp.Block, resp.StatusCode)
		}
	}
}

// A rules read that fails is not an empty rule set: enforcement keeps the
// cached rules and an edit is refused instead of saving over unread rules.
func TestUnreadableRulesAreNotEmptyRules(t *testing.T) {
	kv := newMemKV()
	p := newTestPlugin(nil)
	p.store = newKVStore(kv)
	seed := []Rule{{ID: "r0", Name: "seed", Limit: Limit{Type: "requests", Value: 5}}}
	if err := p.saveRuleSetToKV(&RuleSet{Rules: seed}); err != nil {
		t.Fatal(err)
	}
	if got := p.loadRulesCached(); len(got) != 1 {
		t.Fatalf("cached rules = %v", got)
	}

	kv.readErr = errKVUnavailable
	p.invalidateRulesCache()
	if got := p.loadRulesCached(); len(got) != 1 {
		t.Errorf("rules during a KV outage = %v, want the cached rule kept", got)
	}
	if _, err := p.loadRuleSetFromKV(); err == nil {
		t.Error("loadRuleSetFromKV hid the read error")
	}
	if err := p.saveRuleSetToKV(&RuleSet{Version: 1, Rules: nil}); err == nil {
		t.Error("saveRuleSetToKV wrote without reading the current version")
	}
}

// memKV is a concurrency-safe in-memory plugin_sdk.KVService. Missing keys
// fail with gRPC NotFound, like the AI Studio and Microgateway KV services;
// readErr, when set, fails every read (an outage or a denied scope).
type memKV struct {
	mu      sync.Mutex
	data    map[string][]byte
	readErr error
}

func newMemKV() *memKV { return &memKV{data: map[string][]byte{}} }

func (m *memKV) Read(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readErr != nil {
		return nil, m.readErr
	}
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("failed to read KV data: %w", status.Errorf(codes.NotFound, "key not found"))
	}
	return v, nil
}

func (m *memKV) Write(_ context.Context, key string, value []byte, _ *time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return true, nil
}

func (m *memKV) WriteWithTTL(ctx context.Context, key string, value []byte, _ time.Duration) (bool, error) {
	return m.Write(ctx, key, value, nil)
}

func (m *memKV) Delete(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return true, nil
}

func (m *memKV) List(context.Context, string) ([]string, error) { return nil, nil }

func TestShutdownClosesRedisClient(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	p := newTestPlugin(nil)
	p.store = &redisStore{client: client}

	if err := p.Shutdown(testCtx()); err != nil {
		t.Fatal(err)
	}
	if err := client.Ping(context.Background()).Err(); !errors.Is(err, redis.ErrClosed) {
		t.Errorf("client after Shutdown: %v, want redis.ErrClosed", err)
	}
	// A second Shutdown is harmless
	if err := p.Shutdown(testCtx()); err != nil {
		t.Fatal(err)
	}
}

func TestKVStore_CorruptCounterIsAStorageError(t *testing.T) {
	kv := newMemKV()
	store := newKVStore(kv)
	ctx := context.Background()
	kv.Write(ctx, "rl:w:r1:g:60", []byte("{not json"), nil)
	kv.Write(ctx, "rl:c:r1:g", []byte("{not json"), nil)

	_, err := store.EvalSlidingWindow(ctx, "rl:w:r1:g:60", "rl:w:r1:g:0", 60, 90, 1, time.Minute)
	assertCorrupt(t, "EvalSlidingWindow", err, "rl:w:r1:g:60")
	// A corrupt counter is not overwritten as if it were zero
	if v, _ := kv.Read(ctx, "rl:w:r1:g:60"); string(v) != "{not json" {
		t.Errorf("corrupt counter was overwritten: %q", v)
	}

	_, _, err = store.IncrementIfBelow(ctx, "rl:c:r1:g", 5, time.Minute)
	assertCorrupt(t, "IncrementIfBelow", err, "rl:c:r1:g")
	_, err = store.DecrementCounter(ctx, "rl:c:r1:g", time.Minute)
	assertCorrupt(t, "DecrementCounter", err, "rl:c:r1:g")

	// Missing keys still read as zero
	if n, err := store.EvalSlidingWindow(ctx, "rl:w:r2:g:60", "rl:w:r2:g:0", 60, 90, 1, time.Minute); err != nil || n != 0 {
		t.Errorf("missing counter: %d, %v", n, err)
	}
}

func assertCorrupt(t *testing.T, op string, err error, key string) {
	t.Helper()
	if !errors.Is(err, errCorruptCounter) || !strings.Contains(err.Error(), key) {
		t.Errorf("%s: err = %v, want errCorruptCounter naming %q", op, err, key)
	}
}

// A corrupt counter is handled like any storage error: fail_open decides.
func TestPostAuth_CorruptCounterFollowsFailOpen(t *testing.T) {
	rule := Rule{
		ID: "r1", Name: "corrupt", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 10}, Action: "enforce", Enabled: true,
	}
	for _, failOpen := range []bool{true, false} {
		kv := newMemKV()
		p := newTestPlugin(nil, func(c *Config) { c.FailOpen = failOpen })
		p.store = newKVStore(kv)
		p.rulesCache = &RuleSet{Rules: []Rule{rule}}
		p.rulesTTL = time.Now().Add(time.Hour)

		// Corrupt both buckets the request could read
		req := testEnrichedRequest(1, 0, 0, "", "req-corrupt")
		dimKey, ok := ResolveKey(rule, req.Request.Context, req)
		if !ok {
			t.Fatal("rule does not resolve for the request")
		}
		now := time.Now()
		for _, epoch := range []int64{BucketEpoch(now, 60), PreviousBucketEpoch(now, 60)} {
			kv.Write(context.Background(), WindowKey(rule.ID, dimKey, epoch), []byte("garbage"), nil)
		}

		resp, err := p.HandlePostAuth(testCtx(), req)
		if err != nil {
			t.Fatal(err)
		}
		if failOpen && (resp.Block || resp.Headers["X-RateLimit-Error"] != "storage_unavailable") {
			t.Errorf("fail_open=true: block=%v headers=%v, want allowed with X-RateLimit-Error", resp.Block, resp.Headers)
		}
		if !failOpen && (!resp.Block || resp.StatusCode != 503) {
			t.Errorf("fail_open=false: block=%v status=%d, want 503", resp.Block, resp.StatusCode)
		}
	}
}

// Two saves based on the same version: one wins, the other conflicts.
func TestSaveRuleSet_ConcurrentSavesOneWins(t *testing.T) {
	p := newTestPlugin(nil)
	p.store = newKVStore(newMemKV())
	if err := p.saveRuleSetToKV(&RuleSet{Rules: []Rule{{ID: "r0", Name: "seed"}}}); err != nil {
		t.Fatal(err)
	}

	for round := 0; round < 50; round++ {
		var loaded [2]*RuleSet
		for i := range loaded {
			rs, err := p.loadRuleSetFromKV()
			if err != nil {
				t.Fatal(err)
			}
			rs.Rules = append(rs.Rules, Rule{ID: "new", Name: "writer"})
			loaded[i] = rs
		}

		var wg sync.WaitGroup
		errs := make([]error, 2)
		start := make(chan struct{})
		for i := range loaded {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = p.saveRuleSetToKV(loaded[i])
			}(i)
		}
		close(start)
		wg.Wait()

		wins, conflicts := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrVersionConflict):
				conflicts++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatalf("round %d: %d wins, %d conflicts; want 1 and 1", round, wins, conflicts)
		}
	}
}

func TestRuleStatsHasNoUsageField(t *testing.T) {
	out, _ := json.Marshal(RuleStatsResponse{RuleID: "r1"})
	if strings.Contains(string(out), "current_usage") {
		t.Errorf("rule stats still report a usage figure: %s", out)
	}
}
