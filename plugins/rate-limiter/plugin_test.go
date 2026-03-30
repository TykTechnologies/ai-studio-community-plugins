package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// --- Test helpers ---

// newTestPlugin creates a RateLimiterPlugin backed by an in-memory store
// and pre-loaded with the given rules.
func newTestPlugin(rules []Rule, opts ...func(*Config)) *RateLimiterPlugin {
	store := newMemStore()
	config := &Config{
		FailOpen:          true,
		StorageBackend:    "kv",
		WindowSizeSeconds: 60,
	}
	for _, o := range opts {
		o(config)
	}

	p := &RateLimiterPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(PluginName, PluginVersion, "test"),
		config:     config,
		store:      store,
		locks:      make(map[string]*sync.Mutex),
	}

	// Seed rules into KV
	if len(rules) > 0 {
		rs := RuleSet{Rules: rules, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
		data, _ := json.Marshal(rs)
		store.Set(context.Background(), rulesKVKey, data, 0)
	}

	return p
}

func testCtx() plugin_sdk.Context {
	return plugin_sdk.Context{
		Runtime: plugin_sdk.RuntimeStudio,
		Context: context.Background(),
	}
}

func testEnrichedRequest(appID, userID, llmID uint32, model, requestID string) *pb.EnrichedRequest {
	return &pb.EnrichedRequest{
		Request: &pb.PluginRequest{
			Context: &pb.PluginContext{
				AppId:     appID,
				UserId:    userID,
				LlmId:    llmID,
				LlmSlug:  model,
				RequestId: requestID,
			},
		},
		AuthClaims: map[string]string{},
	}
}

func testResponseWriteRequest(requestID string, body []byte, isStream bool) *pb.ResponseWriteRequest {
	return &pb.ResponseWriteRequest{
		Context: &pb.PluginContext{
			RequestId: requestID,
		},
		Body:          body,
		Headers:       map[string]string{},
		IsStreamChunk: isStream,
	}
}

func openAIResponse(promptTokens, completionTokens int) []byte {
	resp := map[string]interface{}{
		"id":      "chatcmpl-test",
		"choices": []interface{}{},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	}
	data, _ := json.Marshal(resp)
	return data
}

// --- PostAuth Tests ---

func TestPostAuth_NoRules_PassThrough(t *testing.T) {
	p := newTestPlugin(nil)
	ctx := testCtx()
	req := testEnrichedRequest(1, 1, 1, "gpt-4o", "req-1")

	resp, err := p.HandlePostAuth(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Block {
		t.Error("expected pass-through with no rules")
	}
}

func TestPostAuth_NilContext_PassThrough(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "test", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "requests", Value: 10}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()
	req := &pb.EnrichedRequest{
		Request: &pb.PluginRequest{Context: nil},
	}

	resp, err := p.HandlePostAuth(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Block {
		t.Error("expected pass-through with nil context")
	}
}

func TestPostAuth_DisabledRule_Skipped(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "disabled", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "requests", Value: 1}, Action: "enforce", Enabled: false,
	}})
	ctx := testCtx()

	// Even though limit is 1, disabled rules should not block
	for i := 0; i < 5; i++ {
		req := testEnrichedRequest(1, 0, 0, "", fmt.Sprintf("req-%d", i))
		resp, err := p.HandlePostAuth(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Block {
			t.Errorf("request %d blocked by disabled rule", i)
		}
	}
}

func TestPostAuth_MissingDimension_Skipped(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "user-limit", Dimensions: []string{"user_id"},
		Limit: Limit{Type: "requests", Value: 1}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	// user_id is 0 → rule should be skipped, not block
	for i := 0; i < 5; i++ {
		req := testEnrichedRequest(1, 0, 0, "gpt-4o", fmt.Sprintf("req-%d", i))
		resp, err := p.HandlePostAuth(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Block {
			t.Errorf("request %d blocked by rule with missing dimension", i)
		}
	}
}

func TestPostAuth_RequestRate_BlocksOnBreach(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "app-rpm", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "requests", Value: 3}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	// First 3 requests should pass
	for i := 0; i < 3; i++ {
		req := testEnrichedRequest(42, 0, 0, "gpt-4o", fmt.Sprintf("req-%d", i))
		resp, err := p.HandlePostAuth(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Block {
			t.Errorf("request %d should not be blocked (limit=3)", i)
		}
	}

	// 4th request should be blocked
	req := testEnrichedRequest(42, 0, 0, "gpt-4o", "req-3")
	resp, err := p.HandlePostAuth(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Block {
		t.Error("4th request should be blocked")
	}
	if resp.StatusCode != 429 {
		t.Errorf("expected 429, got %d", resp.StatusCode)
	}
	if resp.Headers["Retry-After"] == "" {
		t.Error("expected Retry-After header on 429")
	}
}

func TestPostAuth_RequestRate_DifferentKeys_Independent(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "per-app", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "requests", Value: 2}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	// App 1: 2 requests OK
	for i := 0; i < 2; i++ {
		req := testEnrichedRequest(1, 0, 0, "", fmt.Sprintf("app1-req-%d", i))
		resp, _ := p.HandlePostAuth(ctx, req)
		if resp.Block {
			t.Errorf("app1 request %d should not be blocked", i)
		}
	}

	// App 2: should have its own counter
	req := testEnrichedRequest(2, 0, 0, "", "app2-req-0")
	resp, _ := p.HandlePostAuth(ctx, req)
	if resp.Block {
		t.Error("app2 first request should not be blocked")
	}

	// App 1: 3rd request blocked
	req = testEnrichedRequest(1, 0, 0, "", "app1-req-2")
	resp, _ = p.HandlePostAuth(ctx, req)
	if !resp.Block {
		t.Error("app1 3rd request should be blocked")
	}
}

func TestPostAuth_TokenRate_BlocksOnBreach(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "token-limit", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "tokens", Value: 500}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	// Manually seed token counts into the window bucket to simulate prior usage
	now := time.Now()
	epoch := BucketEpoch(now, 60)
	key := WindowKey("r1", "app_id:10", epoch)
	WriteWindowState(context.Background(), p.store, key, WindowState{Count: 500, UpdatedAt: now.Unix()}, 2*time.Minute)

	// Invalidate cache so plugin reads fresh rules
	p.invalidateRulesCache()

	req := testEnrichedRequest(10, 0, 0, "gpt-4o", "req-token-1")
	resp, err := p.HandlePostAuth(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Block {
		t.Error("expected block when token count already at limit")
	}
}

func TestPostAuth_Concurrent_BlocksOnBreach(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "conc-limit", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "concurrent", Value: 2}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	// First 2 requests pass (concurrent = 1 then 2)
	for i := 0; i < 2; i++ {
		req := testEnrichedRequest(5, 0, 0, "", fmt.Sprintf("req-c-%d", i))
		resp, _ := p.HandlePostAuth(ctx, req)
		if resp.Block {
			t.Errorf("request %d should not be blocked (concurrent limit=2)", i)
		}
	}

	// 3rd should be blocked (concurrent = 2, at limit)
	req := testEnrichedRequest(5, 0, 0, "", "req-c-2")
	resp, _ := p.HandlePostAuth(ctx, req)
	if !resp.Block {
		t.Error("3rd concurrent request should be blocked")
	}
}

func TestPostAuth_ShadowMode_LogsButDoesNotBlock(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "shadow-rule", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "requests", Value: 1}, Action: "log", Enabled: true,
	}})
	ctx := testCtx()

	// First request OK
	req := testEnrichedRequest(1, 0, 0, "", "req-s-0")
	resp, _ := p.HandlePostAuth(ctx, req)
	if resp.Block {
		t.Error("first request should not be blocked")
	}

	// Second request breaches but should NOT block (shadow mode)
	req = testEnrichedRequest(1, 0, 0, "", "req-s-1")
	resp, _ = p.HandlePostAuth(ctx, req)
	if resp.Block {
		t.Error("shadow mode should not block")
	}
	// Should have shadow breach header
	if resp.Headers == nil || resp.Headers["X-RateLimit-Shadow-Breach"] != "shadow-rule" {
		t.Error("expected X-RateLimit-Shadow-Breach header")
	}
}

func TestPostAuth_MultipleRules_FirstEnforceBreachBlocks(t *testing.T) {
	p := newTestPlugin([]Rule{
		{
			ID: "r1", Name: "loose", Dimensions: []string{"app_id"}, Priority: 0,
			Limit: Limit{Type: "requests", Value: 100}, Action: "enforce", Enabled: true,
		},
		{
			ID: "r2", Name: "tight", Dimensions: []string{"app_id"}, Priority: 1,
			Limit: Limit{Type: "requests", Value: 2}, Action: "enforce", Enabled: true,
		},
	})
	ctx := testCtx()

	// 2 requests OK
	for i := 0; i < 2; i++ {
		req := testEnrichedRequest(1, 0, 0, "", fmt.Sprintf("req-m-%d", i))
		resp, _ := p.HandlePostAuth(ctx, req)
		if resp.Block {
			t.Errorf("request %d should pass", i)
		}
	}

	// 3rd blocked by the tight rule
	req := testEnrichedRequest(1, 0, 0, "", "req-m-2")
	resp, _ := p.HandlePostAuth(ctx, req)
	if !resp.Block {
		t.Error("3rd request should be blocked by tight rule")
	}

	// Verify error body mentions the tight rule
	var errBody map[string]interface{}
	json.Unmarshal(resp.Body, &errBody)
	if errBody["rule"] != "tight" {
		t.Errorf("expected breach from 'tight' rule, got %v", errBody["rule"])
	}
}

func TestPostAuth_CompositeDimensions(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "per-app-model", Dimensions: []string{"app_id", "model"},
		Limit: Limit{Type: "requests", Value: 2}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	// App 1 + gpt-4o: 2 requests
	for i := 0; i < 2; i++ {
		req := testEnrichedRequest(1, 0, 0, "gpt-4o", fmt.Sprintf("req-am1-%d", i))
		resp, _ := p.HandlePostAuth(ctx, req)
		if resp.Block {
			t.Errorf("app1+gpt-4o request %d should pass", i)
		}
	}

	// App 1 + claude: should have its own counter
	req := testEnrichedRequest(1, 0, 0, "claude-sonnet", "req-am2-0")
	resp, _ := p.HandlePostAuth(ctx, req)
	if resp.Block {
		t.Error("app1+claude first request should pass (different composite key)")
	}

	// App 1 + gpt-4o: 3rd blocked
	req = testEnrichedRequest(1, 0, 0, "gpt-4o", "req-am1-2")
	resp, _ = p.HandlePostAuth(ctx, req)
	if !resp.Block {
		t.Error("app1+gpt-4o 3rd request should be blocked")
	}
}

func TestPostAuth_RateLimitHeaders_Returned(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "header-test", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 100}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	req := testEnrichedRequest(1, 0, 0, "", "req-h-0")
	resp, _ := p.HandlePostAuth(ctx, req)
	if resp.Block {
		t.Fatal("should not block")
	}

	if resp.Headers == nil {
		t.Fatal("expected rate limit headers")
	}
	if resp.Headers["X-RateLimit-Limit"] != "100" {
		t.Errorf("expected limit=100, got %s", resp.Headers["X-RateLimit-Limit"])
	}
	// Remaining reflects the state at evaluation time (before this request's increment)
	if resp.Headers["X-RateLimit-Remaining"] != "100" {
		t.Errorf("expected remaining=100, got %s", resp.Headers["X-RateLimit-Remaining"])
	}
	if resp.Headers["X-RateLimit-Reset"] == "" {
		t.Error("expected X-RateLimit-Reset header")
	}
}

// --- Fail-open / fail-closed tests ---

type failingStore struct{}

func (s *failingStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, fmt.Errorf("storage unavailable")
}
func (s *failingStore) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return fmt.Errorf("storage unavailable")
}
func (s *failingStore) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("storage unavailable")
}

func TestPostAuth_FailOpen_AllowsOnStorageError(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "fail-test", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 10}, Action: "enforce", Enabled: true,
	}}, func(c *Config) { c.FailOpen = true })

	// Replace store with failing one, but keep rules in cache
	p.rulesCache = &RuleSet{Rules: []Rule{{
		ID: "r1", Name: "fail-test", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 10}, Action: "enforce", Enabled: true,
	}}}
	p.rulesTTL = time.Now().Add(time.Hour)
	p.store = &failingStore{}

	ctx := testCtx()
	req := testEnrichedRequest(1, 0, 0, "", "req-fo-0")
	resp, err := p.HandlePostAuth(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Block {
		t.Error("fail-open should allow request on storage error")
	}
	if resp.Headers == nil || resp.Headers["X-RateLimit-Error"] != "storage_unavailable" {
		t.Error("expected X-RateLimit-Error header")
	}
}

func TestPostAuth_FailClosed_BlocksOnStorageError(t *testing.T) {
	p := newTestPlugin(nil, func(c *Config) { c.FailOpen = false })
	p.rulesCache = &RuleSet{Rules: []Rule{{
		ID: "r1", Name: "fail-test", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 10}, Action: "enforce", Enabled: true,
	}}}
	p.rulesTTL = time.Now().Add(time.Hour)
	p.store = &failingStore{}

	ctx := testCtx()
	req := testEnrichedRequest(1, 0, 0, "", "req-fc-0")
	resp, err := p.HandlePostAuth(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Block {
		t.Error("fail-closed should block on storage error")
	}
	if resp.StatusCode != 503 {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

// --- OnResponse Tests ---

func TestOnResponse_StreamChunk_PassThrough(t *testing.T) {
	p := newTestPlugin(nil)
	ctx := testCtx()
	req := testResponseWriteRequest("req-1", []byte("chunk"), true)

	resp, err := p.OnBeforeWrite(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Modified {
		t.Error("streaming chunks should pass through unmodified")
	}
}

func TestOnResponse_NilContext_PassThrough(t *testing.T) {
	p := newTestPlugin(nil)
	ctx := testCtx()
	req := &pb.ResponseWriteRequest{
		Context:       nil,
		Body:          []byte("test"),
		Headers:       map[string]string{},
		IsStreamChunk: false,
	}

	resp, err := p.OnBeforeWrite(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Modified {
		t.Error("nil context should pass through")
	}
}

func TestOnResponse_NoRequestState_PassThrough(t *testing.T) {
	p := newTestPlugin(nil)
	ctx := testCtx()
	req := testResponseWriteRequest("unknown-req", openAIResponse(100, 50), false)

	resp, err := p.OnBeforeWrite(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	// No request state stored → pass through
	if resp.Modified {
		t.Error("should pass through when no request state exists")
	}
}

func TestOnResponse_TokenCounting_UpdatesBucket(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "token-rule", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "tokens", Value: 10000}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	// Post-auth first to create request state
	req := testEnrichedRequest(10, 0, 0, "gpt-4o", "req-tok-1")
	resp, err := p.HandlePostAuth(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Block {
		t.Fatal("should not block")
	}

	// Verify request state was saved
	rs, err := ReadRequestState(context.Background(), p.store, RequestStateKey("req-tok-1"))
	if err != nil {
		t.Fatalf("expected request state to exist: %v", err)
	}
	if len(rs.TokenRuleKeys) != 1 {
		t.Fatalf("expected 1 token rule key, got %d", len(rs.TokenRuleKeys))
	}

	// Now simulate response with 200 total tokens
	writeReq := testResponseWriteRequest("req-tok-1", openAIResponse(150, 50), false)
	writeResp, err := p.OnBeforeWrite(ctx, writeReq)
	if err != nil {
		t.Fatal(err)
	}
	if !writeResp.Modified {
		t.Error("expected modified response with token headers")
	}
	if writeResp.Headers["X-RateLimit-Tokens-Used"] != "200" {
		t.Errorf("expected tokens-used=200, got %s", writeResp.Headers["X-RateLimit-Tokens-Used"])
	}

	// Verify the window bucket was updated
	epoch := BucketEpoch(time.Now(), 60)
	key := WindowKey("r1", "app_id:10", epoch)
	state := ReadWindowState(context.Background(), p.store, key)
	if state.Count != 200 {
		t.Errorf("expected bucket count=200, got %d", state.Count)
	}

	// Request state should be cleaned up
	_, err = ReadRequestState(context.Background(), p.store, RequestStateKey("req-tok-1"))
	if err == nil {
		t.Error("expected request state to be deleted after response")
	}
}

func TestOnResponse_ConcurrentDecrement(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "conc-rule", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "concurrent", Value: 10}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	// Post-auth increments concurrent
	req := testEnrichedRequest(7, 0, 0, "", "req-cd-1")
	resp, _ := p.HandlePostAuth(ctx, req)
	if resp.Block {
		t.Fatal("should not block")
	}

	// Check concurrent is 1
	concKey := ConcurrentKey("r1", "app_id:7")
	state := ReadConcurrentState(context.Background(), p.store, concKey)
	if state.Count != 1 {
		t.Errorf("expected concurrent=1 after post-auth, got %d", state.Count)
	}

	// Response decrements
	writeReq := testResponseWriteRequest("req-cd-1", []byte(`{"id":"test"}`), false)
	p.OnBeforeWrite(ctx, writeReq)

	state = ReadConcurrentState(context.Background(), p.store, concKey)
	if state.Count != 0 {
		t.Errorf("expected concurrent=0 after response, got %d", state.Count)
	}
}

func TestOnResponse_ConcurrentDecrement_FloorsAtZero(t *testing.T) {
	p := newTestPlugin(nil)
	ctx := testCtx()

	// Manually create request state with a concurrent key
	concKey := ConcurrentKey("r1", "app_id:1")
	WriteConcurrentState(context.Background(), p.store, concKey, ConcurrentState{Count: 0}, 5*time.Minute)
	WriteRequestState(context.Background(), p.store, RequestStateKey("req-floor"), &RequestState{
		ConcRuleKeys: []string{concKey},
		Timestamp:    time.Now().Unix(),
	}, 5*time.Minute)

	writeReq := testResponseWriteRequest("req-floor", []byte(`{}`), false)
	p.OnBeforeWrite(ctx, writeReq)

	state := ReadConcurrentState(context.Background(), p.store, concKey)
	if state.Count != 0 {
		t.Errorf("expected concurrent floored at 0, got %d", state.Count)
	}
}

func TestOnResponse_BlockedRequest_StillDecrementsConcurrent(t *testing.T) {
	// When a request is blocked after concurrent was already incremented,
	// the request state should still be saved so OnResponse can decrement.
	p := newTestPlugin([]Rule{
		{
			ID: "r1", Name: "conc", Dimensions: []string{"app_id"},
			Limit: Limit{Type: "concurrent", Value: 10}, Action: "enforce", Enabled: true, Priority: 0,
		},
		{
			ID: "r2", Name: "rpm", Dimensions: []string{"app_id"},
			Limit: Limit{Type: "requests", Value: 1}, Action: "enforce", Enabled: true, Priority: 1,
		},
	})
	ctx := testCtx()

	// First request: passes both rules
	req := testEnrichedRequest(1, 0, 0, "", "req-bd-0")
	resp, _ := p.HandlePostAuth(ctx, req)
	if resp.Block {
		t.Fatal("first request should pass")
	}

	// Concurrent is now 1
	concKey := ConcurrentKey("r1", "app_id:1")
	state := ReadConcurrentState(context.Background(), p.store, concKey)
	if state.Count != 1 {
		t.Fatalf("expected concurrent=1, got %d", state.Count)
	}

	// Second request: concurrent passes (limit 10), but rpm blocks (limit 1)
	req = testEnrichedRequest(1, 0, 0, "", "req-bd-1")
	resp, _ = p.HandlePostAuth(ctx, req)
	if !resp.Block {
		t.Fatal("second request should be blocked by rpm rule")
	}

	// Concurrent was incremented to 2 before the block
	state = ReadConcurrentState(context.Background(), p.store, concKey)
	if state.Count != 2 {
		t.Fatalf("expected concurrent=2 after blocked request, got %d", state.Count)
	}

	// Request state should exist so response phase can decrement
	rs, err := ReadRequestState(context.Background(), p.store, RequestStateKey("req-bd-1"))
	if err != nil {
		t.Fatal("expected request state to be saved for blocked request")
	}
	if len(rs.ConcRuleKeys) != 1 {
		t.Errorf("expected 1 concurrent key in request state, got %d", len(rs.ConcRuleKeys))
	}

	// Simulate response (even though blocked, the gateway may still call response hooks)
	writeReq := testResponseWriteRequest("req-bd-1", []byte(`{}`), false)
	p.OnBeforeWrite(ctx, writeReq)

	state = ReadConcurrentState(context.Background(), p.store, concKey)
	if state.Count != 1 {
		t.Errorf("expected concurrent=1 after decrement, got %d", state.Count)
	}
}

// --- Full lifecycle: PostAuth → Response ---

func TestFullLifecycle_RequestAndTokenRules(t *testing.T) {
	p := newTestPlugin([]Rule{
		{
			ID: "r1", Name: "rpm", Dimensions: []string{"app_id"},
			Limit: Limit{Type: "requests", Value: 100}, Action: "enforce", Enabled: true, Priority: 0,
		},
		{
			ID: "r2", Name: "tpm", Dimensions: []string{"app_id"},
			Limit: Limit{Type: "tokens", Value: 1000}, Action: "enforce", Enabled: true, Priority: 1,
		},
		{
			ID: "r3", Name: "conc", Dimensions: []string{"app_id"},
			Limit: Limit{Type: "concurrent", Value: 5}, Action: "enforce", Enabled: true, Priority: 2,
		},
	})
	ctx := testCtx()
	concKey := ConcurrentKey("r3", "app_id:1")

	// Phase 1: Send 3 requests without completing them (post-auth only)
	for i := 0; i < 3; i++ {
		reqID := fmt.Sprintf("lifecycle-%d", i)
		req := testEnrichedRequest(1, 0, 0, "gpt-4o", reqID)

		resp, err := p.HandlePostAuth(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Block {
			t.Fatalf("request %d should not be blocked", i)
		}
	}

	// All 3 in-flight: concurrent should be 3
	state := ReadConcurrentState(context.Background(), p.store, concKey)
	if state.Count != 3 {
		t.Errorf("expected concurrent=3 with 3 in-flight, got %d", state.Count)
	}

	// Phase 2: Complete all 3 requests, each with 200 tokens
	for i := 0; i < 3; i++ {
		reqID := fmt.Sprintf("lifecycle-%d", i)
		writeReq := testResponseWriteRequest(reqID, openAIResponse(100, 100), false)
		p.OnBeforeWrite(ctx, writeReq)
	}

	// After all responses: concurrent back to 0
	concState := ReadConcurrentState(context.Background(), p.store, concKey)
	if concState.Count != 0 {
		t.Errorf("expected concurrent=0 after all responses, got %d", concState.Count)
	}

	// Token count should be 600 (3 * 200)
	epoch := BucketEpoch(time.Now(), 60)
	tokenKey := WindowKey("r2", "app_id:1", epoch)
	tokenState := ReadWindowState(context.Background(), p.store, tokenKey)
	if tokenState.Count != 600 {
		t.Errorf("expected token count=600, got %d", tokenState.Count)
	}

	// Request count should be 3
	rpmKey := WindowKey("r1", "app_id:1", epoch)
	rpmState := ReadWindowState(context.Background(), p.store, rpmKey)
	if rpmState.Count != 3 {
		t.Errorf("expected rpm count=3, got %d", rpmState.Count)
	}
}

// --- RPC Tests ---

func TestRPC_CreateAndListRules(t *testing.T) {
	p := newTestPlugin(nil)

	// Create a rule
	payload, _ := json.Marshal(CreateRuleRequest{
		Name:       "test-rule",
		Dimensions: []string{"app_id", "model"},
		Limit:      Limit{Type: "requests", Value: 50},
		Action:     "enforce",
	})

	result, err := p.rpcCreateRule(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp := result.(RuleResponse)
	if !resp.Success {
		t.Error("expected success")
	}
	if resp.Rule.Name != "test-rule" {
		t.Errorf("expected test-rule, got %s", resp.Rule.Name)
	}
	if resp.Rule.ID == "" {
		t.Error("expected non-empty ID")
	}

	// List rules
	listResult, err := p.rpcListRules()
	if err != nil {
		t.Fatal(err)
	}
	list := listResult.(RulesListResponse)
	if list.Count != 1 {
		t.Errorf("expected 1 rule, got %d", list.Count)
	}
	if list.Rules[0].Name != "test-rule" {
		t.Errorf("expected test-rule, got %s", list.Rules[0].Name)
	}
}

func TestRPC_UpdateRule(t *testing.T) {
	p := newTestPlugin(nil)

	// Create
	payload, _ := json.Marshal(CreateRuleRequest{
		Name: "orig", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 10}, Action: "enforce",
	})
	result, _ := p.rpcCreateRule(payload)
	created := result.(RuleResponse).Rule

	// Update
	payload, _ = json.Marshal(UpdateRuleRequest{
		ID: created.ID, Name: "updated",
		Dimensions: []string{"app_id"}, Limit: Limit{Type: "tokens", Value: 999},
		Action: "log", Enabled: true,
	})
	result, err := p.rpcUpdateRule(payload)
	if err != nil {
		t.Fatal(err)
	}
	updated := result.(RuleResponse).Rule
	if updated.Name != "updated" {
		t.Errorf("expected 'updated', got %s", updated.Name)
	}
	if updated.Limit.Type != "tokens" || updated.Limit.Value != 999 {
		t.Error("limit not updated")
	}
	if updated.Action != "log" {
		t.Errorf("expected 'log', got %s", updated.Action)
	}
}

func TestRPC_DeleteRule(t *testing.T) {
	p := newTestPlugin(nil)

	payload, _ := json.Marshal(CreateRuleRequest{
		Name: "to-delete", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 1},
	})
	result, _ := p.rpcCreateRule(payload)
	id := result.(RuleResponse).Rule.ID

	payload, _ = json.Marshal(DeleteRuleRequest{ID: id})
	_, err := p.rpcDeleteRule(payload)
	if err != nil {
		t.Fatal(err)
	}

	listResult, _ := p.rpcListRules()
	list := listResult.(RulesListResponse)
	if list.Count != 0 {
		t.Errorf("expected 0 rules after delete, got %d", list.Count)
	}
}

func TestRPC_ReorderRules(t *testing.T) {
	p := newTestPlugin(nil)

	// Create 3 rules
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		payload, _ := json.Marshal(CreateRuleRequest{
			Name: fmt.Sprintf("rule-%d", i), Dimensions: []string{"global"},
			Limit: Limit{Type: "requests", Value: 100},
		})
		result, _ := p.rpcCreateRule(payload)
		ids[i] = result.(RuleResponse).Rule.ID
	}

	// Reverse order
	reversed := []string{ids[2], ids[1], ids[0]}
	payload, _ := json.Marshal(ReorderRulesRequest{RuleIDs: reversed})
	result, err := p.rpcReorderRules(payload)
	if err != nil {
		t.Fatal(err)
	}

	list := result.(RulesListResponse)
	if list.Rules[0].ID != ids[2] {
		t.Error("expected reversed order")
	}
	if list.Rules[0].Priority != 0 || list.Rules[2].Priority != 2 {
		t.Error("priorities not reassigned correctly")
	}
}

func TestRPC_CreateRule_Validation(t *testing.T) {
	p := newTestPlugin(nil)

	// Missing name
	payload, _ := json.Marshal(CreateRuleRequest{
		Dimensions: []string{"global"}, Limit: Limit{Type: "requests", Value: 1},
	})
	_, err := p.rpcCreateRule(payload)
	if err == nil {
		t.Error("expected validation error for missing name")
	}

	// Invalid dimension
	payload, _ = json.Marshal(CreateRuleRequest{
		Name: "bad", Dimensions: []string{"invalid"}, Limit: Limit{Type: "requests", Value: 1},
	})
	_, err = p.rpcCreateRule(payload)
	if err == nil {
		t.Error("expected validation error for invalid dimension")
	}

	// Invalid limit type
	payload, _ = json.Marshal(CreateRuleRequest{
		Name: "bad", Dimensions: []string{"global"}, Limit: Limit{Type: "bogus", Value: 1},
	})
	_, err = p.rpcCreateRule(payload)
	if err == nil {
		t.Error("expected validation error for invalid limit type")
	}
}

// --- Rules cache tests ---

func TestRulesCache_HitAndExpiry(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "cached", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 100}, Enabled: true,
	}})

	// First load populates cache
	rules := p.loadRulesCached()
	if len(rules) != 1 || rules[0].Name != "cached" {
		t.Fatal("expected to load 1 rule")
	}

	// Mutate KV directly — cache should still return old value
	p.saveRulesToKV([]Rule{{
		ID: "r2", Name: "new-rule", Dimensions: []string{"global"},
		Limit: Limit{Type: "requests", Value: 50}, Enabled: true,
	}})

	rules = p.loadRulesCached()
	if len(rules) != 1 || rules[0].Name != "cached" {
		t.Error("expected cached value, not fresh KV")
	}

	// Invalidate cache
	p.invalidateRulesCache()
	rules = p.loadRulesCached()
	if len(rules) != 1 || rules[0].Name != "new-rule" {
		t.Error("expected fresh value after cache invalidation")
	}
}

// --- OnBeforeWriteHeaders no-op test ---

func TestOnBeforeWriteHeaders_NoOp(t *testing.T) {
	p := newTestPlugin(nil)
	ctx := testCtx()
	req := &pb.HeadersRequest{
		Headers: map[string]string{"X-Existing": "value"},
	}
	resp, err := p.OnBeforeWriteHeaders(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Modified {
		t.Error("OnBeforeWriteHeaders should be a no-op")
	}
	if resp.Headers["X-Existing"] != "value" {
		t.Error("should pass through existing headers")
	}
}
