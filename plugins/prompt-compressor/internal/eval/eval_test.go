package eval

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
	"github.com/TykTechnologies/midsommar/v2/guardrails"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
	"github.com/TykTechnologies/midsommar/v2/models"
)

var (
	corpusOnce sync.Once
	corpusM    corpus.Manifest
	corpusErr  error
)

func loadCorpus(t *testing.T) corpus.Manifest {
	t.Helper()
	corpusOnce.Do(func() { corpusM, corpusErr = corpus.Load(corpus.Dir()) })
	if corpusErr != nil {
		t.Fatal(corpusErr)
	}
	return corpusM
}

// truncator is a toy deterministic compressor: every tool result leaf longer
// than keep bytes is cut to its head. It is a pure function of the unit text,
// like the real compressor must be.
type truncator struct{ keep int }

func (truncator) Name() string { return "truncator" }
func (c truncator) Compress(format, _ string, body []byte, level int) ([]byte, error) {
	if level == 0 || bytes.Contains(body, []byte(`"context_management"`)) || bytes.Contains(body, []byte(`"compaction"`)) {
		return body, nil
	}
	units, err := segments.ExtractUnits(format, body)
	if err != nil {
		return body, nil // not a format we read: pass through
	}
	var edits []segments.Edit
	for _, u := range units {
		if u.Kind != guardrails.KindToolResult {
			continue
		}
		for _, l := range u.Leaves {
			if len(l.Text) > c.keep {
				head := l.Text[:c.keep]
				for !utf8.ValidString(head) {
					head = head[:len(head)-1]
				}
				edits = append(edits, segments.Edit{Path: l.Path, Old: l.Text, New: head + " [cut]"})
			}
		}
	}
	if len(edits) == 0 {
		return body, nil
	}
	return segments.Apply(body, edits)
}

// flaky stamps a counter into every body: nondeterministic and cache-hostile.
type flaky struct{ n *int }

func (flaky) Name() string { return "flaky" }
func (f flaky) Compress(_, _ string, body []byte, level int) ([]byte, error) {
	if level == 0 {
		return body, nil
	}
	*f.n++
	return append([]byte(fmt.Sprintf(`{"x":%d,`, *f.n)), body[1:]...), nil
}

func TestIdentityReplayIsNeutral(t *testing.T) {
	t.Parallel()
	rep, err := Replay(loadCorpus(t), Options{Levels: []int{0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range rep.Levels {
		if l.TokensBefore != l.TokensAfter || l.TokensBefore == 0 {
			t.Errorf("level %d: tokens %d -> %d", l.Level, l.TokensBefore, l.TokensAfter)
		}
		if len(l.SkipViolations)+len(l.Nondeterministic)+len(l.Errors) != 0 {
			t.Errorf("level %d: %v %v %v", l.Level, l.SkipViolations, l.Nondeterministic, l.Errors)
		}
		if math.Abs(l.SessionCost-l.BaselineCost) > 1e-12 || l.SessionCost == 0 {
			t.Errorf("level %d: cost %v vs baseline %v", l.Level, l.SessionCost, l.BaselineCost)
		}
		for _, s := range l.Sessions {
			if s.Skipped != "" {
				continue
			}
			if s.PrefixBreaks != 0 {
				t.Errorf("%s: %d prefix breaks at identity", s.Name, s.PrefixBreaks)
			}
			if s.History == "full" && s.Usage.CacheRead == 0 {
				t.Errorf("%s: no cache reads modelled", s.Name)
			}
		}
	}
	md := rep.Markdown()
	for _, want := range []string{"## Summary", "### Tool output by kind", "anthropic_agent_30"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
	if _, err := rep.JSON(); err != nil {
		t.Fatal(err)
	}
}

func TestDeterministicCompressionSavesAfterCache(t *testing.T) {
	t.Parallel()
	rep, err := Replay(loadCorpus(t), Options{Levels: []int{1}, Compressor: truncator{keep: 600}})
	if err != nil {
		t.Fatal(err)
	}
	l := rep.Levels[0]
	if len(l.SkipViolations) != 0 || len(l.Nondeterministic) != 0 || len(l.Errors) != 0 {
		t.Fatalf("violations %v nondeterministic %v errors %v", l.SkipViolations, l.Nondeterministic, l.Errors)
	}
	if l.TokensAfter >= l.TokensBefore {
		t.Fatalf("no reduction: %d -> %d", l.TokensBefore, l.TokensAfter)
	}
	for _, k := range []string{"maven", "pytest", "json_api"} {
		if l.ByKind[k].Reduction() < 0.5 {
			t.Errorf("%s reduction %.2f, want > 0.5 for a 600-byte cut", k, l.ByKind[k].Reduction())
		}
	}
	// Session tool results are attributed to their kinds by marker (only
	// the Codex apply_patch confirmations are "other"), and the session
	// mix reaches the table.
	if o := l.SessionByKind["other"]; l.SessionUnattributed != 0 || o.Units > 6 || o.Before > 6*30 {
		t.Errorf("session results unattributed: %d tokens, other %+v", l.SessionUnattributed, o)
	}
	for _, k := range []string{"ripgrep", "gotest", "git_diff"} {
		if l.SessionByKind[k].Units == 0 || l.SessionByKind[k].Reduction() <= 0 {
			t.Errorf("session %s: %+v, want units cut by the truncator", k, l.SessionByKind[k])
		}
	}
	for _, s := range l.Sessions {
		if s.Skipped != "" {
			continue
		}
		if s.PrefixBreaks != 0 {
			t.Errorf("%s: %d prefix breaks from a deterministic compressor", s.Name, s.PrefixBreaks)
		}
		if s.CostDelta >= 0 {
			t.Errorf("%s: cost %.4f not below baseline %.4f", s.Name, s.Cost, s.BaselineCost)
		}
	}
}

func TestNondeterministicCompressionIsCaught(t *testing.T) {
	t.Parallel()
	n := 0
	rep, err := Replay(loadCorpus(t), Options{Levels: []int{1}, Compressor: flaky{&n}})
	if err != nil {
		t.Fatal(err)
	}
	l := rep.Levels[0]
	if len(l.Nondeterministic) == 0 {
		t.Error("nondeterminism not reported")
	}
	if len(l.SkipViolations) == 0 {
		t.Error("changed pass-through bodies not reported")
	}
	for _, s := range l.Sessions {
		if s.Skipped != "" || s.Name != "anthropic_agent_30" {
			continue
		}
		if s.PrefixBreaks == 0 || s.Usage.CacheRead != 0 || s.CostDelta <= 0 {
			t.Errorf("%s: breaks %d, cache read %d, cost delta %v: a prefix-breaking compressor must lose the cache and cost more",
				s.Name, s.PrefixBreaks, s.Usage.CacheRead, s.CostDelta)
		}
	}
}

// contextual cuts harder once the request is long: deterministic per body,
// but not a pure function of the unit text, so old tool results change
// between turns (the R1 violation the replay must flag).
type contextual struct{}

func (contextual) Name() string { return "contextual" }
func (contextual) Compress(format, path string, body []byte, level int) ([]byte, error) {
	keep := 600
	if len(body) > 150000 {
		keep = 300
	}
	return truncator{keep: keep}.Compress(format, path, body, level)
}

func TestContextDependentCompressionBreaksPrefix(t *testing.T) {
	t.Parallel()
	rep, err := Replay(loadCorpus(t), Options{Levels: []int{1}, Compressor: contextual{}})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rep.Levels[0].Sessions {
		if s.Name == "anthropic_agent_30" && s.PrefixBreaks == 0 {
			t.Errorf("%s: unit output changed with request length but no prefix break reported", s.Name)
		}
	}
}

// The plugin's compress library at level 1 over the whole corpus: no
// pass-through body touched, nothing nondeterministic, no prefix breaks, and
// sessions no dearer than at off (the level-1 tiered gate's replay check).
func TestLibraryLevel1Replay(t *testing.T) {
	t.Parallel()
	lib := NewLibrary()
	rep, err := Replay(loadCorpus(t), Options{Levels: []int{1}, Compressor: lib})
	if err != nil {
		t.Fatal(err)
	}
	l := rep.Levels[0]
	if len(l.SkipViolations)+len(l.Nondeterministic)+len(l.Errors) != 0 {
		t.Fatalf("skip violations %v, nondeterministic %v, errors %v", l.SkipViolations, l.Nondeterministic, l.Errors)
	}
	if l.TokensAfter > l.TokensBefore {
		t.Errorf("level 1 grew the corpus: %d -> %d tokens", l.TokensBefore, l.TokensAfter)
	}
	for _, s := range l.Sessions {
		if s.PrefixBreaks != 0 {
			t.Errorf("%s: %d prefix breaks", s.Name, s.PrefixBreaks)
		}
		if s.CostDelta > 1e-9 {
			t.Errorf("%s: level 1 costs more than off after cache (%+.4f)", s.Name, s.CostDelta)
		}
	}
	t.Logf("level 1: %d -> %d tokens, session cost %.4f vs %.4f at off, outcomes %v",
		l.TokensBefore, l.TokensAfter, l.SessionCost, l.BaselineCost, lib.Outcomes)
}

// protecting mimics protect_recent_turns: results in the newest turn get a
// light touch (level 1: a trailing space trimmed), older ones the full cut.
// A result compressed further as it leaves the newest turn is a late
// compression, not a prefix break.
type protecting struct{ keep int }

func (protecting) Name() string { return "protecting" }
func (p protecting) Compress(format, path string, body []byte, level int) ([]byte, error) {
	units, err := segments.ExtractUnits(format, body)
	if err != nil {
		return body, nil
	}
	newest := 0
	for _, u := range units {
		newest = max(newest, u.Turn)
	}
	var edits []segments.Edit
	for _, u := range units {
		if u.Kind != guardrails.KindToolResult {
			continue
		}
		for _, l := range u.Leaves {
			switch {
			case u.Turn == newest:
				if t := strings.TrimRight(l.Text, " \n"); t != l.Text {
					edits = append(edits, segments.Edit{Path: l.Path, Old: l.Text, New: t})
				}
			case len(l.Text) > p.keep:
				head := l.Text[:p.keep]
				for !utf8.ValidString(head) {
					head = head[:len(head)-1]
				}
				edits = append(edits, segments.Edit{Path: l.Path, Old: l.Text, New: head + " [cut]"})
			}
		}
	}
	if len(edits) == 0 {
		return body, nil
	}
	return segments.Apply(body, edits)
}

func TestProtectedTurnIsLateNotBroken(t *testing.T) {
	t.Parallel()
	rep, err := Replay(loadCorpus(t), Options{Levels: []int{2}, Compressor: protecting{keep: 600}})
	if err != nil {
		t.Fatal(err)
	}
	late := 0
	for _, s := range rep.Levels[0].Sessions {
		if s.PrefixBreaks != 0 {
			t.Errorf("%s: %d prefix breaks; results leaving the protected turn are late compressions", s.Name, s.PrefixBreaks)
		}
		late += s.LateCompressions
	}
	if late == 0 {
		t.Error("no late compressions counted")
	}
}

// leaky fails the way a careless compressor would: quoting the text it
// failed on.
type leaky struct{}

func (leaky) Name() string { return "leaky" }
func (leaky) Compress(format, _ string, body []byte, _ int) ([]byte, error) {
	units, _ := segments.ExtractUnits(format, body)
	text := string(body)
	if len(units) > 0 {
		text = units[len(units)-1].Text
	}
	return nil, fmt.Errorf("cannot reduce %q: %w", text, segments.ErrUnrecognisedBody)
}

// Reports of customer traffic carry counts, names and error classes, never
// body text, even when the compressor's errors quote it (security, eac.23).
func TestReportNeverQuotesBodyText(t *testing.T) {
	const canary = "CANARY-pc23-7f3e-do-not-report"
	dir := t.TempDir()
	body := `{"model":"gpt-4.1","messages":[{"role":"user","content":"run it"},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"bash","arguments":"{}"}}]},` +
		`{"role":"tool","tool_call_id":"call_1","content":"secret-looking output ` + canary + `"}]}`
	if err := os.MkdirAll(filepath.Join(dir, "openai"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "openai", "captured-1.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadBodies(dir)
	if err != nil || len(m.Entries) != 1 {
		t.Fatalf("load: %v, %d entries", err, len(m.Entries))
	}
	rep, err := Replay(m, Options{Levels: []int{1}, Compressor: leaky{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Levels[0].Errors) != 1 || !strings.Contains(rep.Levels[0].Errors[0], "unrecognised_body") {
		t.Fatalf("errors %v, want one classified error", rep.Levels[0].Errors)
	}
	js, _ := rep.JSON()
	for name, out := range map[string]string{"report.json": string(js), "report.md": rep.Markdown()} {
		if strings.Contains(out, canary) || strings.Contains(out, "secret-looking output") {
			t.Errorf("%s quotes body text", name)
		}
	}
}

func TestCostMatchesChatRecordCost(t *testing.T) {
	p := DefaultPrices()["anthropic"]
	u := Usage{Input: 1000, Output: 200, CacheWrite: 3000, CacheRead: 50000}
	want := (p.CPIT*1000 + p.CPT*200 + p.CacheWritePT*3000 + p.CacheReadPT*50000)
	if got := p.Cost(u); math.Abs(got-want) > 1e-12 {
		t.Fatalf("cost %v, want %v", got, want)
	}
	// OpenAI with no cache price set bills cache reads at the input price,
	// as the gateway does.
	o := Price{Vendor: "openai", CPIT: 2e-6, CPT: 8e-6}
	if got, want := o.Cost(Usage{CacheRead: 1000}), models.ChatRecordCost("openai", 8e-6, 2e-6, 0, 0, 0, 0, 0, 1000)/10000; got != want || got == 0 {
		t.Fatalf("openai fallback cost %v, want %v", got, want)
	}
}

func TestTurnUsage(t *testing.T) {
	anth, oai := CacheRules["anthropic"], CacheRules["openai"]
	cases := []struct {
		name     string
		rule     CacheRule
		tokens   int
		shared   int
		explicit bool
		want     Usage
	}{
		{"first turn writes", anth, 5000, 0, true, Usage{CacheWrite: 5000, Output: 10}},
		{"next turn reads prefix", anth, 6000, 5000, true, Usage{CacheRead: 5000, CacheWrite: 1000, Output: 10}},
		{"no breakpoints no cache", anth, 6000, 5000, false, Usage{Input: 6000, Output: 10}},
		{"short prefix not cached", anth, 6000, 900, true, Usage{CacheRead: 0, CacheWrite: 6000, Output: 10}},
		{"openai 128 steps", oai, 6000, 5000, false, Usage{CacheRead: 1024 + (5000-1024)/128*128, Input: 6000 - (1024 + (5000-1024)/128*128), Output: 10}},
		{"openai short", oai, 900, 800, false, Usage{Input: 900, Output: 10}},
		{"no rule", CacheRule{}, 100, 100, true, Usage{Input: 100, Output: 10}},
	}
	for _, c := range cases {
		if got := TurnUsage(c.rule, c.tokens, c.shared, c.explicit, 10); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestPairedDiff(t *testing.T) {
	control := []float64{1, 0, 1, 1, 0, 1, 1, 1, 0, 1}
	treat := []float64{1, 0, 1, 1, 0, 1, 1, 1, 0, 1}
	iv := PairedDiff(treat, control, 0.95, 2000, 7)
	if iv.Estimate != 0 || iv.Low != 0 || iv.High != 0 {
		t.Fatalf("identical arms: %+v", iv)
	}
	treat[0] = 0
	a := PairedDiff(treat, control, 0.95, 2000, 7)
	b := PairedDiff(treat, control, 0.95, 2000, 7)
	if a != b {
		t.Fatalf("same seed, different intervals: %+v %+v", a, b)
	}
	if a.Estimate != -0.1 || a.Low > a.Estimate || a.High < a.Estimate || a.Low >= 0 && a.High <= 0 {
		t.Fatalf("diff interval %+v", a)
	}
}

func TestWilson(t *testing.T) {
	iv := Wilson(80, 100, 0.95)
	// Reference values for 80/100 at 95%: [0.7112, 0.8666].
	if math.Abs(iv.Low-0.7112) > 0.001 || math.Abs(iv.High-0.8666) > 0.001 {
		t.Fatalf("wilson %+v", iv)
	}
	if z := zFor(0.8); math.Abs(z-1.2816) > 0.001 {
		t.Fatalf("z(0.8) = %v", z)
	}
}

func TestNonInferior(t *testing.T) {
	ok := NonInferior("resolve rate", Interval{Estimate: -1, Low: -4, High: 2, N: 200}, 5, 200)
	if !ok.Pass {
		t.Error("CI above -margin should pass")
	}
	bad := NonInferior("resolve rate", Interval{Estimate: -2, Low: -6, High: 1, N: 200}, 5, 200)
	if bad.Pass {
		t.Error("CI crossing -margin should fail")
	}
	small := NonInferior("resolve rate", Interval{Estimate: 0, Low: -1, High: 1, N: 50}, 5, 200)
	if small.Pass || !small.Inconclusive {
		t.Error("too few pairs must be inconclusive")
	}
}

func TestPlanReproducibleAndGuarded(t *testing.T) {
	ids := make([]string, 500)
	for i := range ids {
		ids[i] = fmt.Sprintf("repo__issue-%03d", i)
	}
	spec := Suites["swebench"]
	a, err := NewPlan(spec, ids, 200, 42, []int{2, 1}, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	rev := append([]string(nil), ids...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	b, _ := NewPlan(spec, rev, 200, 42, []int{1, 2}, "anthropic")
	if strings.Join(a.Tasks, ",") != strings.Join(b.Tasks, ",") {
		t.Fatal("same seed selected different tasks")
	}
	c, _ := NewPlan(spec, ids, 200, 43, []int{1, 2}, "anthropic")
	if strings.Join(a.Tasks, ",") == strings.Join(c.Tasks, ",") {
		t.Fatal("different seeds selected the same tasks")
	}
	if a.Runs != 200*3*3 || fmt.Sprint(a.Levels) != "[0 1 2]" {
		t.Fatalf("runs %d levels %v", a.Runs, a.Levels)
	}
	est := a.Estimate(DefaultPrices()["anthropic"], 8)
	if !est.MeetsGate || est.USD <= 0 {
		t.Fatalf("estimate %+v", est)
	}
	if err := a.Authorize(est); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("unapproved run allowed: %v", err)
	}
	a.Approve = &Approval{By: "lead", MaxUSD: est.USD / 2}
	if err := a.Authorize(est); !errors.Is(err, ErrNeedsApproval) {
		t.Fatal("approval below the estimate allowed")
	}
	a.Approve.MaxUSD = est.USD * 1.1
	if err := a.Authorize(est); err != nil {
		t.Fatal(err)
	}
}
