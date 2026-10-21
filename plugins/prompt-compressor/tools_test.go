package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// A request's tools reach its record and the edge's batches as names and
// counts only: no schema text, description or argument leaves the edge.
func TestToolsInRecordsCarryNoSchemaOrArguments(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"audit"}`)
	rec := &recorded{}
	p.SetRecorder(rec)
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"go"}],"tools":[` +
		`{"type":"function","function":{"name":"read_file","description":"SCHEMA-CANARY reads","parameters":{"type":"object","properties":{"CANARY_PARAM":{"type":"string"}}}}},` +
		`{"type":"function","function":{"name":"never_used","parameters":{}}},` +
		`{"type":"function","function":{"name":"bad name with spaces","parameters":{}}}]}`)
	p.HandlePostAuth(ctx, request(body, nil))
	resp := `{"choices":[{"message":{"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"ARG-CANARY\"}"}}]}}],"usage":{"prompt_tokens":100}}`
	p.OnBeforeWrite(ctx, &pb.ResponseWriteRequest{Body: []byte(resp)})
	if len(rec.reqs) != 1 {
		t.Fatalf("%d records", len(rec.reqs))
	}
	r := rec.reqs[0]
	if r.ToolDefs != 3 || r.ToolDefTokens <= 0 || len(r.ToolListHash) != toolListHashLen || !reflect.DeepEqual(r.CalledTools, []string{"read_file"}) {
		t.Fatalf("record %+v", r)
	}

	agg := records.NewAggregator(func(vendor, model string) (records.Price, bool) {
		return records.Price{Input: 2e-6, CacheRead: 2e-7, Currency: "USD"}, true
	})
	agg.Add(r)
	batches := agg.Flush()
	raw, _ := json.Marshal(batches)
	for _, canary := range []string{"CANARY", "reads", "properties", "bad name"} {
		if strings.Contains(string(raw), canary) {
			t.Errorf("the batch carries %q: %s", canary, raw)
		}
	}
	d := batches[0].Deltas[0]
	if d.ToolRequests != 1 || d.ToolDefs != 3 || d.Tools["read_file"].Called != 1 || d.Tools["never_used"].Offered != 1 ||
		d.Tools["never_used"].Called != 0 || d.Tools[records.OtherTool].Offered != 1 || d.ToolDefCost["USD"] <= 0 || d.ToolDefCostAtCacheRead["USD"] <= 0 {
		t.Errorf("delta %+v", d)
	}
	if err := batches[0].Validate(); err != nil {
		t.Error(err)
	}
}

const schema = `{"type":"object","properties":{"path":{"type":"string","description":"the file to read"}},"required":["path"]}`

func TestParseOfferedTools(t *testing.T) {
	for _, tc := range []struct {
		format, body string
		names        []string
	}{
		{"openai", `{"model":"gpt-4o","tools":[{"type":"function","function":{"name":"read_file","description":"Read a file","parameters":` + schema + `}},{"type":"function","function":{"name":"shell","parameters":{}}}]}`, []string{"read_file", "shell"}},
		{"ollama", `{"model":"llama3","tools":[{"type":"function","function":{"name":"read_file","parameters":` + schema + `}}]}`, []string{"read_file"}},
		{"anthropic", `{"model":"claude","tools":[{"name":"read_file","description":"Read","input_schema":` + schema + `},{"type":"web_search_20250305","name":"web_search","max_uses":3}]}`, []string{"read_file", "web_search"}},
		{"openai_responses", `{"model":"gpt-5","tools":[{"type":"function","name":"read_file","parameters":` + schema + `},{"type":"web_search"}]}`, []string{"read_file", "web_search"}},
		{"google_ai", `{"contents":[],"tools":[{"functionDeclarations":[{"name":"read_file","parameters":` + schema + `},{"name":"shell"}]},{"function_declarations":[{"name":"grep"}]}]}`, []string{"read_file", "shell", "grep"}},
		{"bedrock", `{"messages":[],"toolConfig":{"tools":[{"toolSpec":{"name":"read_file","inputSchema":{"json":` + schema + `}}},{"cachePoint":{"type":"default"}}]}}`, []string{"read_file"}},
	} {
		info := parseOfferedTools(tc.format, []byte(tc.body))
		var names []string
		var sum int64
		for _, tool := range info.Tools {
			names = append(names, tool.Name)
			if tool.Tokens <= 0 {
				t.Errorf("%s %s: no size", tc.format, tool.Name)
			}
			sum += tool.Tokens
		}
		if !reflect.DeepEqual(names, tc.names) || sum != info.Tokens || len(info.Hash) != toolListHashLen {
			t.Errorf("%s: %+v", tc.format, info)
		}
	}
	if info := parseOfferedTools("openai", []byte(`{"messages":[]}`)); len(info.Tools) != 0 || info.Hash != "" {
		t.Errorf("no tools: %+v", info)
	}
	if info := parseOfferedTools("openai", []byte(`not json`)); len(info.Tools) != 0 {
		t.Errorf("bad body: %+v", info)
	}
}

// The hash follows the tool list, not its formatting.
func TestToolListHash(t *testing.T) {
	a := parseOfferedTools("anthropic", []byte(`{"tools":[{"name":"a","input_schema":{}},{"name":"b","input_schema":{}}]}`))
	spaced := parseOfferedTools("anthropic", []byte(`{"tools":[ {"name": "a", "input_schema": {}}, {"name":"b","input_schema":{}} ]}`))
	reordered := parseOfferedTools("anthropic", []byte(`{"tools":[{"name":"b","input_schema":{}},{"name":"a","input_schema":{}}]}`))
	changed := parseOfferedTools("anthropic", []byte(`{"tools":[{"name":"a","input_schema":{"x":1}},{"name":"b","input_schema":{}}]}`))
	if a.Hash != spaced.Hash || a.Hash == reordered.Hash || a.Hash == changed.Hash {
		t.Errorf("hashes %s %s %s %s", a.Hash, spaced.Hash, reordered.Hash, changed.Hash)
	}
}

func TestParseCalledToolsBuffered(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want []string
	}{
		"openai":    {`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"shell","arguments":"{\"cmd\":\"ls\"}"}},{"id":"c2","type":"function","function":{"name":"read_file","arguments":"{}"}}]}}]}`, []string{"read_file", "shell"}},
		"anthropic": {`{"content":[{"type":"text","text":"calling tool_use now"},{"type":"tool_use","id":"t1","name":"read_file","input":{"path":"a"}},{"type":"server_tool_use","id":"s1","name":"web_search","input":{}}]}`, []string{"read_file", "web_search"}},
		"responses": {`{"output":[{"type":"reasoning","summary":[]},{"type":"function_call","name":"shell","call_id":"c1","arguments":"{}"},{"type":"message","content":[{"type":"output_text","text":"\"name\":\"fake\""}]}]}`, []string{"shell"}},
		"google":    {`{"candidates":[{"content":{"parts":[{"text":"ok"},{"functionCall":{"name":"grep","args":{"q":"x"}}}]}}]}`, []string{"grep"}},
		"bedrock":   {`{"output":{"message":{"role":"assistant","content":[{"text":"x"},{"toolUse":{"toolUseId":"t","name":"read_file","input":{}}}]}},"stopReason":"tool_use"}`, []string{"read_file"}},
		"none":      {`{"choices":[{"message":{"content":"no tools"}}]}`, nil},
	} {
		if got := parseCalledTools([]byte(tc.body)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestParseCalledToolsStreaming(t *testing.T) {
	sse := func(events ...string) string {
		var b strings.Builder
		for _, e := range events {
			b.WriteString("data: " + e + "\n\n")
		}
		return b.String()
	}
	for name, tc := range map[string]struct {
		stream string
		want   []string
	}{
		"openai": {sse(
			`{"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"shell","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"read_file","arguments":"{}"}}]}}]}`,
			`[DONE]`), []string{"read_file", "shell"}},
		"anthropic": {"event: content_block_start\n" + sse(
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"read_file","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"name\":\"x\"}"}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`), []string{"read_file"}},
		"responses": {sse(
			`{"type":"response.output_item.added","item":{"type":"function_call","name":"shell","call_id":"c1","arguments":""}}`,
			`{"type":"response.function_call_arguments.delta","delta":"{\"name\":\"fake\"}"}`,
			`{"type":"response.completed","response":{"output":[{"type":"function_call","name":"shell"},{"type":"function_call","name":"grep"}]}}`), []string{"grep", "shell"}},
		"google sse": {sse(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"grep","args":{}}}]}}]}`), []string{"grep"}},
		"google array": {`[{"candidates":[{"content":{"parts":[{"text":"a"}]}}]},
{"candidates":[{"content":{"parts":[{"functionCall":{"name":"grep","args":{}}}]}}]}]`, []string{"grep"}},
		"bedrock": {"\x00\x00\x00\x9b\x00\x00\x00K:event-type\x07\x00\x11contentBlockStart" +
			`{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"t1","name":"read_file"}}}` +
			"\x00\x00\x00\x50" + `{"contentBlockIndex":0,"delta":{"text":"I will call \"toolUse\":{\"name\":\"fake\"}"}}`, []string{"read_file"}},
	} {
		if got := parseStreamCalledTools([]byte(tc.stream)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}
