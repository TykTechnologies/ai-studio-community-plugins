package main

import (
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

const (
	converseBody   = `{"messages":[{"role":"user","content":[{"text":"capital of France?"}]}]}`
	converseAnswer = `{"output":{"message":{"role":"assistant","content":[{"text":"Paris."}]}},"stopReason":"end_turn","usage":{"inputTokens":12,"outputTokens":3,"totalTokens":15}}`
	novaPath       = "/llm/rest/br/model/amazon.nova-pro-v1:0/converse"
)

func bedrockPostAuth(t *testing.T, p *LLMCachePlugin, requestID, path string) *pb.PluginResponse {
	t.Helper()
	resp, err := p.HandlePostAuth(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio}, &pb.EnrichedRequest{
		Request: &pb.PluginRequest{
			Method:  "POST",
			Path:    path,
			Body:    []byte(converseBody),
			Context: &pb.PluginContext{RequestId: requestID, Vendor: "bedrock", LlmId: 7, LlmSlug: "br", AppId: 1},
		},
		Authenticated: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func bedrockWrite(t *testing.T, p *LLMCachePlugin, requestID, body string, headers map[string]string) {
	t.Helper()
	if headers == nil {
		headers = map[string]string{"Content-Type": "application/json"}
	}
	if _, err := p.OnBeforeWrite(plugin_sdk.Context{}, &pb.ResponseWriteRequest{
		Body:    []byte(body),
		Headers: headers,
		Context: &pb.PluginContext{RequestId: requestID, Vendor: "bedrock", LlmId: 7},
	}); err != nil {
		t.Fatal(err)
	}
}

// A Converse answer is stored with its camelCase token count, so a hit
// reports tokens saved; AWS errors are never stored.
func TestBedrockConverse_TokensSavedAndErrorsNotCached(t *testing.T) {
	p := newCachePlugin(t)

	bedrockPostAuth(t, p, "err-1", novaPath)
	bedrockWrite(t, p, "err-1", `{"message":"Too many requests, please wait before trying again."}`, nil)
	bedrockPostAuth(t, p, "err-2", novaPath)
	bedrockWrite(t, p, "err-2", converseAnswer, map[string]string{"Content-Type": "application/json", "X-Amzn-Errortype": "ThrottlingException"})
	if resp := bedrockPostAuth(t, p, "after-err", novaPath); resp.Block {
		t.Fatal("an AWS error response was served from cache")
	}

	bedrockWrite(t, p, "after-err", converseAnswer, nil)
	resp := bedrockPostAuth(t, p, "hit", novaPath)
	if !resp.Block || string(resp.Body) != converseAnswer {
		t.Fatalf("expected a hit with the Converse answer, got block=%v body=%s", resp.Block, resp.Body)
	}
	if got := p.metrics.GetTotalTokensSaved(); got != 15 {
		t.Errorf("tokens saved = %d, want 15", got)
	}
}

// Headers other plugins set about the request that produced the answer are
// not stored with it, so a hit never reports compression, rate-limit usage or
// routing that did not happen for it. The vendor's own headers are kept.
func TestHitDoesNotReplayOtherPluginsRequestHeaders(t *testing.T) {
	p := newCachePlugin(t)
	bedrockPostAuth(t, p, "miss", novaPath)
	bedrockWrite(t, p, "miss", converseAnswer, map[string]string{
		"Content-Type":                   "application/json",
		"X-Amzn-Requestid":               "aws-1",
		"X-Tyk-Compression":              "applied",
		"X-Tyk-Compression-Tokens-Saved": "512",
		"X-Tyk-Compression-Policy":       "abcdef123456",
		"X-RateLimit-Tokens-Used":        "15",
		"X-Ratelimit-Remaining":          "9",
		"X-LB-Upstream":                  "east",
		"X-LB-Pool":                      "br",
		"X-LB-Latency-Ms":                "200",
		"Server-Timing":                  "upstream;dur=200",
		"X-Tyk-AI-Request-Id":            "req-1",
	})

	resp := bedrockPostAuth(t, p, "hit", novaPath)
	if !resp.Block {
		t.Fatal("expected a hit")
	}
	for k := range resp.Headers {
		lk := strings.ToLower(k)
		for _, prefix := range []string{"x-tyk-compression", "x-ratelimit", "x-lb-", "server-timing", "x-tyk-ai-request-id"} {
			if strings.HasPrefix(lk, prefix) {
				t.Errorf("hit replays %s: %s", k, resp.Headers[k])
			}
		}
	}
	if resp.Headers["X-Amzn-Requestid"] != "aws-1" {
		t.Errorf("vendor header not kept: %v", resp.Headers)
	}
	if resp.Headers["X-Cache-Status"] != "HIT" {
		t.Errorf("X-Cache-Status = %q, want HIT", resp.Headers["X-Cache-Status"])
	}
}

// An entry written before 1.3.0 still holds those headers; a hit filters
// them again when it replays the entry.
func TestHitHeadersFilterOldEntries(t *testing.T) {
	stored := map[string]string{
		"x-tyk-compression":   "applied",
		"X-RATELIMIT-LIMIT":   "10",
		"x-Lb-Upstream":       "east",
		"X-Tyk-AI-Request-Id": "req-1",
		"X-Amzn-Requestid":    "aws-1",
	}
	got := hitHeaders(stored, []byte(converseAnswer), false)
	for k := range stored {
		if _, ok := got[k]; ok && k != "X-Amzn-Requestid" {
			t.Errorf("replayed %s", k)
		}
	}
	if got["X-Amzn-Requestid"] != "aws-1" {
		t.Errorf("vendor header dropped: %v", got)
	}
}

func TestIsErrorResponse_Bedrock(t *testing.T) {
	p := &LLMCachePlugin{}
	cases := []struct {
		name, vendor, body string
		headers            map[string]string
		want               bool
	}{
		{"converse answer", "bedrock", converseAnswer, nil, false},
		{"invoke answer", "bedrock", `{"id":"msg_1","type":"message","content":[{"type":"text","text":"hi"}]}`, nil, false},
		{"message error", "bedrock", `{"message":"The security token included in the request is invalid."}`, nil, true},
		{"typed error", "bedrock", `{"__type":"ValidationException","message":"bad"}`, nil, true},
		{"error header", "bedrock", converseAnswer, map[string]string{"x-amzn-errortype": "ValidationException"}, true},
		{"openai error", "openai", `{"error":{"message":"bad"}}`, nil, true},
		{"message is not an error for other vendors", "ollama", `{"model":"m","message":"hi"}`, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.isErrorResponse(tc.vendor, tc.headers, []byte(tc.body)); got != tc.want {
				t.Errorf("isErrorResponse = %v, want %v", got, tc.want)
			}
		})
	}
}

func pendingCount(p *LLMCachePlugin) int {
	p.pendingMu.RLock()
	defer p.pendingMu.RUnlock()
	return len(p.pendingCache)
}

// Every stream releases its pending entry when it completes, whether stream
// caching is on or off, and a Bedrock converse-stream goes upstream uncached.
func TestStreamComplete_ReleasesPending(t *testing.T) {
	for _, cacheStreams := range []bool{true, false} {
		p := newCachePlugin(t)
		p.config.CacheStreamingResponses = cacheStreams

		for _, id := range []string{"s-1", "s-2", "s-3"} {
			req := &pb.EnrichedRequest{Request: &pb.PluginRequest{
				Method:  "POST",
				Path:    "/llm/stream/gpt/v1/chat/completions",
				Body:    []byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`),
				Context: &pb.PluginContext{RequestId: id, Vendor: "openai", LlmId: 1},
			}}
			if _, err := p.HandlePostAuth(plugin_sdk.Context{}, req); err != nil {
				t.Fatal(err)
			}
			if _, err := p.OnStreamComplete(plugin_sdk.Context{}, &pb.StreamCompleteRequest{
				AccumulatedResponse: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"),
				StatusCode:          200,
				Context:             &pb.PluginContext{RequestId: id, Vendor: "openai"},
			}); err != nil {
				t.Fatal(err)
			}
		}

		resp := bedrockPostAuth(t, p, "br-stream", "/llm/stream/br/model/amazon.nova-pro-v1:0/converse-stream")
		if resp.Block {
			t.Fatal("a converse-stream request was served from cache")
		}
		p.pendingMu.RLock()
		op := p.pendingCache["br-stream"]
		p.pendingMu.RUnlock()
		if op == nil || op.ShouldCache {
			t.Errorf("converse-stream pending op = %+v, want one that does not cache", op)
		}
		if _, err := p.OnStreamComplete(plugin_sdk.Context{}, &pb.StreamCompleteRequest{
			AccumulatedResponse: []byte("data: {\"metadata\":{\"usage\":{\"inputTokens\":12,\"outputTokens\":3,\"totalTokens\":15}}}\n\n"),
			StatusCode:          200,
			Context:             &pb.PluginContext{RequestId: "br-stream", Vendor: "bedrock"},
		}); err != nil {
			t.Fatal(err)
		}

		if n := pendingCount(p); n != 0 {
			t.Errorf("cache_streaming_responses=%v: %d pending entries left after the streams completed", cacheStreams, n)
		}
	}
}

// A streamed response with an error status is never cached.
func TestStreamComplete_ErrorStatusNotCached(t *testing.T) {
	p := newCachePlugin(t)
	req := &pb.EnrichedRequest{Request: &pb.PluginRequest{
		Method:  "POST",
		Path:    "/llm/stream/gpt/v1/chat/completions",
		Body:    []byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`),
		Context: &pb.PluginContext{RequestId: "s-err", Vendor: "openai", LlmId: 1},
	}}
	if _, err := p.HandlePostAuth(plugin_sdk.Context{}, req); err != nil {
		t.Fatal(err)
	}
	done, err := p.OnStreamComplete(plugin_sdk.Context{}, &pb.StreamCompleteRequest{
		AccumulatedResponse: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"overloaded\"}}]}\n\ndata: [DONE]\n\n"),
		StatusCode:          529,
		Context:             &pb.PluginContext{RequestId: "s-err", Vendor: "openai"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.Cached {
		t.Error("a 529 stream was cached")
	}
}
