package compress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

var fuzzFormats = []string{"openai", "ollama", "anthropic", "google_ai", "bedrock", "openai_responses", "huggingface"}

// FuzzReduce: a reducer never panics, never grows the text, and keeps the
// lines it keeps verbatim.
func FuzzReduce(f *testing.F) {
	for _, tc := range reducerCases {
		f.Add(caseOutput(tc.kind, tc.variant, 3000).Text)
	}
	f.Add("a\na\na\na\na\na\na\na\nb")
	f.Add("diff --git a b\n@@ -1 +1 @@\n x\n-y\n+z\n x\n x\n x\n x\n x")
	f.Fuzz(func(t *testing.T, text string) {
		out, name := reduce(text, nil)
		if name == "" {
			if out != text {
				t.Fatal("no reducer, changed text")
			}
			return
		}
		if len(out) >= len(text) {
			t.Fatalf("%s grew the text", name)
		}
		in := map[string]bool{}
		for _, l := range strings.Split(text, "\n") {
			in[l] = true
		}
		for _, l := range strings.Split(out, "\n") {
			if !strings.HasPrefix(l, "[tyk-compressed: ") && !in[l] {
				t.Fatalf("%s kept a line that is not an input line: %q", name, l)
			}
		}
	})
}

// FuzzCompress drives selection, the strategies and Apply: Compress never
// panics, never grows the body, returns the input itself unless it
// compressed, and a compressed body reads in its format with its tool calls
// still paired.
func FuzzCompress(f *testing.F) {
	m := loadCorpus(f)
	for _, e := range m.Entries {
		if len(e.Body()) > 64<<10 {
			continue
		}
		for i, format := range fuzzFormats {
			if format == e.Format {
				f.Add(uint8(i), uint8(0), e.Body())
			}
		}
	}
	f.Add(uint8(0), uint8(1), []byte(`{"messages":[{"role":"tool","tool_call_id":"c","content":"\u001b[31m{ \"a\" :  1 }\r\n\n\n\n"}]}`))
	f.Add(uint8(2), uint8(2), []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"a  \n\n\n"}]}]}`))

	c := New(Options{MemoBytes: 1 << 20})
	f.Fuzz(func(t *testing.T, fi, flags uint8, body []byte) {
		format := fuzzFormats[int(fi)%len(fuzzFormats)]
		p := allRoles()
		p.DedupeReferences = flags&1 != 0
		if flags&2 != 0 {
			p.CacheMode = config.CacheSkipCached
		}
		p.ProtectRecentTurns = int(flags>>2) % 3
		p.Level = 1 + int(flags>>4)%2
		out, rep := c.Compress(Input{Format: format, Body: body, Policy: p})
		if len(out) > len(body) {
			t.Fatalf("output grew: %d > %d", len(out), len(body))
		}
		if rep.Outcome != OutcomeCompressed {
			if !bytes.Equal(out, body) {
				t.Fatalf("%s outcome with a changed body", rep.Outcome)
			}
			return
		}
		if !json.Valid(out) {
			t.Fatalf("invalid JSON: %s", out)
		}
		if _, err := segments.ExtractUnits(format, out); err != nil {
			t.Fatalf("compressed body does not read: %v", err)
		}
		if segments.CheckToolPairing(format, body) == nil && segments.CheckToolPairing(format, out) != nil {
			t.Fatal("pairing lost")
		}
		again, _ := c.Compress(Input{Format: format, Body: body, Policy: p})
		if !bytes.Equal(again, out) {
			t.Fatal("a second run differs")
		}
	})
}

// benchBody is an OpenAI agent conversation of about tokens tokens whose
// tool outputs are noisy shell logs and pretty JSON, the shapes level 1
// compresses.
func benchBody(tokens int) []byte {
	msgs := []any{map[string]any{"role": "user", "content": "Fix the failing build and summarise."}}
	logLine := "\x1b[32mPASS\x1b[0m pkg/service/handler_test.go   \r 42%\r100%   \n"
	approxTokens := 0
	for i := 0; approxTokens < tokens; i++ {
		id := fmt.Sprintf("call_%06d", i)
		msgs = append(msgs, map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "shell", "arguments": `{"cmd":"go test ./..."}`}},
		}})
		var out string
		if i%2 == 0 {
			out = strings.Repeat(logLine, 60) + "\n\n\n\nok  all\n"
		} else {
			rows := make([]map[string]any, 40)
			for j := range rows {
				rows[j] = map[string]any{"id": j, "name": fmt.Sprintf("item-%d", j), "price": 12.5, "tags": []string{"a", "b"}}
			}
			pretty, _ := json.MarshalIndent(rows, "", "    ")
			out = string(pretty)
		}
		msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": id, "content": out})
		approxTokens += len(out) / 4
	}
	b, _ := json.Marshal(map[string]any{"model": "gpt-4o", "messages": msgs})
	return b
}

func benchmarkCompress(b *testing.B, tokens int, warm bool) {
	body := benchBody(tokens)
	p := config.DefaultPolicy()
	in := Input{Format: "openai", Body: body, Policy: p, MaxBodyBytes: len(body) + 1}
	c := New(Options{})
	if warm {
		c.Compress(in)
	}
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !warm {
			c = New(Options{})
		}
		out, rep := c.Compress(in)
		if rep.Outcome != OutcomeCompressed || len(out) >= len(body) {
			b.Fatalf("%s %s", rep.Outcome, rep.Reason)
		}
	}
}

func BenchmarkCompress10kTokens(b *testing.B)      { benchmarkCompress(b, 10_000, false) }
func BenchmarkCompress100kTokens(b *testing.B)     { benchmarkCompress(b, 100_000, false) }
func BenchmarkCompress1MTokens(b *testing.B)       { benchmarkCompress(b, 1_000_000, false) }
func BenchmarkCompress100kTokensWarm(b *testing.B) { benchmarkCompress(b, 100_000, true) }
