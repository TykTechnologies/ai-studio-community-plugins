package corpus_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
	"github.com/TykTechnologies/midsommar/v2/guardrails"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

func load(t *testing.T) corpus.Manifest {
	t.Helper()
	m, err := corpus.Load(corpus.Dir())
	if err != nil {
		t.Fatalf("load corpus: %v (regenerate with go generate ./internal/corpus)", err)
	}
	return m
}

// The committed files are exactly what the generator writes, so a change to
// the generator that is not regenerated (or a hand edit) fails here.
func TestCorpusMatchesGenerator(t *testing.T) {
	got := load(t)
	want := corpus.Build()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("manifest.json is stale: run go generate ./internal/corpus")
	}
	for i, e := range want.Entries {
		if !bytes.Equal(got.Entries[i].Body(), e.Body()) {
			t.Errorf("%s: body differs from the generator: run go generate ./internal/corpus", e.Name)
		}
	}
	for i, s := range want.Sessions {
		gb, wb := got.Sessions[i].Bodies(), s.Bodies()
		if len(gb) != len(wb) {
			t.Errorf("%s: %d turns, generator makes %d", s.Name, len(gb), len(wb))
			continue
		}
		for k := range wb {
			if !bytes.Equal(gb[k], wb[k]) {
				t.Errorf("%s turn %d differs from the generator: run go generate ./internal/corpus", s.Name, k+1)
			}
		}
	}
}

// Every body either reads with segments.ExtractUnits, with each tool output
// it promises present intact in a unit of the right kind, or is refused the
// way a pass-through must be.
func TestEntriesParse(t *testing.T) {
	m := load(t)
	for _, e := range m.Entries {
		t.Run(e.Name, func(t *testing.T) {
			if !json.Valid(e.Body()) {
				t.Fatal("body is not valid JSON")
			}
			units, err := segments.ExtractUnits(e.Format, e.Body())
			switch {
			case e.Expect == corpus.ExpectSkip:
				if err == nil {
					t.Fatalf("pass-through body read by ExtractUnits(%q): %d units", e.Format, len(units))
				}
				if !errors.Is(err, segments.ErrUnsupportedFormat) && !errors.Is(err, segments.ErrUnrecognisedBody) {
					t.Fatalf("pass-through body refused with %v, want unsupported format or unrecognised body", err)
				}
				generic, gerr := segments.Generic(e.Body())
				if gerr != nil {
					t.Fatalf("Generic: %v", gerr)
				}
				checkMarkers(t, e, generic, false)
			case !e.Readable(segments.Supports):
				if !errors.Is(err, segments.ErrUnsupportedFormat) {
					t.Fatalf("pending format %q: got %v, want ErrUnsupportedFormat until %s lands", e.Format, err, e.Pending)
				}
				t.Logf("format %s not supported yet (%s); checked once it is", e.Format, e.Pending)
			default:
				if err != nil {
					t.Fatalf("ExtractUnits(%q): %v", e.Format, err)
				}
				checkMarkers(t, e, units, true)
			}
		})
	}
}

func checkMarkers(t *testing.T, e corpus.Entry, units []segments.Unit, strict bool) {
	t.Helper()
	for _, ref := range e.Tools {
		found := false
		for _, u := range units {
			if !containsMarker(u.Text, ref.Marker) {
				continue
			}
			if strict && ref.Via == "tool_result" && u.Kind != guardrails.KindToolResult {
				continue
			}
			found = true
			break
		}
		if !found {
			t.Errorf("%s output (marker %q, via %s) is in no unit", ref.Kind, ref.Marker, ref.Via)
		}
	}
}

// containsMarker also finds a marker inside a JSON string nested in the
// text (Codex wraps shell output as {"output": "..."}).
func containsMarker(text, marker string) bool {
	if strings.Contains(text, marker) {
		return true
	}
	esc, _ := json.Marshal(marker)
	return strings.Contains(text, string(esc[1:len(esc)-1]))
}

// The formats and tool types the bead lists are all covered.
func TestCoverage(t *testing.T) {
	m := load(t)
	formats := map[string]int{}
	kindFormats := map[corpus.Kind]map[string]bool{}
	shapes := map[string]bool{}
	invokeAnthropic, invokeOther := false, false
	for _, e := range m.Entries {
		formats[e.Format]++
		for _, s := range e.Shapes {
			shapes[s] = true
		}
		if e.Format == "bedrock" && strings.HasSuffix(e.Path, "/invoke") {
			if e.Expect == corpus.ExpectExtract {
				invokeAnthropic = true
			} else {
				invokeOther = true
			}
		}
		if e.Expect != corpus.ExpectExtract || e.Pending != "" {
			continue
		}
		for _, ref := range e.Tools {
			if kindFormats[ref.Kind] == nil {
				kindFormats[ref.Kind] = map[string]bool{}
			}
			kindFormats[ref.Kind][e.Format] = true
		}
	}
	for _, f := range []string{"openai", "anthropic", "google_ai", "vertex", "bedrock", "ollama", "openai_responses", "openai_input", "openai_decisions"} {
		if formats[f] == 0 {
			t.Errorf("no entry in format %s", f)
		}
	}
	if !invokeAnthropic || !invokeOther {
		t.Errorf("want Bedrock InvokeModel entries with an Anthropic body and a non-Anthropic one (got %v, %v)", invokeAnthropic, invokeOther)
	}
	for _, k := range corpus.AllKinds {
		if len(kindFormats[k]) < 2 {
			t.Errorf("tool output %s appears in %d formats, want at least 2", k, len(kindFormats[k]))
		}
	}
	for _, s := range []string{
		corpus.ShapeToolResultString, corpus.ShapeToolResultBlocks, corpus.ShapeCacheControlBlock,
		corpus.ShapeCacheControlSystem, corpus.ShapeBedrockCachePoint, corpus.ShapeContextManagement,
		corpus.ShapeCompaction, corpus.ShapeThinking, corpus.ShapeRedactedThinking, corpus.ShapeImage,
		corpus.ShapeBase64TextDocument, corpus.ShapeDeveloperRole, corpus.ShapeEncryptedReasoning,
		corpus.ShapePreviousResponseID, corpus.ShapeMCPResult,
	} {
		if !shapes[s] {
			t.Errorf("no entry has shape %s", s)
		}
	}
	anthropicShapes := map[string]bool{}
	for _, e := range m.Entries {
		if e.Format == "anthropic" {
			for _, s := range e.Shapes {
				anthropicShapes[s] = true
			}
		}
	}
	if !anthropicShapes[corpus.ShapeToolResultString] || !anthropicShapes[corpus.ShapeToolResultBlocks] {
		t.Error("Anthropic needs both tool_result content shapes")
	}
}

// Each shape an entry claims is really in its body.
func TestShapesPresent(t *testing.T) {
	m := load(t)
	for _, e := range m.Entries {
		t.Run(e.Name, func(t *testing.T) {
			doc := decode(t, e.Body())
			for _, s := range e.Shapes {
				if !hasShape(s, doc, e.Body()) {
					t.Errorf("shape %s claimed but not found", s)
				}
			}
			if e.NativeContextManagement != (hasShape(corpus.ShapeContextManagement, doc, e.Body()) || hasShape(corpus.ShapeCompaction, doc, e.Body())) {
				t.Errorf("native_context_management %v disagrees with the body", e.NativeContextManagement)
			}
		})
	}
}

// Tool calls and results pair up one to one.
func TestToolPairing(t *testing.T) {
	m := load(t)
	for _, e := range m.Entries {
		if e.Expect != corpus.ExpectExtract || e.ToolPairing != "paired" {
			continue
		}
		t.Run(e.Name, func(t *testing.T) {
			calls, results := pairs(e.Format, decode(t, e.Body()))
			if !reflect.DeepEqual(calls, results) {
				t.Errorf("calls %v, results %v", calls, results)
			}
		})
	}
}

// The sessions: 30 turns each, at least three formats with full history
// where turn k+1 is turn k plus new messages, and a chained Responses one.
func TestSessions(t *testing.T) {
	m := load(t)
	full := map[string]bool{}
	for _, s := range m.Sessions {
		t.Run(s.Name, func(t *testing.T) {
			bodies := s.Bodies()
			if len(bodies) != 30 || s.Turns != 30 {
				t.Fatalf("%d turns (manifest %d), want 30", len(bodies), s.Turns)
			}
			for k, b := range bodies {
				units, err := segments.ExtractUnits(s.Format, b)
				switch {
				case s.Pending != "" && !segments.Supports(s.Format):
					if !errors.Is(err, segments.ErrUnsupportedFormat) {
						t.Fatalf("turn %d: pending format: %v", k+1, err)
					}
				case err != nil:
					t.Fatalf("turn %d: ExtractUnits: %v", k+1, err)
				case len(units) == 0:
					t.Fatalf("turn %d: no units", k+1)
				}
			}
			switch s.History {
			case "full":
				full[s.Format] = true
				for k := 0; k+1 < len(bodies); k++ {
					checkAppendOnly(t, k+1, s, decode(t, bodies[k]), decode(t, bodies[k+1]))
				}
			case "previous_response_id":
				if len(s.ResponseIDs) != len(bodies) {
					t.Fatalf("%d response ids for %d turns", len(s.ResponseIDs), len(bodies))
				}
				for k, b := range bodies {
					got, _ := decode(t, b).(map[string]any)["previous_response_id"].(string)
					want := ""
					if k > 0 {
						want = s.ResponseIDs[k-1]
					}
					if got != want {
						t.Errorf("turn %d: previous_response_id %q, want %q", k+1, got, want)
					}
				}
			default:
				t.Fatalf("unknown history %q", s.History)
			}
			if s.MovingCacheControl {
				checkMovingCache(t, bodies)
			}
		})
	}
	if len(full) < 3 {
		t.Errorf("full-history sessions in %d formats, want at least 3", len(full))
	}
}

func conversationKey(doc map[string]any) string {
	for _, k := range []string{"messages", "input", "contents"} {
		if _, ok := doc[k].([]any); ok {
			return k
		}
	}
	return ""
}

func checkAppendOnly(t *testing.T, turn int, s corpus.Session, cur, next any) {
	t.Helper()
	c, n := cur.(map[string]any), next.(map[string]any)
	key := conversationKey(c)
	if key == "" || key != conversationKey(n) {
		t.Fatalf("turn %d: no conversation array", turn)
	}
	cm, nm := c[key].([]any), n[key].([]any)
	if s.MovingCacheControl {
		cm, nm = stripCacheControl(cm).([]any), stripCacheControl(nm).([]any)
	}
	if len(nm) <= len(cm) {
		t.Fatalf("turn %d -> %d: %s did not grow (%d -> %d)", turn, turn+1, key, len(cm), len(nm))
	}
	if !reflect.DeepEqual(cm, nm[:len(cm)]) {
		for i := range cm {
			if !reflect.DeepEqual(cm[i], nm[i]) {
				t.Fatalf("turn %d -> %d: %s[%d] changed", turn, turn+1, key, i)
			}
		}
	}
	for k, v := range c {
		if k != key && !reflect.DeepEqual(v, n[k]) {
			t.Errorf("turn %d -> %d: top-level %s changed", turn, turn+1, k)
		}
	}
	for k := range n {
		if _, ok := c[k]; !ok {
			t.Errorf("turn %d -> %d: top-level %s added", turn, turn+1, k)
		}
	}
}

func stripCacheControl(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			if k != "cache_control" {
				out[k] = stripCacheControl(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = stripCacheControl(e)
		}
		return out
	}
	return v
}

// checkMovingCache: exactly one message carries cache_control, and it is
// the newest user message.
func checkMovingCache(t *testing.T, bodies [][]byte) {
	t.Helper()
	for k, b := range bodies {
		msgs := decode(t, b).(map[string]any)["messages"].([]any)
		lastUser := -1
		var marked []int
		for i, m := range msgs {
			mm := m.(map[string]any)
			if mm["role"] == "user" {
				lastUser = i
			}
			if strings.Contains(mustJSON(mm), `"cache_control"`) {
				marked = append(marked, i)
			}
		}
		if len(marked) != 1 || marked[0] != lastUser {
			t.Errorf("turn %d: cache_control on messages %v, want only the newest user message %d", k+1, marked, lastUser)
		}
	}
}

func decode(t *testing.T, b []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// objects calls fn on every object in v.
func objects(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, e := range t {
			objects(e, fn)
		}
	case []any:
		for _, e := range t {
			objects(e, fn)
		}
	}
}

func any2(v any, pred func(map[string]any) bool) bool {
	found := false
	objects(v, func(o map[string]any) {
		if !found && pred(o) {
			found = true
		}
	})
	return found
}

func hasShape(shape string, doc any, raw []byte) bool {
	top, _ := doc.(map[string]any)
	isStr := func(v any) bool { _, ok := v.(string); return ok }
	isArr := func(v any) bool { _, ok := v.([]any); return ok }
	switch shape {
	case corpus.ShapeToolResultString:
		return any2(doc, func(o map[string]any) bool {
			return (o["type"] == "tool_result" || o["role"] == "tool") && isStr(o["content"])
		}) || any2(doc, func(o map[string]any) bool {
			r, ok := o["toolResult"].(map[string]any)
			return ok && isArr(r["content"])
		})
	case corpus.ShapeToolResultBlocks:
		return any2(doc, func(o map[string]any) bool { return o["type"] == "tool_result" && isArr(o["content"]) })
	case corpus.ShapeToolResultJSON:
		return any2(doc, func(o map[string]any) bool {
			_, ok := o["json"].(map[string]any)
			_, arr := o["json"].([]any)
			r, fr := o["functionResponse"].(map[string]any)
			_, res := func() (any, bool) {
				if !fr {
					return nil, false
				}
				resp, _ := r["response"].(map[string]any)
				v, ok := resp["result"]
				return v, ok
			}()
			return ok || arr || res
		})
	case corpus.ShapeToolContentParts:
		return any2(doc, func(o map[string]any) bool { return o["role"] == "tool" && isArr(o["content"]) })
	case corpus.ShapeCacheControlSystem:
		sys, _ := top["system"].([]any)
		return strings.Contains(mustJSON(sys), `"cache_control"`)
	case corpus.ShapeCacheControlBlock:
		return any2(doc, func(o map[string]any) bool { return o["type"] == "tool_result" && o["cache_control"] != nil })
	case corpus.ShapeCacheControlTools:
		tools, _ := top["tools"].([]any)
		return strings.Contains(mustJSON(tools), `"cache_control"`)
	case corpus.ShapeBedrockCachePoint:
		return any2(doc, func(o map[string]any) bool { return o["cachePoint"] != nil })
	case corpus.ShapeContextManagement:
		return top["context_management"] != nil
	case corpus.ShapeCompaction:
		return any2(doc, func(o map[string]any) bool { return o["type"] == "compaction" })
	case corpus.ShapeThinking:
		return any2(doc, func(o map[string]any) bool {
			return o["type"] == "thinking" && o["signature"] != "" && isStr(o["signature"])
		})
	case corpus.ShapeRedactedThinking:
		return any2(doc, func(o map[string]any) bool { return o["type"] == "redacted_thinking" && isStr(o["data"]) })
	case corpus.ShapeReasoningContent:
		return any2(doc, func(o map[string]any) bool {
			rt, _ := o["reasoningText"].(map[string]any)
			return rt != nil && isStr(rt["signature"])
		})
	case corpus.ShapeEncryptedReasoning:
		return any2(doc, func(o map[string]any) bool { return o["type"] == "reasoning" && isStr(o["encrypted_content"]) })
	case corpus.ShapeThoughtSignature:
		return any2(doc, func(o map[string]any) bool { return isStr(o["thoughtSignature"]) })
	case corpus.ShapeImage:
		return any2(doc, func(o map[string]any) bool { return o["type"] == "image" || o["type"] == "image_url" })
	case corpus.ShapeBase64TextDocument:
		return any2(doc, func(o map[string]any) bool {
			src, _ := o["source"].(map[string]any)
			return o["type"] == "document" && src["type"] == "base64" && src["media_type"] == "text/plain"
		})
	case corpus.ShapeDeveloperRole:
		return any2(doc, func(o map[string]any) bool { return o["role"] == "developer" })
	case corpus.ShapeParallelToolCalls:
		return any2(doc, func(o map[string]any) bool {
			if calls, ok := o["tool_calls"].([]any); ok && len(calls) > 1 {
				return true
			}
			for _, key := range []string{"content", "parts", "input"} {
				arr, _ := o[key].([]any)
				n := 0
				for _, el := range arr {
					b, _ := el.(map[string]any)
					switch {
					case b["type"] == "tool_use", b["toolUse"] != nil, b["functionCall"] != nil, b["type"] == "function_call", b["type"] == "function_call_output":
						n++
						if n > 1 {
							return true
						}
					default:
						n = 0
					}
				}
			}
			return false
		})
	case corpus.ShapeIsError:
		return any2(doc, func(o map[string]any) bool { return o["is_error"] == true || o["status"] == "error" })
	case corpus.ShapeMCPResult:
		return bytes.Contains(raw, []byte("structuredContent"))
	case corpus.ShapePythonJSON:
		for _, c := range raw {
			if c >= 0x80 {
				return false
			}
		}
		// The euro sign, escaped the way ensure_ascii writes it.
		return bytes.HasPrefix(raw, []byte(`{"model": `)) && bytes.Contains(raw, []byte(`\`+`u20ac`))
	case corpus.ShapeFunctionRole:
		return any2(doc, func(o map[string]any) bool { return o["role"] == "function" })
	case corpus.ShapeStringInput:
		return isStr(top["input"])
	case corpus.ShapeCustomToolOutput:
		return any2(doc, func(o map[string]any) bool { return o["type"] == "custom_tool_call_output" })
	case corpus.ShapeWrappedOutput:
		return any2(doc, func(o map[string]any) bool {
			s, _ := o["output"].(string)
			return o["type"] == "function_call_output" && strings.HasPrefix(s, `{"output":`)
		})
	case corpus.ShapePreviousResponseID:
		return isStr(top["previous_response_id"])
	case corpus.ShapeANSI:
		return bytes.Contains(raw, []byte(`\u001b[`))
	}
	panic("unknown shape " + shape)
}

// pairs returns the sorted tool call ids and result ids of a body. Formats
// without ids on the wire (Google, Ollama) pair by count.
func pairs(format string, doc any) (calls, results []string) {
	top := doc.(map[string]any)
	add := func(dst *[]string, v any) {
		if s, ok := v.(string); ok {
			*dst = append(*dst, s)
		}
	}
	switch {
	case format == "openai":
		objects(doc, func(o map[string]any) {
			if tc, ok := o["tool_calls"].([]any); ok {
				for _, c := range tc {
					add(&calls, c.(map[string]any)["id"])
				}
			}
			if o["role"] == "tool" {
				add(&results, o["tool_call_id"])
			}
		})
	case format == "anthropic" || (format == "bedrock" && top["anthropic_version"] != nil):
		objects(doc, func(o map[string]any) {
			switch o["type"] {
			case "tool_use":
				add(&calls, o["id"])
			case "tool_result":
				add(&results, o["tool_use_id"])
			}
		})
	case format == "bedrock":
		objects(doc, func(o map[string]any) {
			if u, ok := o["toolUse"].(map[string]any); ok {
				add(&calls, u["toolUseId"])
			}
			if r, ok := o["toolResult"].(map[string]any); ok {
				add(&results, r["toolUseId"])
			}
		})
	case format == "openai_responses":
		objects(doc, func(o map[string]any) {
			switch o["type"] {
			case "function_call", "custom_tool_call":
				add(&calls, o["call_id"])
			case "function_call_output", "custom_tool_call_output":
				add(&results, o["call_id"])
			}
		})
	case format == "google_ai" || format == "vertex":
		objects(doc, func(o map[string]any) {
			if fc, ok := o["functionCall"].(map[string]any); ok {
				add(&calls, fc["name"])
			}
			if fr, ok := o["functionResponse"].(map[string]any); ok {
				add(&results, fr["name"])
			}
		})
	case format == "ollama":
		objects(doc, func(o map[string]any) {
			if tc, ok := o["tool_calls"].([]any); ok {
				for _, c := range tc {
					add(&calls, c.(map[string]any)["function"].(map[string]any)["name"])
				}
			}
			if o["role"] == "tool" {
				add(&results, o["tool_name"])
			}
		})
	}
	sort.Strings(calls)
	sort.Strings(results)
	return calls, results
}
