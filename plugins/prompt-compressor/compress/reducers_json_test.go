package compress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
)

// jsonCases are corpus JSON tool outputs with long arrays.
var jsonCases = []struct {
	kind    corpus.Kind
	variant string
	size    int
}{
	{corpus.JSONAPI, "", 60000},
	{corpus.JSONAPI, "compact", 50000},
	{corpus.MCP, "", 30000},
	{corpus.Kubectl, "json", 100000},
}

// The level-2 text of each corpus JSON output matches its golden file
// (go test -run TestJSONReducerGolden -update rewrites them).
func TestJSONReducerGolden(t *testing.T) {
	for _, tc := range jsonCases {
		name := fmt.Sprintf("json_%s_%s_%d", tc.kind, tc.variant, tc.size)
		t.Run(name, func(t *testing.T) {
			in := caseOutput(tc.kind, tc.variant, tc.size).Text
			out, steps := pass{level: 2}.run(in, 0)
			if !hasStep(steps, reducerStrategy(jsonReducerName)) {
				t.Fatalf("not reduced: steps %+v", steps)
			}
			if !json.Valid([]byte(out)) {
				t.Fatal("output is not JSON")
			}
			path := filepath.Join("testdata", "reducers", name+".golden")
			if *updateGolden {
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

// assertItemsVerbatim checks every reduced array in out against the same
// array in in: each item that is not a marker is an input item, in order,
// byte for byte. It follows object members and unreduced arrays.
func assertItemsVerbatim(t *testing.T, in, out string) {
	t.Helper()
	var walk func(a, b string)
	walk = func(a, b string) {
		switch {
		case strings.HasPrefix(a, "{"):
			am, bm := jsonMembers(a), jsonMembers(b)
			if len(am) != len(bm) {
				t.Fatalf("members changed: %d -> %d", len(am), len(bm))
			}
			for i := range am {
				if am[i].key != bm[i].key {
					t.Fatalf("member order changed: %q -> %q", am[i].key, bm[i].key)
				}
				walk(am[i].val, bm[i].val)
			}
		case strings.HasPrefix(a, "["):
			ai, _ := jsonItems(a, 0)
			bi, _ := jsonItems(b, 0)
			if len(ai) == len(bi) {
				for i := range ai {
					walk(a[ai[i].start:ai[i].end], b[bi[i].start:bi[i].end])
				}
				return
			}
			next := 0
			for _, s := range bi {
				item := b[s.start:s.end]
				if strings.HasPrefix(item, `"[tyk-compressed: `) {
					continue
				}
				for next < len(ai) && a[ai[next].start:ai[next].end] != item {
					next++
				}
				if next == len(ai) {
					t.Fatalf("kept item is not an input item: %.120s", item)
				}
				next++
			}
		}
	}
	walk(in, out)
}

// Corpus JSON: valid JSON, kept items verbatim, a marker, deterministic.
func TestJSONReducerProperties(t *testing.T) {
	for _, tc := range jsonCases {
		for seed := 0; seed < 5; seed++ {
			o := corpus.Make(corpus.NewR(fmt.Sprintf("props/%s/%d", tc.kind, seed)), tc.kind, tc.variant, tc.size)
			in, ok := compactJSON(o.Text)
			if !ok {
				t.Fatalf("%s: not JSON", tc.kind)
			}
			out, cut := reduceJSON(in, nil, 0)
			if !cut {
				t.Fatalf("%s seed %d: nothing cut", tc.kind, seed)
			}
			if !json.Valid([]byte(out)) || len(out) >= len(in) {
				t.Fatalf("%s seed %d: invalid or not shorter", tc.kind, seed)
			}
			if !strings.Contains(out, `"[tyk-compressed: json output, `) {
				t.Errorf("%s seed %d: no marker", tc.kind, seed)
			}
			assertItemsVerbatim(t, in, out)
			if again, _ := reduceJSON(in, nil, 0); again != out {
				t.Errorf("%s seed %d: not deterministic", tc.kind, seed)
			}
		}
	}
}

// rowsJSON is a compact array of n objects; special replaces some rows.
func rowsJSON(n int, special map[int]string) string {
	parts := make([]string, n)
	for i := range parts {
		if s, ok := special[i]; ok {
			parts[i] = s
			continue
		}
		parts[i] = fmt.Sprintf(`{"id":%d,"name":"job-%03d","status":"succeeded","latency_ms":%d,"success":true,"error":null}`, i, i, 100+i%7)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestJSONReducerKeepsFailuresAndOutliers(t *testing.T) {
	special := map[int]string{
		10: `{"id":10,"status":"Failed"}`,
		12: `{"id":12,"error":{"code":"E42","message":"boom"}}`,
		14: `{"id":14,"exit_code":2}`,
		16: `{"id":16,"success":false}`,
		18: `{"id":18,"status":503}`,
		20: `{"id":20,"status":{"phase":"CrashLoopBackOff"}}`,
		22: `{"id":22,"name":"job-022","status":"succeeded","latency_ms":9000}`,
		24: `{"id":24,"name":"needle-row"}`,
		26: `{"id":26,"errors":[]}`,
		28: `{"id":28,"exit_code":0,"status":200}`,
	}
	in := rowsJSON(60, special)
	keep := func(s string) bool { return strings.Contains(s, "needle") }
	out, cut := reduceJSON(in, keep, 0)
	if !cut || !json.Valid([]byte(out)) {
		t.Fatalf("cut %v, valid %v", cut, json.Valid([]byte(out)))
	}
	for i := 10; i <= 24; i += 2 {
		if !strings.Contains(out, special[i]) {
			t.Errorf("row %d dropped: %s", i, special[i])
		}
	}
	for _, i := range []int{26, 28} {
		if strings.Contains(out, special[i]) {
			t.Errorf("row %d is not a failure but was kept", i)
		}
	}
	for _, i := range []int{0, 4, 55, 59} {
		if !strings.Contains(out, fmt.Sprintf(`"id":%d,`, i)) {
			t.Errorf("edge row %d dropped", i)
		}
	}
	if strings.Contains(out, `"id":30,`) {
		t.Error("a plain middle row was kept")
	}
	assertItemsVerbatim(t, in, out)
	if !strings.HasPrefix(out, `[{"id":0,`) || !strings.Contains(out, `"[tyk-compressed: json output, `) || !strings.Contains(out, `"[tyk-compressed: 1 item]"`) && !strings.Contains(out, ` items]"`) {
		t.Errorf("markers: %.300s", out)
	}
}

func TestJSONReducerNumbersAndNesting(t *testing.T) {
	nums := make([]string, 40)
	for i := range nums {
		nums[i] = fmt.Sprint(1000 + i%5)
	}
	nums[17] = "250000"
	in := `{"samples":[` + strings.Join(nums, ",") + `],"meta":{"pages":[1,2,3]}}`
	out, cut := reduceJSON(in, nil, 0)
	if !cut || !json.Valid([]byte(out)) {
		t.Fatalf("cut %v: %s", cut, out)
	}
	if !strings.Contains(out, ",250000,") || !strings.HasSuffix(out, `"meta":{"pages":[1,2,3]}}`) {
		t.Errorf("outlier or short array lost: %s", out)
	}
	// A short array, and arrays inside kept items of a reduced array, stay
	// whole.
	inner := "[" + strings.TrimSuffix(strings.Repeat("7,", 30), ",") + "]"
	rows := rowsJSON(30, map[int]string{0: `{"id":0,"inner":` + inner + `}`})
	out, _ = reduceJSON(rows, nil, 0)
	if !strings.Contains(out, inner) {
		t.Error("an array inside a kept item was cut")
	}
	if _, cut := reduceJSON(rowsJSON(19, nil), nil, 0); cut {
		t.Error("an array under the threshold was cut")
	}
}

// A size budget cuts harder until the output fits, and stops at the floor.
func TestJSONReducerBudget(t *testing.T) {
	in := rowsJSON(60, nil)
	plain, _ := reduceJSON(in, nil, 0)
	fit, _ := reduceJSON(in, nil, selectionTokens(plain)-40)
	if selectionTokens(fit) > selectionTokens(plain)-40 {
		t.Errorf("budget %d not met: %d", selectionTokens(plain)-40, selectionTokens(fit))
	}
	floor, _ := reduceJSON(in, nil, 1)
	if !strings.HasPrefix(floor, `[{"id":0,`) || !strings.Contains(floor, `{"id":59,`) || strings.Contains(floor, `{"id":1,`) {
		t.Errorf("floor keeps one item at each end: %.300s", floor)
	}
	// Under the budget, the plain cut.
	if same, _ := reduceJSON(in, nil, 1<<20); same != plain {
		t.Error("a budget the output already fits changed it")
	}
	// The budget lets shorter arrays be cut.
	if _, cut := reduceJSON(rowsJSON(12, nil), nil, 10); !cut {
		t.Error("a 12-item array over budget was not cut")
	}
}

// The compressor applies a tool's budget to that tool's results only, and
// the memo keeps the two apart.
func TestToolBudgetInCompress(t *testing.T) {
	rows := rowsJSON(16, nil)
	body := func(tool string) []byte {
		b, _ := json.Marshal(map[string]any{"model": "gpt-4o", "messages": []any{
			map[string]any{"role": "user", "content": "go"},
			map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": tool, "arguments": "{}"}}}},
			map[string]any{"role": "tool", "tool_call_id": "c1", "content": rows},
			map[string]any{"role": "user", "content": "next"},
		}})
		return b
	}
	p := config.DefaultPolicy()
	p.Level, p.MinUnitTokens, p.ProtectRecentTurns = 2, 0, 0
	p.ToolOverrides = map[string]*config.ToolOverride{"tight": {MaxUnitTokens: 50}}
	c := New(Options{})
	_, loose := c.Compress(Input{Format: "openai", Body: body("loose"), Policy: p})
	_, tight := c.Compress(Input{Format: "openai", Body: body("tight"), Policy: p})
	if loose.Outcome == OutcomeCompressed && loose.Strategies[reducerStrategy(jsonReducerName)] != nil {
		t.Errorf("16 rows cut without a budget: %+v", loose)
	}
	if tight.Outcome != OutcomeCompressed || tight.Strategies[reducerStrategy(jsonReducerName)] == nil {
		t.Errorf("budgeted tool not reduced: %+v", tight)
	}
}
