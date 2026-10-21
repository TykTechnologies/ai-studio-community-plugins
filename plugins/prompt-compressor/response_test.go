package main

import (
	"strconv"
	"sync"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// captureRecorder keeps what it is given.
type captureRecorder struct {
	mu  sync.Mutex
	got []records.Request
}

func (c *captureRecorder) Add(r records.Request) {
	c.mu.Lock()
	c.got = append(c.got, r)
	c.mu.Unlock()
}

// A buffered response: the header hook keeps every upstream header and adds
// the labels, the body hook completes the record with the usage and frees
// the pending entry.
func TestResponsePhaseBuffered(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"enforce"}`)
	rec := &captureRecorder{}
	p.SetRecorder(rec)
	if resp, _ := p.HandlePostAuth(ctx, request(noisyBody(t), nil)); !resp.Modified {
		t.Fatal("post_auth did not compress")
	}

	upstream := map[string]string{"Content-Type": "application/json", "X-Request-Id": "abc", "Openai-Processing-Ms": "12", "X-Cache": "MISS"}
	hr, err := p.OnBeforeWriteHeaders(ctx, &pb.HeadersRequest{Headers: upstream})
	if err != nil || !hr.Modified {
		t.Fatalf("header hook: %+v %v", hr, err)
	}
	for k, v := range upstream {
		if hr.Headers[k] != v {
			t.Errorf("upstream header %s = %q, want %q", k, hr.Headers[k], v)
		}
	}
	if len(hr.Headers) != len(upstream)+3 {
		t.Errorf("headers %v", hr.Headers)
	}
	if hr.Headers[HeaderOutcome] != records.OutcomeApplied {
		t.Errorf("outcome header %q", hr.Headers[HeaderOutcome])
	}
	if n, err := strconv.Atoi(hr.Headers[HeaderTokensSaved]); err != nil || n <= 0 {
		t.Errorf("tokens saved header %q", hr.Headers[HeaderTokensSaved])
	}
	if len(hr.Headers[HeaderPolicy]) != 12 {
		t.Errorf("policy header %q", hr.Headers[HeaderPolicy])
	}

	body := `{"choices":[],"usage":{"prompt_tokens":2000,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":1024}}}`
	wr, err := p.OnBeforeWrite(ctx, &pb.ResponseWriteRequest{Body: []byte(body)})
	if err != nil || wr.Modified {
		t.Fatalf("body hook: %+v %v", wr, err)
	}
	if p.pending.len() != 0 {
		t.Error("the pending entry was not freed")
	}
	if len(rec.got) != 1 {
		t.Fatalf("records %+v", rec.got)
	}
	r := rec.got[0]
	if r.Outcome != records.OutcomeApplied || r.InputTokens != 2000 || r.CacheReadTokens != 1024 || r.UsageMissing || r.Streaming ||
		r.TokensSaved() <= 0 || r.LLMID != 4 || r.Model != "gpt-4o" || len(r.Strategies) == 0 || r.PolicyHash == "" {
		t.Errorf("record %+v", r)
	}
}

// A streaming response completes its record from the accumulated stream and
// frees its pending entry; a stream without usage says so.
func TestResponsePhaseStreaming(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"audit"}`)
	rec := &captureRecorder{}
	p.SetRecorder(rec)
	p.HandlePostAuth(ctx, request(noisyBody(t), nil))

	hr, _ := p.OnBeforeWriteHeaders(ctx, &pb.HeadersRequest{Headers: map[string]string{"Content-Type": "text/event-stream"}})
	if hr.Headers[HeaderOutcome] != records.OutcomeAudit || hr.Headers["Content-Type"] != "text/event-stream" {
		t.Errorf("headers %v", hr.Headers)
	}
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	if _, err := p.OnStreamComplete(ctx, &pb.StreamCompleteRequest{AccumulatedResponse: []byte(stream)}); err != nil {
		t.Fatal(err)
	}
	if p.pending.len() != 0 {
		t.Error("the pending entry was not freed")
	}
	if len(rec.got) != 1 || !rec.got[0].UsageMissing || !rec.got[0].Streaming || rec.got[0].InputTokens != 0 {
		t.Fatalf("records %+v", rec.got)
	}
}

// Requests post_auth did not see are left alone by the response hooks.
func TestResponsePhaseUnknownRequest(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"enforce"}`)
	ctx.RequestID = "never-seen"
	hr, err := p.OnBeforeWriteHeaders(ctx, &pb.HeadersRequest{Headers: map[string]string{"A": "b"}})
	if err != nil || hr.Modified || hr.Headers != nil {
		t.Fatalf("header hook changed an unknown response: %+v", hr)
	}
	if wr, _ := p.OnBeforeWrite(ctx, &pb.ResponseWriteRequest{Body: []byte(`{}`)}); wr.Modified {
		t.Fatal("body hook changed an unknown response")
	}
}

// Bypassed and skipped requests are labelled too, without a saving.
func TestResponsePhaseOutcomes(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"enforce","allow_client_bypass":true}`)
	p.HandlePostAuth(ctx, request(noisyBody(t), map[string]string{"X-Tyk-Compression": "off"}))
	hr, _ := p.OnBeforeWriteHeaders(ctx, &pb.HeadersRequest{Headers: map[string]string{}})
	if hr.Headers[HeaderOutcome] != records.OutcomeBypassed || hr.Headers[HeaderTokensSaved] != "" {
		t.Errorf("bypassed: %v", hr.Headers)
	}

	p, ctx = hookPlugin(t, `{"mode":"enforce","bindings":{"llm:gpt":"off"}}`)
	p.HandlePostAuth(ctx, request(noisyBody(t), nil))
	hr, _ = p.OnBeforeWriteHeaders(ctx, &pb.HeadersRequest{Headers: map[string]string{}})
	if hr.Headers[HeaderOutcome] != records.OutcomeSkipped {
		t.Errorf("off policy: %v", hr.Headers)
	}
}
