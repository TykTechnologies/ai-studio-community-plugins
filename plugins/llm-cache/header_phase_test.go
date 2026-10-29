package main

import (
	"testing"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// X-Cache-Status is set in the header phase too (lsj.26): a stream, and an
// /ai or /v1 call whose inner hop streams, has no body phase, so a MISS
// labelled only in OnBeforeWrite never reached those clients.
func TestCacheStatusSetInHeaderPhase(t *testing.T) {
	p := newCachePlugin(t)
	pctx := func(id string) *pb.PluginContext {
		return &pb.PluginContext{RequestId: id, Vendor: "openai", LlmId: 1, LlmSlug: "gpt", AppId: 1, Metadata: map[string]string{}}
	}
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, AppID: 1, LLMID: 1, LLMSlug: "gpt", Vendor: "openai"}
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	resp, err := p.HandlePostAuth(ctx, &pb.EnrichedRequest{Request: &pb.PluginRequest{
		Method: "POST", Path: "/llm/call/gpt/v1/chat/completions", Body: body, Context: pctx("miss"),
	}})
	if err != nil || resp.Block {
		t.Fatalf("post_auth: block=%v err=%v", resp.GetBlock(), err)
	}

	hr, err := p.OnBeforeWriteHeaders(ctx, &pb.HeadersRequest{
		Headers: map[string]string{"Content-Type": "text/event-stream"},
		Context: pctx("miss"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hr.Modified || hr.Headers["X-Cache-Status"] != "MISS" || hr.Headers["Content-Type"] != "text/event-stream" {
		t.Errorf("header phase = %+v, want X-Cache-Status MISS and the existing headers", hr)
	}

	// The header phase leaves the pending operation to the body phase or
	// the stream's completion, which store the answer.
	p.pendingMu.Lock()
	_, pending := p.pendingCache["miss"]
	p.pendingMu.Unlock()
	if !pending {
		t.Fatal("header phase removed the pending operation")
	}

	// A request the cache never saw is left alone.
	hr, err = p.OnBeforeWriteHeaders(ctx, &pb.HeadersRequest{Headers: map[string]string{}, Context: pctx("unknown")})
	if err != nil {
		t.Fatal(err)
	}
	if hr.Modified || hr.Headers["X-Cache-Status"] != "" {
		t.Errorf("unknown request modified: %+v", hr)
	}
}

// A bypassed request is labelled BYPASS in the header phase, as in the body
// phase.
func TestCacheBypassSetInHeaderPhase(t *testing.T) {
	p := newCachePlugin(t)
	p.pendingMu.Lock()
	p.pendingCache["byp"] = &PendingCacheOp{Bypassed: true}
	p.pendingMu.Unlock()

	hr, err := p.OnBeforeWriteHeaders(plugin_sdk.Context{}, &pb.HeadersRequest{
		Headers: map[string]string{}, Context: &pb.PluginContext{RequestId: "byp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hr.Headers["X-Cache-Status"] != "BYPASS" {
		t.Errorf("X-Cache-Status = %q, want BYPASS", hr.Headers["X-Cache-Status"])
	}
}
