package main

// These tests put the cache behind a small stand-in for the edge gateway and
// talk to it the way Studio's OpenAI-compatible endpoints (/ai/{slug}/v1 and
// the unified /v1 router) do: through the langchaingo vendor drivers, which
// call /llm/call/{slug}/... in the LLM's native API. The gateway runs the
// plugin's hooks as the edge does (auth_hooks.go and proxy.go):
//
//   - post_auth: a blocked response is written with the plugin's headers,
//     status and body, as they are;
//   - a JSON response goes through OnBeforeWrite;
//   - a streamed response is relayed, then OnStreamComplete gets the whole
//     stream with the vendor only in Context.Metadata["vendor"].
//
// Each fake upstream answers "reply to: <prompt>", so a response served from
// the wrong cache entry shows up as the wrong text.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
	"github.com/TykTechnologies/midsommar/v2/third_party/langchaingo/llms"
	"github.com/TykTechnologies/midsommar/v2/third_party/langchaingo/llms/anthropic"
	"github.com/TykTechnologies/midsommar/v2/third_party/langchaingo/llms/openai"
)

// ---------------------------------------------------------------------------
// Fake vendor upstreams
// ---------------------------------------------------------------------------

type fakeUpstream struct {
	*httptest.Server
	calls atomic.Int32
}

func lastUserText(messages []interface{}) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m, _ := messages[i].(map[string]interface{})
		if m == nil || m["role"] != "user" {
			continue
		}
		switch c := m["content"].(type) {
		case string:
			return c
		case []interface{}:
			for _, part := range c {
				if p, ok := part.(map[string]interface{}); ok {
					if t, ok := p["text"].(string); ok {
						return t
					}
				}
			}
		}
	}
	return ""
}

func writeSSE(w http.ResponseWriter, events []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	for _, e := range events {
		io.WriteString(w, e)
		io.WriteString(w, "\n\n")
	}
}

func sseEvent(name string, data interface{}) string {
	b, _ := json.Marshal(data)
	if name == "" {
		return "data: " + string(b)
	}
	return "event: " + name + "\ndata: " + string(b)
}

func wantsTool(prompt string) bool { return strings.Contains(prompt, "use the weather tool") }

// newAnthropicUpstream serves POST /v1/messages.
func newAnthropicUpstream(t *testing.T) *fakeUpstream {
	u := &fakeUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs, _ := req["messages"].([]interface{})
		prompt := lastUserText(msgs)
		text := "reply to: " + prompt
		model, _ := req["model"].(string)
		tool := wantsTool(prompt)
		if stream, _ := req["stream"].(bool); stream {
			events := []string{
				sseEvent("message_start", map[string]interface{}{"type": "message_start", "message": map[string]interface{}{
					"id": "msg_1", "type": "message", "role": "assistant", "model": model, "content": []interface{}{},
					"stop_reason": nil, "stop_sequence": nil, "usage": map[string]interface{}{"input_tokens": 7, "output_tokens": 1}}}),
			}
			stop := "end_turn"
			if tool {
				stop = "tool_use"
				events = append(events,
					sseEvent("content_block_start", map[string]interface{}{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]interface{}{}}}),
					sseEvent("content_block_delta", map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "input_json_delta", "partial_json": `{"city":`}}),
					sseEvent("content_block_delta", map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "input_json_delta", "partial_json": `"Paris"}`}}),
					sseEvent("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": 0}),
				)
			} else {
				half := len(text) / 2
				events = append(events,
					sseEvent("content_block_start", map[string]interface{}{"type": "content_block_start", "index": 0, "content_block": map[string]interface{}{"type": "text", "text": ""}}),
					sseEvent("ping", map[string]interface{}{"type": "ping"}),
					sseEvent("content_block_delta", map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": text[:half]}}),
					sseEvent("content_block_delta", map[string]interface{}{"type": "content_block_delta", "index": 0, "delta": map[string]interface{}{"type": "text_delta", "text": text[half:]}}),
					sseEvent("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": 0}),
				)
			}
			events = append(events,
				sseEvent("message_delta", map[string]interface{}{"type": "message_delta", "delta": map[string]interface{}{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]interface{}{"output_tokens": 5}}),
				sseEvent("message_stop", map[string]interface{}{"type": "message_stop"}),
			)
			writeSSE(w, events)
			return
		}
		content := []interface{}{map[string]interface{}{"type": "text", "text": text}}
		stop := "end_turn"
		if tool {
			content = []interface{}{map[string]interface{}{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]interface{}{"city": "Paris"}}}
			stop = "tool_use"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "msg_1", "type": "message", "role": "assistant", "model": model,
			"content": content, "stop_reason": stop, "stop_sequence": nil, "usage": map[string]interface{}{"input_tokens": 7, "output_tokens": 5}})
	}))
	t.Cleanup(u.Close)
	return u
}

// newOpenAIUpstream serves POST /v1/chat/completions.
func newOpenAIUpstream(t *testing.T) *fakeUpstream {
	u := &fakeUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		msgs, _ := req["messages"].([]interface{})
		prompt := lastUserText(msgs)
		text := "reply to: " + prompt
		model, _ := req["model"].(string)
		tool := wantsTool(prompt)
		if stream, _ := req["stream"].(bool); stream {
			chunk := func(delta map[string]interface{}, finish interface{}) string {
				return sseEvent("", map[string]interface{}{"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 1, "model": model,
					"choices": []interface{}{map[string]interface{}{"index": 0, "delta": delta, "finish_reason": finish}}})
			}
			var events []string
			if tool {
				events = []string{
					chunk(map[string]interface{}{"role": "assistant", "tool_calls": []interface{}{map[string]interface{}{"index": 0, "id": "call_1", "type": "function", "function": map[string]interface{}{"name": "get_weather", "arguments": ""}}}}, nil),
					chunk(map[string]interface{}{"tool_calls": []interface{}{map[string]interface{}{"index": 0, "function": map[string]interface{}{"arguments": `{"city":"Paris"}`}}}}, nil),
					chunk(map[string]interface{}{}, "tool_calls"),
				}
			} else {
				half := len(text) / 2
				events = []string{
					chunk(map[string]interface{}{"role": "assistant", "content": ""}, nil),
					chunk(map[string]interface{}{"content": text[:half]}, nil),
					chunk(map[string]interface{}{"content": text[half:]}, nil),
					chunk(map[string]interface{}{}, "stop"),
				}
			}
			events = append(events, "data: [DONE]")
			writeSSE(w, events)
			return
		}
		message := map[string]interface{}{"role": "assistant", "content": text}
		finish := "stop"
		if tool {
			message = map[string]interface{}{"role": "assistant", "content": nil, "tool_calls": []interface{}{map[string]interface{}{
				"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "get_weather", "arguments": `{"city":"Paris"}`}}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": model,
			"choices": []interface{}{map[string]interface{}{"index": 0, "message": message, "finish_reason": finish}},
			"usage":   map[string]interface{}{"prompt_tokens": 7, "completion_tokens": 5, "total_tokens": 12}})
	}))
	t.Cleanup(u.Close)
	return u
}

// newGeminiUpstream serves POST /v1beta/models/{model}:generateContent and
// :streamGenerateContent?alt=sse. The model is only in the path.
func newGeminiUpstream(t *testing.T) *fakeUpstream {
	u := &fakeUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		var req struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		prompt := ""
		if n := len(req.Contents); n > 0 && len(req.Contents[n-1].Parts) > 0 {
			prompt = req.Contents[n-1].Parts[0].Text
		}
		model := strings.TrimPrefix(r.URL.Path, "/v1beta/models/")
		model = model[:strings.Index(model, ":")]
		text := "reply to: " + prompt + " (" + model + ")"
		candidate := func(t string, finish string) map[string]interface{} {
			c := map[string]interface{}{"content": map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": t}}}, "index": 0}
			if finish != "" {
				c["finishReason"] = finish
			}
			return c
		}
		usage := map[string]interface{}{"promptTokenCount": 7, "candidatesTokenCount": 5, "totalTokenCount": 12}
		if strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
			half := len(text) / 2
			writeSSE(w, []string{
				sseEvent("", map[string]interface{}{"candidates": []interface{}{candidate(text[:half], "")}}),
				sseEvent("", map[string]interface{}{"candidates": []interface{}{candidate(text[half:], "STOP")}, "usageMetadata": usage}),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"candidates": []interface{}{candidate(text, "STOP")}, "usageMetadata": usage})
	}))
	t.Cleanup(u.Close)
	return u
}

// ---------------------------------------------------------------------------
// Stand-in edge gateway
// ---------------------------------------------------------------------------

type gatewayLLM struct {
	id       uint32
	vendor   string
	upstream *fakeUpstream
}

type testGateway struct {
	*httptest.Server
	plugin *LLMCachePlugin
	llms   map[string]gatewayLLM
	seq    atomic.Int64
	// streamDone closes when OnStreamComplete has run for the latest stream;
	// the real gateway runs it in the background after the stream ends.
	mu         sync.Mutex
	streamDone chan struct{}
}

func newCachePlugin(t *testing.T) *LLMCachePlugin {
	t.Helper()
	p := NewLLMCachePlugin()
	if err := p.Initialize(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio}, map[string]string{}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return p
}

func newTestGateway(t *testing.T, p *LLMCachePlugin, llmsBySlug map[string]gatewayLLM) *testGateway {
	g := &testGateway{plugin: p, llms: llmsBySlug}
	g.Server = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.Close)
	return g
}

func (g *testGateway) serve(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/llm/call/")
	slug, upstreamPath, _ := strings.Cut(rest, "/")
	llm, ok := g.llms[slug]
	if !ok {
		http.Error(w, "unknown llm", http.StatusNotFound)
		return
	}
	upstreamPath = "/" + upstreamPath
	body, _ := io.ReadAll(r.Body)
	requestID := fmt.Sprintf("req-%d", g.seq.Add(1))
	headers := map[string]string{}
	for k, v := range r.Header {
		headers[k] = v[0]
	}
	pctx := &pb.PluginContext{RequestId: requestID, Vendor: llm.vendor, LlmId: llm.id, LlmSlug: slug, AppId: 1, Metadata: map[string]string{}}
	sdkCtx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, RequestID: requestID, AppID: 1, LLMID: llm.id, LLMSlug: slug, Vendor: llm.vendor}

	resp, err := g.plugin.HandlePostAuth(sdkCtx, &pb.EnrichedRequest{
		Request:       &pb.PluginRequest{Method: r.Method, Path: r.URL.Path, Headers: headers, Body: body, Context: pctx},
		AppId:         "1",
		Authenticated: true,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if resp.Block {
		// As microgateway/internal/api/auth_hooks.go writes a blocked response.
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(int(resp.StatusCode))
		w.Write(resp.Body)
		return
	}

	target := llm.upstream.URL + upstreamPath
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	upResp, err := http.Post(target, "application/json", bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer upResp.Body.Close()
	upBody, _ := io.ReadAll(upResp.Body)
	upHeaders := map[string]string{}
	for k, v := range upResp.Header {
		upHeaders[k] = v[0]
	}

	if strings.Contains(upResp.Header.Get("Content-Type"), "text/event-stream") {
		w.Header().Set("Content-Type", upResp.Header.Get("Content-Type"))
		w.WriteHeader(upResp.StatusCode)
		w.Write(upBody)
		// The stream-complete context carries the vendor in metadata only
		// (proxy.go executeOnStreamComplete).
		_, _ = g.plugin.OnStreamComplete(sdkCtx, &pb.StreamCompleteRequest{
			AccumulatedResponse: upBody,
			Headers:             upHeaders,
			StatusCode:          int32(upResp.StatusCode),
			Context:             &pb.PluginContext{RequestId: requestID, LlmId: llm.id, LlmSlug: slug, AppId: 1, Metadata: map[string]string{"vendor": llm.vendor}},
			RequestBody:         body,
		})
		return
	}

	out, err := g.plugin.OnBeforeWrite(sdkCtx, &pb.ResponseWriteRequest{Body: upBody, Headers: upHeaders, Context: pctx})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for k, v := range out.Headers {
		if !strings.EqualFold(k, "Content-Length") {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(upResp.StatusCode)
	w.Write(out.Body)
}

func (g *testGateway) post(t *testing.T, path string, body interface{}) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(g.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s response: %v (Content-Length %q)", resp.Request.URL.Path, err, resp.Header.Get("Content-Length"))
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// The OpenAI-compatible endpoints' view: langchaingo drivers
// ---------------------------------------------------------------------------

type driverCall struct {
	text      string
	toolCalls []llms.ToolCall
	err       error
}

func generate(model llms.Model, prompt string, stream bool, tools []llms.Tool) driverCall {
	var opts []llms.CallOption
	var streamed strings.Builder
	if stream {
		opts = append(opts, llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			streamed.Write(chunk)
			return nil
		}))
	}
	if len(tools) > 0 {
		opts = append(opts, llms.WithTools(tools))
	}
	resp, err := model.GenerateContent(context.Background(), []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, prompt)}, opts...)
	if err != nil {
		return driverCall{err: err}
	}
	if len(resp.Choices) == 0 {
		return driverCall{err: fmt.Errorf("no choices")}
	}
	return driverCall{text: resp.Choices[0].Content, toolCalls: resp.Choices[0].ToolCalls}
}

var weatherTool = []llms.Tool{{Type: "function", Function: &llms.FunctionDefinition{
	Name: "get_weather", Description: "weather for a city",
	Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
}}}

type vendorCase struct {
	name     string
	vendor   string
	upstream func(*testing.T) *fakeUpstream
	driver   func(t *testing.T, gatewayURL string) llms.Model
}

var driverVendors = []vendorCase{
	{"anthropic", "anthropic", newAnthropicUpstream, func(t *testing.T, gw string) llms.Model {
		m, err := anthropic.New(anthropic.WithBaseURL(gw+"/llm/call/model-a/v1"), anthropic.WithToken("k"), anthropic.WithModel("claude-test"))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}},
	{"openai", "openai", newOpenAIUpstream, func(t *testing.T, gw string) llms.Model {
		m, err := openai.New(openai.WithBaseURL(gw+"/llm/call/model-a/v1"), openai.WithToken("k"), openai.WithModel("gpt-test"))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}},
}

// A cached answer must reach the OpenAI-compatible endpoints' driver exactly
// as the upstream's did, whichever of JSON and streaming stored it and
// whichever asks for it: the shim streams from the upstream even for a JSON
// client, so every hit it sees was stored from a stream.
func TestShimDriverGetsTheCachedAnswer(t *testing.T) {
	modes := map[bool]string{false: "json", true: "stream"}
	for _, vc := range driverVendors {
		for _, storeStream := range []bool{false, true} {
			for _, hitStream := range []bool{false, true} {
				name := fmt.Sprintf("%s/stored_%s/hit_%s", vc.name, modes[storeStream], modes[hitStream])
				t.Run(name, func(t *testing.T) {
					up := vc.upstream(t)
					gw := newTestGateway(t, newCachePlugin(t), map[string]gatewayLLM{"model-a": {1, vc.vendor, up}})
					model := vc.driver(t, gw.URL)
					prompt := "what is the capital of France? " + name

					first := generate(model, prompt, storeStream, nil)
					if first.err != nil {
						t.Fatalf("miss: %v", first.err)
					}
					second := generate(model, prompt, hitStream, nil)
					if second.err != nil {
						t.Fatalf("hit: %v", second.err)
					}
					if second.text != first.text {
						t.Fatalf("hit text = %q, want %q", second.text, first.text)
					}
					if got := up.calls.Load(); got != 1 {
						t.Fatalf("upstream calls = %d, want 1 (the second request should be a cache hit)", got)
					}
				})
			}
		}
	}
}

// A tool call must not come back from the cache without its tool call (the
// stream reconstruction only kept text): either the call is replayed intact
// or the request goes to the upstream.
func TestShimDriverNeverGetsALossyToolCall(t *testing.T) {
	for _, vc := range driverVendors {
		for _, storeStream := range []bool{false, true} {
			for _, hitStream := range []bool{false, true} {
				name := fmt.Sprintf("%s/stored_stream=%v/hit_stream=%v", vc.name, storeStream, hitStream)
				t.Run(name, func(t *testing.T) {
					up := vc.upstream(t)
					gw := newTestGateway(t, newCachePlugin(t), map[string]gatewayLLM{"model-a": {1, vc.vendor, up}})
					model := vc.driver(t, gw.URL)
					prompt := "please use the weather tool for Paris " + name

					first := generate(model, prompt, storeStream, weatherTool)
					if first.err != nil {
						t.Fatalf("miss: %v", first.err)
					}
					second := generate(model, prompt, hitStream, weatherTool)
					if second.err != nil {
						t.Fatalf("second request: %v", second.err)
					}
					if len(second.toolCalls) != 1 || second.toolCalls[0].FunctionCall == nil ||
						second.toolCalls[0].FunctionCall.Name != "get_weather" ||
						!jsonEqual(second.toolCalls[0].FunctionCall.Arguments, `{"city":"Paris"}`) {
						t.Fatalf("second request tool calls = %+v, want get_weather({\"city\":\"Paris\"}) (upstream calls %d)", second.toolCalls, up.calls.Load())
					}
				})
			}
		}
	}
}

func jsonEqual(a, b string) bool {
	var x, y interface{}
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return bytes.Equal(xb, yb)
}

// ---------------------------------------------------------------------------
// Native clients: headers and shapes on a hit
// ---------------------------------------------------------------------------

// A hit's framing headers must describe the body it carries: a stored
// Content-Length or Content-Type from the other format empties or mislabels it.
func TestHitHeadersDescribeTheBody(t *testing.T) {
	up := newAnthropicUpstream(t)
	gw := newTestGateway(t, newCachePlugin(t), map[string]gatewayLLM{"model-a": {1, "anthropic", up}})
	req := func(stream bool) map[string]interface{} {
		return map[string]interface{}{"model": "claude-test", "max_tokens": 64, "stream": stream,
			"messages": []interface{}{map[string]interface{}{"role": "user", "content": "headers probe"}}}
	}

	// Stored from a stream.
	readAll(t, gw.post(t, "/llm/call/model-a/v1/messages", req(true)))

	jsonHit := gw.post(t, "/llm/call/model-a/v1/messages", req(false))
	body := readAll(t, jsonHit)
	if ct := jsonHit.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("JSON hit Content-Type = %q, want application/json", ct)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(body), &msg); err != nil || msg["type"] != "message" {
		t.Errorf("JSON hit body is not an Anthropic message: %v %q", err, body)
	}

	streamHit := gw.post(t, "/llm/call/model-a/v1/messages", req(true))
	body = readAll(t, streamHit)
	if ct := streamHit.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("stream hit Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(body, "event: message_stop") || !strings.Contains(body, "reply to: headers probe") {
		t.Errorf("stream hit body is not the Anthropic stream: %q", body)
	}
	if got := up.calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}
}

// Two LLMs that see the same model name and messages must not share entries:
// an OpenAI-compatible LLM and an Anthropic LLM speak different formats, and
// even two LLMs of one vendor are different upstreams.
func TestEntriesAreScopedToTheLLM(t *testing.T) {
	oa, an := newOpenAIUpstream(t), newAnthropicUpstream(t)
	gw := newTestGateway(t, newCachePlugin(t), map[string]gatewayLLM{
		"compat": {1, "openai", oa},
		"claude": {2, "anthropic", an},
	})
	// The very same body to both, so only the LLM tells them apart.
	req := map[string]interface{}{"model": "shared-model", "max_tokens": 64,
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "same question"}}}
	readAll(t, gw.post(t, "/llm/call/compat/v1/chat/completions", req))

	resp := gw.post(t, "/llm/call/claude/v1/messages", req)
	body := readAll(t, resp)
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(body), &msg); err != nil || msg["type"] != "message" {
		t.Fatalf("Anthropic LLM answered with %q, want an Anthropic message", body)
	}
	if an.calls.Load() != 1 {
		t.Fatalf("Anthropic upstream calls = %d, want 1", an.calls.Load())
	}
}

// Request fields that change the answer's shape are part of the key.
func TestShapeParametersArePartOfTheKey(t *testing.T) {
	msgs := []interface{}{map[string]interface{}{"role": "user", "content": "shape probe"}}
	base := map[string]interface{}{"model": "gpt-test", "messages": msgs}
	variants := map[string]map[string]interface{}{
		"response_format": {"response_format": map[string]interface{}{"type": "json_object"}},
		"n":               {"n": 2},
		"max_tokens":      {"max_tokens": 5},
		"top_p":           {"top_p": 0.1},
		"stop":            {"stop": []interface{}{"."}},
		"tool_choice":     {"tool_choice": "none"},
	}
	for name, extra := range variants {
		t.Run(name, func(t *testing.T) {
			up := newOpenAIUpstream(t)
			gw := newTestGateway(t, newCachePlugin(t), map[string]gatewayLLM{"model-a": {1, "openai", up}})
			readAll(t, gw.post(t, "/llm/call/model-a/v1/chat/completions", base))
			variant := map[string]interface{}{}
			for k, v := range base {
				variant[k] = v
			}
			for k, v := range extra {
				variant[k] = v
			}
			readAll(t, gw.post(t, "/llm/call/model-a/v1/chat/completions", variant))
			if got := up.calls.Load(); got != 2 {
				t.Fatalf("upstream calls = %d, want 2: a request with %s was served another request's answer", got, name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Gemini: the prompt is in "contents", the model and streaming in the path
// ---------------------------------------------------------------------------

func geminiBody(prompt string) map[string]interface{} {
	return map[string]interface{}{"contents": []interface{}{map[string]interface{}{"role": "user", "parts": []interface{}{map[string]interface{}{"text": prompt}}}}}
}

func geminiText(t *testing.T, body string) string {
	t.Helper()
	var r struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil || len(r.Candidates) == 0 || len(r.Candidates[0].Content.Parts) == 0 {
		t.Fatalf("not a Gemini response: %v %q", err, body)
	}
	return r.Candidates[0].Content.Parts[0].Text
}

func TestGeminiPromptsAndModelsDoNotShareEntries(t *testing.T) {
	up := newGeminiUpstream(t)
	gw := newTestGateway(t, newCachePlugin(t), map[string]gatewayLLM{"gem": {1, "google_ai", up}})
	ask := func(model, prompt string) string {
		return geminiText(t, readAll(t, gw.post(t, "/llm/call/gem/v1beta/models/"+model+":generateContent", geminiBody(prompt))))
	}
	if got := ask("gemini-a", "first question"); got != "reply to: first question (gemini-a)" {
		t.Fatalf("first answer %q", got)
	}
	if got := ask("gemini-a", "second question"); got != "reply to: second question (gemini-a)" {
		t.Fatalf("a different prompt got %q", got)
	}
	if got := ask("gemini-b", "first question"); got != "reply to: first question (gemini-b)" {
		t.Fatalf("a different model got %q", got)
	}
	if got := ask("gemini-a", "first question"); got != "reply to: first question (gemini-a)" {
		t.Fatalf("repeat got %q", got)
	}
	if got := up.calls.Load(); got != 3 {
		t.Fatalf("upstream calls = %d, want 3 (the repeat is a hit)", got)
	}
}

func TestGeminiStreamingHitIsAStream(t *testing.T) {
	up := newGeminiUpstream(t)
	gw := newTestGateway(t, newCachePlugin(t), map[string]gatewayLLM{"gem": {1, "google_ai", up}})
	streamPath := "/llm/call/gem/v1beta/models/gemini-a:streamGenerateContent?alt=sse"
	readAll(t, gw.post(t, streamPath, geminiBody("stream me")))

	resp := gw.post(t, streamPath, geminiBody("stream me"))
	body := readAll(t, resp)
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}
	var text strings.Builder
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") {
			text.WriteString(geminiText(t, strings.TrimPrefix(line, "data: ")))
		}
	}
	if text.String() != "reply to: stream me (gemini-a)" {
		t.Fatalf("streamed text %q from body %q", text.String(), body)
	}
	if got := up.calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}

	// The same prompt as JSON is served from the stream's entry, as JSON.
	resp = gw.post(t, "/llm/call/gem/v1beta/models/gemini-a:generateContent", geminiBody("stream me"))
	body = readAll(t, resp)
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || geminiText(t, body) != "reply to: stream me (gemini-a)" {
		t.Fatalf("JSON after stream: %q %q", resp.Header.Get("Content-Type"), body)
	}
}

// ---------------------------------------------------------------------------
// Vendors the cache cannot convert between formats
// ---------------------------------------------------------------------------

// Ollama streams unless the request says "stream": false, and its stream is
// NDJSON, which the cache cannot produce: a stored JSON answer must not be
// handed to a streaming request.
func TestOllamaStreamingRequestIsNotServedJSON(t *testing.T) {
	p := newCachePlugin(t)
	body := []byte(`{"model":"llama3","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	pctxFor := func(id string) *pb.PluginContext {
		return &pb.PluginContext{RequestId: id, Vendor: "ollama", LlmId: 1, LlmSlug: "llama", AppId: 1, Metadata: map[string]string{}}
	}
	pctx := pctxFor("r1")
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, RequestID: "r1", AppID: 1, LLMID: 1, LLMSlug: "llama", Vendor: "ollama"}
	req := func(id string, b []byte) *pb.EnrichedRequest {
		return &pb.EnrichedRequest{Request: &pb.PluginRequest{Method: "POST", Path: "/llm/call/llama/api/chat", Body: b, Context: pctxFor(id)}}
	}
	if resp, _ := p.HandlePostAuth(ctx, req("r1", body)); resp.Block {
		t.Fatal("first request was a hit")
	}
	stored := []byte(`{"model":"llama3","message":{"role":"assistant","content":"hello"},"done":true}`)
	p.OnBeforeWrite(ctx, &pb.ResponseWriteRequest{Body: stored, Headers: map[string]string{"Content-Type": "application/json"}, Context: pctx})

	// Same request, streaming by default.
	streaming := []byte(`{"model":"llama3","messages":[{"role":"user","content":"hi"}]}`)
	if resp, _ := p.HandlePostAuth(ctx, req("r2", streaming)); resp.Block {
		t.Fatalf("a streaming Ollama request was answered from a JSON entry: %q (%v)", resp.Body, resp.Headers)
	}
	// And the JSON request is still a hit.
	if resp, _ := p.HandlePostAuth(ctx, req("r3", body)); !resp.Block || string(resp.Body) != string(stored) {
		t.Fatalf("the JSON request was not served its entry: block=%v", resp.Block)
	}
}

// Two LLMs of one vendor are still two upstreams (another account, region or
// fine-tune): one's answer is not the other's.
func TestEntriesAreScopedToTheLLMWithinAVendor(t *testing.T) {
	first, second := newOpenAIUpstream(t), newOpenAIUpstream(t)
	gw := newTestGateway(t, newCachePlugin(t), map[string]gatewayLLM{
		"east": {1, "openai", first},
		"west": {2, "openai", second},
	})
	req := map[string]interface{}{"model": "gpt-test", "messages": []interface{}{map[string]interface{}{"role": "user", "content": "same question"}}}
	readAll(t, gw.post(t, "/llm/call/east/v1/chat/completions", req))
	readAll(t, gw.post(t, "/llm/call/west/v1/chat/completions", req))
	if second.calls.Load() != 1 {
		t.Fatalf("second LLM's upstream calls = %d, want 1: it was served the first LLM's answer", second.calls.Load())
	}
}
