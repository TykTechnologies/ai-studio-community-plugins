package compress

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

var updateGolden = flag.Bool("update", false, "rewrite the reducer golden files")

// reducerCases are corpus tool outputs for each reducer, by the reducer
// name they must be detected as.
var reducerCases = []struct {
	kind    corpus.Kind
	variant string
	size    int
	reducer string
}{
	{corpus.Pytest, "", 6000, "pytest"},
	{corpus.Pytest, "", 30000, "pytest"},
	{corpus.Jest, "", 6000, "jest"},
	{corpus.Jest, "ansi", 6000, "jest"},
	{corpus.GoTest, "", 6000, "go test"},
	{corpus.GoTest, "panic", 6000, "go test"},
	{corpus.Cargo, "", 6000, "cargo test"},
	{corpus.Maven, "", 6000, "maven"},
	{corpus.Gradle, "", 6000, "gradle"},
	{corpus.TerraformPlan, "", 6000, "terraform plan"},
	{corpus.Kubectl, "", 6000, "kubectl"},
	{corpus.Kubectl, "describe", 6000, "kubectl"},
	{corpus.DirListing, "", 6000, "listing"},
	{corpus.DirListing, "find", 6000, "listing"},
	{corpus.DockerBuild, "", 12000, "build log"},
}

func caseOutput(kind corpus.Kind, variant string, size int) corpus.Output {
	return corpus.Make(corpus.NewR(fmt.Sprintf("reducer/%s/%s/%d", kind, variant, size)), kind, variant, size)
}

// The level-2 text of each corpus output (level-1 strategies, then its
// reducer) matches its golden file. go test -run TestReducerGolden -update
// rewrites them after a deliberate change (which also bumps
// config.StrategyVersion).
func TestReducerGolden(t *testing.T) {
	for _, tc := range reducerCases {
		name := fmt.Sprintf("%s_%s_%d", tc.kind, tc.variant, tc.size)
		t.Run(name, func(t *testing.T) {
			in := caseOutput(tc.kind, tc.variant, tc.size).Text
			out, steps := pass{level: 2}.run(in, 0)
			if !hasStep(steps, reducerStrategy(tc.reducer)) {
				t.Fatalf("not reduced by %s: steps %+v", tc.reducer, steps)
			}
			path := filepath.Join("testdata", "reducers", name+".golden")
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

func hasStep(steps []step, name string) bool {
	for _, s := range steps {
		if s.Strategy == name {
			return true
		}
	}
	return false
}

// Per reducer: never longer, every failure line and protected line kept,
// a marker whenever lines were cut, and the same bytes on a second run.
func TestReducerProperties(t *testing.T) {
	for _, tc := range reducerCases {
		for seed := 0; seed < 5; seed++ {
			o := corpus.Make(corpus.NewR(fmt.Sprintf("props/%s/%d", tc.kind, seed)), tc.kind, tc.variant, tc.size)
			in := collapseWhitespace(collapseCRFrames(stripANSI(o.Text)))
			var r reducer
			for _, x := range registry {
				if x.name == tc.reducer {
					r = x
				}
			}
			lines := strings.Split(in, "\n")
			// Protect one line the reducer would otherwise drop.
			var protected string
			var drops []bool
			if r.plan != nil {
				drops = r.plan(lines).drop
			} else {
				st := &reducerState{}
				for _, l := range lines {
					drops = append(drops, r.drop(st, l))
				}
			}
			for i, l := range lines {
				if drops[i] && strings.TrimSpace(l) != "" && !(r.keepErrors && errorLine.MatchString(l)) {
					protected = l
					break
				}
			}
			keep := func(l string) bool { return protected != "" && l == protected }

			out, name := reduce(in, keep)
			if name != tc.reducer {
				t.Fatalf("%s seed %d: reduced by %q", tc.kind, seed, name)
			}
			if len(out) > len(in) {
				t.Errorf("%s seed %d: longer", tc.kind, seed)
			}
			if !strings.Contains(out, "[tyk-compressed: "+tc.reducer+" output, ") {
				t.Errorf("%s seed %d: no marker", tc.kind, seed)
			}
			outLines := map[string]int{}
			for _, l := range strings.Split(out, "\n") {
				outLines[l]++
			}
			for _, l := range lines {
				if r.failure != nil && r.failure(l) && outLines[l] == 0 {
					t.Errorf("%s seed %d: failure line dropped: %q", tc.kind, seed, l)
				}
			}
			if protected != "" && outLines[protected] == 0 {
				t.Errorf("%s seed %d: protected line dropped: %q", tc.kind, seed, protected)
			}
			if again, _ := reduce(in, keep); again != out {
				t.Errorf("%s seed %d: not deterministic", tc.kind, seed)
			}
			assertVerbatim(t, fmt.Sprintf("%s seed %d", tc.kind, seed), r, in, out)
		}
	}
}

// Text that is not a test runner's output is left alone at level 2.
func TestReducersIgnoreOtherText(t *testing.T) {
	prose := strings.Repeat("The quarterly report shows revenue grew in every region, with the strongest gains in EMEA.\nCosts were flat.\n\n", 20)
	code := strings.Repeat("func (s *Store) Get(id string) (*Entry, error) {\n\tif id == \"\" {\n\t\treturn nil, ErrNotFound\n\t}\n\treturn s.m[id], nil\n}\n", 20)
	rows := make([]map[string]any, 50)
	for i := range rows {
		rows[i] = map[string]any{"id": i, "status": "PASS", "name": fmt.Sprintf("test_%d", i)}
	}
	js, _ := json.MarshalIndent(rows, "", "  ")
	mentions := strings.Repeat("We ran pytest and go test yesterday; 3 failed, 10 passed in 2.1s.\nok so FAIL is a word.\n", 20)
	for name, text := range map[string]string{"prose": prose, "code": code, "json": string(js), "mentions": mentions} {
		if out, reducer := reduce(text, nil); reducer != "" || out != text {
			t.Errorf("%s was reduced by %q", name, reducer)
		}
	}
	// Other corpus shapes are left alone.
	for _, kind := range []corpus.Kind{corpus.CSV, corpus.HTML} {
		if _, reducer := reduce(caseOutput(kind, "", 6000).Text, nil); reducer != "" {
			t.Errorf("%s output was reduced by %q", kind, reducer)
		}
	}
}

func TestMarkerFormat(t *testing.T) {
	if got := marker("pytest", 18400, 1150, 3); got != "[tyk-compressed: pytest output, 18,400 → 1,150 tokens; 3 failures kept]" {
		t.Errorf("marker = %q", got)
	}
	if got := marker("go test", 1234567, 5, 1); got != "[tyk-compressed: go test output, 1,234,567 → 5 tokens; 1 failure kept]" {
		t.Errorf("marker = %q", got)
	}
	if shortMarker(1) != "[tyk-compressed: 1 line]" || shortMarker(12) != "[tyk-compressed: 12 lines]" {
		t.Error("short marker")
	}
}

// The registry's reducers and versions are pinned with the strategy version
// that feeds every policy hash: change a reducer, bump both.
func TestRegistryPinnedToStrategyVersion(t *testing.T) {
	const pinned = "pytest@1,jest@1,go test@1,cargo test@1,maven@1,gradle@1," +
		"terraform plan@1,kubectl@1,git diff@1,grep@1,listing@1,build log@1,stack trace@1,repeated lines@1," +
		"json@1[{5 20} {5 13} {3 9} {2 7} {1 5}]/d16,level3@1"
	if got := registryFingerprint(); got != pinned || config.StrategyVersion != 5 {
		t.Fatalf("registry %q at strategy version %d: update the pin here and bump config.StrategyVersion", got, config.StrategyVersion)
	}
}

// A protected recent turn gets level 1 even when an older unit with the same
// text was reduced at level 2: the memo key holds the level.
func TestMemoKeyHoldsLevel(t *testing.T) {
	out := caseOutput(corpus.Pytest, "", 6000).Text
	body := mustJSON(t, map[string]any{"messages": []any{
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "pytest", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": out},
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "pytest", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "c2", "content": out},
	}})
	p := config.DefaultPolicy()
	p.Level = 2
	p.ProtectRecentTurns = 1
	c := New(Options{})
	compressed, rep := c.Compress(Input{Format: "openai", Body: body, Policy: p})
	if rep.Outcome != OutcomeCompressed {
		t.Fatalf("report %+v", rep)
	}
	units, _ := segments.ExtractUnits("openai", compressed)
	var old, recent string
	for _, u := range units {
		switch u.ToolCallID {
		case "c1":
			old = u.Text
		case "c2":
			recent = u.Text
		}
	}
	if !strings.Contains(old, "[tyk-compressed: pytest output") {
		t.Error("the old unit was not reduced at level 2")
	}
	if strings.Contains(recent, "[tyk-compressed:") {
		t.Error("the protected recent unit got the level-2 result")
	}
	// A fresh compressor (cold memo) gives the same bytes: R1.
	again, _ := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p})
	if string(again) != string(compressed) {
		t.Error("output depends on the memo")
	}
}

// Protect patterns from the policy reach the reducers.
func TestProtectPatternsSurviveLevel2(t *testing.T) {
	out := caseOutput(corpus.GoTest, "", 6000).Text
	first := regexp.MustCompile(`(?m)^=== RUN\s+(\S+)$`).FindStringSubmatch(out)
	if first == nil {
		t.Fatal("no RUN line")
	}
	body := mustJSON(t, map[string]any{"messages": []any{
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "go", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": out},
	}})
	p := config.DefaultPolicy()
	p.Level = 2
	p.ProtectRecentTurns = 0
	p.ProtectPatterns = []string{regexp.QuoteMeta(first[1]) + `$`}
	cfg := config.Default()
	cfg.Policies = map[string]*config.Policy{"p": p}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	compressed, _ := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: cfg.Policies["p"]})
	units, _ := segments.ExtractUnits("openai", compressed)
	if !strings.Contains(units[0].Text, first[0]) {
		t.Errorf("protected line %q was dropped", first[0])
	}
	if !strings.Contains(units[0].Text, "[tyk-compressed: go test output") {
		t.Error("not reduced")
	}
}

// assertVerbatim checks R3: every line the reducer kept is a line of the
// input, unchanged, and (for reducers that keep errors) every input line
// that mentions an error survives.
func assertVerbatim(t *testing.T, name string, r reducer, in, out string) {
	t.Helper()
	inLines := map[string]bool{}
	for _, l := range strings.Split(in, "\n") {
		inLines[l] = true
	}
	outLines := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		outLines[l] = true
		if strings.HasPrefix(l, "[tyk-compressed: ") {
			continue
		}
		if !inLines[l] {
			t.Errorf("%s: kept line %q is not an input line", name, l)
		}
	}
	for l := range inLines {
		if r.keepErrors && errorLine.MatchString(l) && !outLines[l] {
			t.Errorf("%s: error line dropped: %q", name, l)
		}
	}
}

// The log and listing reducers on shapes the corpus does not generate.
func TestTextReducers(t *testing.T) {
	javaTrace := strings.Join([]string{
		"java.lang.IllegalStateException: period already closed",
		"\tat com.acme.ledger.PeriodCloser.close(PeriodCloser.java:88)",
		"\tat com.acme.ledger.StatementService.closePeriod(StatementService.java:140)",
		"\tat java.base/jdk.internal.reflect.DirectMethodHandleAccessor.invoke(DirectMethodHandleAccessor.java:103)",
		"\tat java.base/java.lang.reflect.Method.invoke(Method.java:580)",
		"\tat org.springframework.aop.support.AopUtils.invokeJoinpointUsingReflection(AopUtils.java:355)",
		"\tat org.springframework.aop.framework.ReflectiveMethodInvocation.invokeJoinpoint(ReflectiveMethodInvocation.java:196)",
		"\tat org.springframework.aop.framework.ReflectiveMethodInvocation.proceed(ReflectiveMethodInvocation.java:163)",
		"\tat org.springframework.transaction.interceptor.TransactionInterceptor.invoke(TransactionInterceptor.java:119)",
		"\tat com.acme.ledger.api.PeriodController.close(PeriodController.java:52)",
		"\tat org.apache.catalina.core.ApplicationFilterChain.doFilter(ApplicationFilterChain.java:166)",
		"\tat org.apache.catalina.core.StandardWrapperValve.invoke(StandardWrapperValve.java:90)",
		"\tat java.base/java.lang.Thread.run(Thread.java:1583)",
	}, "\n")
	repeated := "starting worker\n" + strings.Repeat("WARN retrying connection to db:5432\n", 40) + "connected\nready\n" + strings.Repeat("tick\n", 10)
	var rg strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&rg, "internal/store/ledger.go:%d:\ttotal := x.Subtotal()\n", 100+i)
	}
	rg.WriteString("internal/api/handler.go:12:\treturn errors.New(\"subtotal failed\")\n")
	rg.WriteString("pkg/client/tenant.go:9:\ttotal := x.Subtotal()\n")
	diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,20 +1,20 @@\n" +
		strings.Repeat(" ctx line\n", 8) + "-old\n+new\n" + strings.Repeat(" ctx tail\n", 9)
	for _, tc := range []struct {
		name, text, reducer string
		contains, absent    []string
	}{
		{"java trace", javaTrace, "stack trace",
			[]string{"IllegalStateException", "PeriodCloser.close", "StatementService.closePeriod", "PeriodController.close", "Thread.run", "framework frames]"},
			[]string{"AopUtils"}},
		{"repeated", repeated, "repeated lines",
			[]string{"WARN retrying connection to db:5432\n", "[tyk-compressed: previous line repeated 39 more times]", "tick\n[tyk-compressed: previous line repeated 9 more times]", "connected"},
			nil},
		{"ripgrep", rg.String(), "grep",
			[]string{"ledger.go:104:", "[tyk-compressed: 35 more matches in internal/store/ledger.go]", "handler.go:12:", "tenant.go:9:"},
			[]string{"ledger.go:105:"}},
		{"wide diff", diff, "git diff",
			[]string{"@@ -1,20 +1,20 @@", "-old", "+new"},
			nil},
	} {
		out, name := reduce(tc.text, nil)
		if name != tc.reducer {
			t.Errorf("%s: reduced by %q, want %q", tc.name, name, tc.reducer)
			continue
		}
		for _, want := range tc.contains {
			if !strings.Contains(out, want) {
				t.Errorf("%s: output lacks %q:\n%s", tc.name, want, out)
			}
		}
		for _, no := range tc.absent {
			if strings.Contains(out, no) {
				t.Errorf("%s: output still has %q", tc.name, no)
			}
		}
		for _, r := range registry {
			if r.name == tc.reducer && r.name != "repeated lines" {
				assertVerbatim(t, tc.name, r, tc.text, out)
			}
		}
	}
}
