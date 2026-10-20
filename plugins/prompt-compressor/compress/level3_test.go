package compress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
	"github.com/TykTechnologies/midsommar/v2/guardrails"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// level3Cases are corpus outputs cut at level 3 with a small budget.
var level3Cases = []struct {
	kind    corpus.Kind
	variant string
	size    int
	budget  int
}{
	{corpus.DockerBuild, "", 12000, 400},
	{corpus.HTML, "", 12000, 400},
	{corpus.JSONAPI, "", 60000, 1500},
	{corpus.MCP, "", 30000, 300},
	{corpus.Kubectl, "json", 100000, 300},
}

// The level-3 text of each case matches its golden file
// (go test -run TestLevel3Golden -update rewrites them).
func TestLevel3Golden(t *testing.T) {
	for _, tc := range level3Cases {
		name := fmt.Sprintf("l3_%s_%s_%d_%d", tc.kind, tc.variant, tc.size, tc.budget)
		t.Run(name, func(t *testing.T) {
			in := caseOutput(tc.kind, tc.variant, tc.size).Text
			out, steps := pass{level: 3, budget: tc.budget}.run(in, 0)
			if len(steps) == 0 || out == in {
				t.Fatal("not cut")
			}
			if _, isJSON := compactJSON(in); isJSON && !json.Valid([]byte(out)) {
				t.Fatal("a JSON output is no longer JSON")
			}
			if !strings.Contains(out, "[tyk-compressed: ") {
				t.Error("no marker")
			}
			path := filepath.Join("testdata", "level3", name+".golden")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if out != string(want) {
				t.Errorf("output differs from %s (run with -update after a deliberate change)", path)
			}
		})
	}
}

func TestHeadTail(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "step %03d: compiling module %d\n", i, i)
	}
	b.WriteString("FAILED: 2 errors")
	text := b.String()
	protected := "step 200: compiling module 200"
	keep := func(l string) bool { return l == protected }
	out, ok := headTail(text, 300, keep)
	if !ok {
		t.Fatal("not cut")
	}
	if !strings.HasPrefix(out, "step 000: ") || !strings.HasSuffix(out, "FAILED: 2 errors") || !strings.Contains(out, "\n"+protected+"\n") {
		t.Errorf("head, tail or protected line lost:\n%s", out)
	}
	if !strings.Contains(out, "[tyk-compressed: head and tail output, ") {
		t.Error("no marker")
	}
	in := map[string]bool{}
	for _, l := range strings.Split(text, "\n") {
		in[l] = true
	}
	for _, l := range strings.Split(out, "\n") {
		if !strings.HasPrefix(l, "[tyk-compressed: ") && !in[l] {
			t.Errorf("kept a line that is not an input line: %q", l)
		}
	}
	if selectionTokens(out) > 300+len(protected) {
		t.Errorf("%d tokens over a budget of 300", selectionTokens(out))
	}
	if same, ok := headTail(text, 1<<20, nil); ok || same != text {
		t.Error("a text under budget was cut")
	}
	// One long line is cut by bytes, at rune boundaries.
	long := strings.Repeat("héllo wörld ", 2000)
	out, ok = headTail(long, 100, nil)
	if !ok || !utf8.ValidString(out) || !strings.Contains(out, " bytes cut]") || len(out) >= len(long) {
		t.Errorf("byte cut: ok %v, valid %v, %d bytes", ok, utf8.ValidString(out), len(out))
	}
}

func TestJSONShape(t *testing.T) {
	long := strings.Repeat("x", 400)
	doc := `{"id":1,"name":"` + long + `","ok":true,"tags":["a","b","c"],"owner":{"login":"` + long + `","site_admin":false},"rows":[{"a":1},{"a":2}],"none":null}`
	out, ok := jsonShape(doc)
	if !ok || !json.Valid([]byte(out)) {
		t.Fatalf("ok %v: %s", ok, out)
	}
	if !strings.HasPrefix(out, `{"tyk-compressed":"[tyk-compressed: json shape, `) {
		t.Errorf("shape %s", out)
	}
	if !strings.HasSuffix(out, `"id":"number","name":"string","ok":"boolean","tags":["string","[tyk-compressed: 2 items]"],"owner":{"login":"string","site_admin":"boolean"},"rows":[{"a":"number"},"[tyk-compressed: 1 item]"],"none":null}`) {
		t.Errorf("shape %s", out)
	}
	arr, ok := jsonShape(rowsJSON(50, nil))
	if !ok || !json.Valid([]byte(arr)) || !strings.HasPrefix(arr, `["[tyk-compressed: json shape, `) {
		t.Errorf("array shape %s", arr)
	}
}

// The corpus JSON outputs stay JSON at level 3 whatever the budget.
func TestLevel3JSONStaysJSON(t *testing.T) {
	for _, tc := range jsonCases {
		in := caseOutput(tc.kind, tc.variant, tc.size).Text
		for _, budget := range []int{1, 50, 200, 1000, 5000} {
			out, _ := pass{level: 3, budget: budget}.run(in, 0)
			if !json.Valid([]byte(out)) {
				t.Errorf("%s budget %d: not JSON", tc.kind, budget)
			}
		}
	}
}

// toolUnits is a conversation of tool results, tokens[t] in turn t.
func toolUnits(tokens []int) []segments.Unit {
	var us []segments.Unit
	for turn, n := range tokens {
		u := segments.Unit{Turn: turn}
		u.Kind = guardrails.KindToolResult
		u.Text = strings.Repeat("abcd", n)
		us = append(us, u)
	}
	return us
}

// The mask boundary only moves forward, and only by at least a step of
// tokens; earlier turns decide it alone.
func TestMaskBoundarySteps(t *testing.T) {
	p := config.DefaultPolicy()
	p.MaskAfterTurns, p.MaskStepTokens = 3, 1000
	tokens := []int{300, 300, 300, 300, 2000, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 900}
	prev, moves := 0, 0
	for last := 0; last < len(tokens); last++ {
		b := maskBoundary(toolUnits(tokens[:last+1]), last, p)
		if b < prev {
			t.Fatalf("turn %d: boundary went back from %d to %d", last, prev, b)
		}
		if b != prev {
			moves++
			masked := 0
			for turn := prev; turn < b; turn++ {
				masked += tokens[turn]
			}
			if masked < p.MaskStepTokens {
				t.Errorf("turn %d: boundary moved %d → %d masking only %d tokens", last, prev, b, masked)
			}
			if b > last-p.MaskAfterTurns+1 {
				t.Errorf("turn %d: masked turn %d, younger than mask_after_turns", last, b-1)
			}
		}
		prev = b
	}
	if moves < 2 {
		t.Errorf("%d moves", moves)
	}
	p.MaskAfterTurns = 0
	if b := maskBoundary(toolUnits(tokens), len(tokens)-1, p); b != 0 {
		t.Errorf("mask_after_turns 0 masked up to %d", b)
	}
}

// At level 3 a session's earlier messages change only when the mask
// boundary moves; the test counts those rewrites.
func TestSessionsCacheStableLevel3(t *testing.T) {
	c := New(Options{})
	p := config.DefaultPolicy()
	p.Level, p.MaxUnitTokens, p.MaskAfterTurns, p.MaskStepTokens = 3, 800, 2, 1500
	rewrites, moves, masked := 0, 0, 0
	for _, s := range loadCorpus(t).Sessions {
		if s.History != "full" || !segments.Supports(s.Format) {
			continue
		}
		var prev []json.RawMessage
		prevNewest, prevBoundary := 0, 0
		for k, body := range s.Bodies() {
			out, rep := c.Compress(Input{Format: s.Format, Body: body, Policy: p})
			if rep.Outcome == OutcomeRejected || rep.Outcome == OutcomeSkipped {
				t.Fatalf("%s turn %d: %s (%s)", s.Name, k, rep.Outcome, rep.Reason)
			}
			if rep.Strategies[StrategyMask] != nil {
				masked++
			}
			units, err := segments.ExtractUnits(s.Format, body)
			if err != nil {
				t.Fatal(err)
			}
			last := 0
			for _, u := range units {
				last = max(last, u.Turn)
			}
			boundary := maskBoundary(units, last, p)
			if boundary != prevBoundary {
				moves++
			}
			cur := conversation(t, s.Format, out)
			if prev != nil {
				for i := 0; i < prevNewest; i++ {
					if !bytes.Equal(prev[i], cur[i]) {
						if boundary == prevBoundary {
							t.Fatalf("%s turn %d: message %d changed without a mask step", s.Name, k, i)
						}
						rewrites++
						break
					}
				}
			}
			prev, prevBoundary = cur, boundary
			prevNewest = newestTurnStart(t, s.Format, out, len(cur))
		}
	}
	if masked == 0 {
		t.Error("no session masked anything at level 3")
	}
	if rewrites > moves {
		t.Errorf("%d prefix rewrites for %d mask steps", rewrites, moves)
	}
	t.Logf("level 3: %d prefix rewrites, %d mask steps", rewrites, moves)
}

// The request-filter guard caps the level at 2, and says so.
func TestCapLossy(t *testing.T) {
	p := config.DefaultPolicy()
	p.Level, p.MinUnitTokens, p.ProtectRecentTurns, p.MaxUnitTokens = 3, 0, 0, 100
	text := caseOutput(corpus.HTML, "", 12000).Text
	body, _ := json.Marshal(map[string]any{"model": "gpt-4o", "messages": []any{
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "fetch", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": text},
		map[string]any{"role": "user", "content": "go on"},
	}})
	_, open := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p})
	_, capped := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p, CapLossy: true, Trace: true})
	if open.Strategies[StrategyHeadTail] == nil {
		t.Errorf("level 3 did not cut: %+v", open.Strategies)
	}
	if capped.Strategies[StrategyHeadTail] != nil || !capped.RequestFiltersCapped {
		t.Errorf("capped request was cut at level 3: %+v", capped)
	}
	for _, u := range capped.Units {
		if u.Selected && (u.Level != 2 || len(u.Capped) == 0 || u.Capped[0] != TraceCapRequestFilters) {
			t.Errorf("trace %+v", u)
		}
	}
}
