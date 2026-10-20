package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
)

// llmGateway answers GetLLM with an endpoint, on top of fakeGateway.
type llmGateway struct {
	*fakeGateway
	endpoint string
}

func (g *llmGateway) GetLLM(ctx context.Context, llmID uint32) (interface{}, error) {
	return &mgmtpb.GetLLMResponse{Llm: &mgmtpb.LLMInfo{Id: llmID, Endpoint: g.endpoint}}, nil
}

type llmBroker struct {
	plugin_sdk.ServiceBroker
	gw *llmGateway
}

func (b *llmBroker) Gateway() plugin_sdk.GatewayServices { return b.gw }
func (b *llmBroker) Logger() plugin_sdk.LogService       { return nil }

// hookPlugin is a gateway plugin with configJSON and a request context for
// an OpenAI LLM.
func hookPlugin(t *testing.T, configJSON string) (*PromptCompressorPlugin, plugin_sdk.Context) {
	t.Helper()
	gw := &llmGateway{fakeGateway: &fakeGateway{metadata: map[uint32]string{}}, endpoint: "https://api.openai.com/v1"}
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, Services: &llmBroker{gw: gw}, Context: context.Background()}
	p := NewPromptCompressorPlugin()
	if err := p.Initialize(ctx, hostConfig(t, configJSON)); err != nil {
		t.Fatal(err)
	}
	ctx.RequestID, ctx.LLMID, ctx.LLMSlug, ctx.Vendor = "req-1", 4, "gpt", "openai"
	return p, ctx
}

// noisyBody is a chat request whose tool output level 1 compresses.
func noisyBody(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"model": "gpt-4o", "messages": []any{
		map[string]any{"role": "user", "content": "run it"},
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "shell", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": strings.Repeat("\u001b[32mok\u001b[0m line   \n\n\n", 200)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func request(body []byte, headers map[string]string) *pb.EnrichedRequest {
	if headers == nil {
		headers = map[string]string{"Authorization": "Bearer secret", "Content-Type": "application/json"}
	}
	return &pb.EnrichedRequest{Request: &pb.PluginRequest{Method: "POST", Path: "/llm/rest/gpt/v1/chat/completions", Headers: headers, Body: body}, AppId: "1", Authenticated: true}
}

func TestPostAuthModes(t *testing.T) {
	body := noisyBody(t)
	for _, tc := range []struct {
		mode      string
		modified  bool
		outcome   string
		forwarded bool
	}{
		{"off", false, "", false},
		{"audit", false, "compressed", false},
		{"enforce", true, "compressed", true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			p, ctx := hookPlugin(t, `{"mode":"`+tc.mode+`"}`)
			resp, err := p.HandlePostAuth(ctx, request(body, nil))
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if resp.Modified != tc.modified || resp.Headers != nil || resp.Block {
				t.Fatalf("response %+v", resp)
			}
			if tc.modified {
				want, _ := compress.New(compress.Options{}).Compress(compress.Input{Format: "openai", Body: body, Policy: p.resolvePolicy(ctx).Policy})
				if !bytes.Equal(resp.Body, want) || len(resp.Body) >= len(body) {
					t.Fatal("enforce did not forward the compress output")
				}
			} else if len(resp.Body) != 0 {
				t.Fatal("an unmodified response carries a body")
			}
			rec, ok := p.pending.take("req-1")
			if tc.outcome == "" {
				if ok {
					t.Fatalf("off mode recorded %+v", rec)
				}
				return
			}
			if !ok || rec.Outcome != tc.outcome || rec.Forwarded != tc.forwarded || rec.PolicyHash == "" || rec.LLMID != 4 || rec.Report.TokensSaved() <= 0 {
				t.Fatalf("pending record %+v", rec)
			}
		})
	}
}

func TestPostAuthBypass(t *testing.T) {
	body := noisyBody(t)
	hdr := map[string]string{"x-tyk-compression": " OFF ", "Authorization": "Bearer secret"}

	p, ctx := hookPlugin(t, `{"mode":"enforce","allow_client_bypass":true}`)
	resp, _ := p.HandlePostAuth(ctx, request(body, hdr))
	rec, _ := p.pending.take("req-1")
	if resp.Modified || rec.Outcome != OutcomeBypassed {
		t.Fatalf("bypass allowed: %+v, %+v", resp, rec)
	}

	p, ctx = hookPlugin(t, `{"mode":"enforce","allow_client_bypass":false}`)
	resp, _ = p.HandlePostAuth(ctx, request(body, hdr))
	if !resp.Modified {
		t.Fatal("the bypass header was honoured while allow_client_bypass is false")
	}
	// No other value of the header does anything.
	p, ctx = hookPlugin(t, `{"mode":"enforce","allow_client_bypass":true}`)
	resp, _ = p.HandlePostAuth(ctx, request(body, map[string]string{"X-Tyk-Compression": "max"}))
	if !resp.Modified {
		t.Fatal("an unknown header value changed the outcome")
	}
}

// A panic inside the compressor fails open: the request continues
// unchanged, with no error, and the outcome is recorded.
func TestPostAuthPanicFailsOpen(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"enforce"}`)
	p.compressFn = func(compress.Input) ([]byte, compress.Report) { panic("boom with request text") }
	resp, err := p.HandlePostAuth(ctx, request(noisyBody(t), nil))
	if err != nil || resp == nil || resp.Modified || resp.Headers != nil {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	rec, ok := p.pending.take("req-1")
	if !ok || rec.Outcome != OutcomeFailedOpen {
		t.Fatalf("record %+v", rec)
	}
	if p.counters.failedOpen.Load() != 1 {
		t.Error("failed_open not counted")
	}
}

func TestPostAuthSkips(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"enforce","max_body_bytes":100}`)
	resp, _ := p.HandlePostAuth(ctx, request(noisyBody(t), nil))
	rec, _ := p.pending.take("req-1")
	if resp.Modified || rec.Outcome != "skipped" || rec.Report.Reason != compress.ReasonTooLarge {
		t.Fatalf("oversized: %+v %+v", resp, rec)
	}

	p, ctx = hookPlugin(t, `{"mode":"enforce"}`)
	ctx.Vendor = "huggingface"
	resp, _ = p.HandlePostAuth(ctx, request(noisyBody(t), nil))
	rec, _ = p.pending.take("req-1")
	if resp.Modified || rec.Report.Reason != compress.ReasonUnsupportedFormat {
		t.Fatalf("unknown format: %+v %+v", resp, rec)
	}

	// A policy bound to "off" compresses nothing.
	p, ctx = hookPlugin(t, `{"mode":"enforce","bindings":{"llm:gpt":"off"}}`)
	resp, _ = p.HandlePostAuth(ctx, request(noisyBody(t), nil))
	rec, _ = p.pending.take("req-1")
	if resp.Modified || rec.Report.Reason != compress.ReasonOff {
		t.Fatalf("off policy: %+v %+v", resp, rec)
	}
}

// The LLM's endpoint decides the format: a Responses call through an LLM
// whose endpoint carries /v1 is read as Responses.
func TestPostAuthFormatFromEndpoint(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"enforce"}`)
	body, _ := json.Marshal(map[string]any{"model": "gpt-5", "input": []any{
		map[string]any{"type": "function_call", "name": "shell", "call_id": "c1", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "c1", "output": strings.Repeat("log line   \n\n\n", 200)},
	}})
	req := request(body, nil)
	req.Request.Path = "/llm/rest/gpt/responses"
	resp, _ := p.HandlePostAuth(ctx, req)
	rec, _ := p.pending.take("req-1")
	if !resp.Modified || rec.Report.Format != "openai_responses" {
		t.Fatalf("resp modified %v, record %+v", resp.Modified, rec)
	}
}

// The pending map stays bounded under a soak with no response hook taking
// records, and old records expire.
func TestPendingBounded(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"audit"}`)
	p.compressFn = func(in compress.Input) ([]byte, compress.Report) {
		return in.Body, compress.Report{Outcome: compress.OutcomeUnchanged}
	}
	body := noisyBody(t)
	for i := 0; i < 10000; i++ {
		ctx.RequestID = fmt.Sprintf("req-%d", i)
		if _, err := p.HandlePostAuth(ctx, request(body, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if n := p.pending.len(); n > pendingMaxSize {
		t.Fatalf("pending holds %d records", n)
	}
	if _, ok := p.pending.take("req-9999"); !ok {
		t.Error("the newest record was dropped")
	}

	c := newPendingCache(time.Minute, 100)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	c.put(pendingRecord{RequestID: "old"})
	now = now.Add(2 * time.Minute)
	if _, ok := c.take("old"); ok {
		t.Error("an expired record was returned")
	}
	c.put(pendingRecord{RequestID: "a"})
	now = now.Add(2 * time.Minute)
	c.put(pendingRecord{RequestID: "b"})
	if c.len() != 1 {
		t.Errorf("expired records kept: %d", c.len())
	}
}
