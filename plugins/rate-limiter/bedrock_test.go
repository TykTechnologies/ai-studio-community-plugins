package main

import (
	"testing"

	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

const converseAnswer = `{"output":{"message":{"role":"assistant","content":[{"text":"Paris."}]}},"stopReason":"end_turn","usage":{"inputTokens":200,"outputTokens":100,"totalTokens":300}}`

func bedrockWriteRequest(requestID string) *pb.ResponseWriteRequest {
	return &pb.ResponseWriteRequest{
		Context: &pb.PluginContext{RequestId: requestID, Vendor: "bedrock"},
		Body:    []byte(converseAnswer),
		Headers: map[string]string{},
	}
}

// Token rules count Bedrock Converse's camelCase usage and enforce on it.
func TestTokenRule_BedrockConverse_CountsAndEnforces(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "r1", Name: "token-limit", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "tokens", Value: 500}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	for i, id := range []string{"br-1", "br-2"} {
		req := testEnrichedRequest(10, 0, 7, "bedrock-llm", id)
		req.Request.Context.Vendor = "bedrock"
		resp, err := p.HandlePostAuth(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Block {
			t.Fatalf("request %d blocked before the limit", i+1)
		}
		writeResp, err := p.OnBeforeWrite(ctx, bedrockWriteRequest(id))
		if err != nil {
			t.Fatal(err)
		}
		if got := writeResp.Headers["X-RateLimit-Tokens-Used"]; got != "300" {
			t.Errorf("request %d: X-RateLimit-Tokens-Used = %q, want 300", i+1, got)
		}
	}

	// 600 tokens counted against a limit of 500: the next request is blocked.
	req := testEnrichedRequest(10, 0, 7, "bedrock-llm", "br-3")
	req.Request.Context.Vendor = "bedrock"
	resp, err := p.HandlePostAuth(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Block {
		t.Error("expected a block once Bedrock usage passed the token limit")
	}
}

// A finished stream counts its tokens and releases its concurrent slot, for
// Bedrock's Converse events as OnStreamComplete gets them and for an OpenAI
// stream with usage.
func TestOnStreamComplete_CountsTokensAndReleasesConcurrent(t *testing.T) {
	streams := []struct {
		vendor, body string
	}{
		{"bedrock", "data: {\"messageStart\":{\"role\":\"assistant\"}}\n\ndata: {\"contentBlockDelta\":{\"delta\":{\"text\":\"Paris.\"},\"contentBlockIndex\":0}}\n\ndata: {\"metadata\":{\"usage\":{\"inputTokens\":200,\"outputTokens\":100,\"totalTokens\":300}}}\n\n"},
		{"openai", "data: {\"choices\":[{\"delta\":{\"content\":\"Paris.\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":200,\"completion_tokens\":100,\"total_tokens\":300}}\n\ndata: [DONE]\n\n"},
	}
	for _, s := range streams {
		t.Run(s.vendor, func(t *testing.T) {
			p := newTestPlugin([]Rule{
				{ID: "tok", Name: "tokens", Dimensions: []string{"app_id"}, Limit: Limit{Type: "tokens", Value: 500}, Action: "enforce", Enabled: true, Priority: 1},
				{ID: "conc", Name: "concurrent", Dimensions: []string{"app_id"}, Limit: Limit{Type: "concurrent", Value: 1}, Action: "enforce", Enabled: true, Priority: 2},
			})
			ctx := testCtx()

			for i, id := range []string{"s-1", "s-2"} {
				req := testEnrichedRequest(10, 0, 7, "llm", id)
				req.Request.Context.Vendor = s.vendor
				resp, err := p.HandlePostAuth(ctx, req)
				if err != nil {
					t.Fatal(err)
				}
				if resp.Block {
					t.Fatalf("stream %d blocked: the previous stream's concurrent slot or tokens were not settled", i+1)
				}
				done, err := p.OnStreamComplete(ctx, &pb.StreamCompleteRequest{
					AccumulatedResponse: []byte(s.body),
					StatusCode:          200,
					Context:             &pb.PluginContext{RequestId: id, Vendor: s.vendor},
				})
				if err != nil {
					t.Fatal(err)
				}
				if !done.Handled {
					t.Errorf("stream %d: not handled", i+1)
				}
			}

			req := testEnrichedRequest(10, 0, 7, "llm", "s-3")
			req.Request.Context.Vendor = s.vendor
			resp, err := p.HandlePostAuth(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			if !resp.Block {
				t.Error("expected a block after 600 streamed tokens against a limit of 500")
			}
		})
	}
}

// An upstream error ends a stream too: its concurrent slot is released.
func TestOnStreamComplete_ErrorReleasesConcurrent(t *testing.T) {
	p := newTestPlugin([]Rule{{
		ID: "conc", Name: "concurrent", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "concurrent", Value: 1}, Action: "enforce", Enabled: true,
	}})
	ctx := testCtx()

	if resp, _ := p.HandlePostAuth(ctx, testEnrichedRequest(10, 0, 7, "llm", "e-1")); resp.Block {
		t.Fatal("first request blocked")
	}
	if _, err := p.OnStreamComplete(ctx, &pb.StreamCompleteRequest{
		AccumulatedResponse: []byte(`{"message":"Too many requests"}`),
		StatusCode:          429,
		Context:             &pb.PluginContext{RequestId: "e-1", Vendor: "bedrock"},
	}); err != nil {
		t.Fatal(err)
	}
	if resp, _ := p.HandlePostAuth(ctx, testEnrichedRequest(10, 0, 7, "llm", "e-2")); resp.Block {
		t.Error("concurrent slot still held after the stream ended with an error")
	}
}
