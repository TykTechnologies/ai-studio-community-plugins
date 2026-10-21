package compress

import (
	"runtime"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// Compress allocates in proportion to the body, never hundreds of times it
// (eac.78: tiktoken over every leaf made a 3.4 MB body allocate 1.2 GB). The
// bound is per call, cold memo (the worst case), for a small, a typical and
// a near-max_body_bytes body at every level.
func TestCompressAllocationIsBounded(t *testing.T) {
	if raceEnabled {
		// The race detector defeats sync.Pool (regexp's machines among
		// them), so allocations under it say nothing about production; CI
		// runs this test again without it
		t.Skip("allocation bounds need a build without -race")
	}
	// Load the tokenizer once: its tables are process-wide, not per call
	EstimatorFor("openai", "gpt-4o").Count("warm up")
	for _, size := range []struct {
		name             string
		turns, toolBytes int
	}{{"small", 2, 4096}, {"typical", 12, 163840}, {"large", 40, 3400000}} {
		body := agentBody(size.turns, size.toolBytes)
		if len(body) > config.DefaultMaxBodyBytes {
			t.Fatalf("%s body is %d bytes, over max_body_bytes", size.name, len(body))
		}
		for _, level := range []int{1, 2, 3} {
			p := config.DefaultPolicy()
			p.Level, p.MinUnitTokens, p.ProtectRecentTurns = level, 0, 0
			in := Input{Format: "openai", Body: body, Policy: p}
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			if _, rep := New(Options{}).Compress(in); rep.Outcome != OutcomeCompressed {
				t.Fatalf("%s L%d: %s", size.name, level, rep.Outcome)
			}
			runtime.ReadMemStats(&after)
			allocated := after.TotalAlloc - before.TotalAlloc
			// 16x the body plus 2 MiB for the fixed costs (the tokenizer
			// sample, the memos); eac.80 (one parse per call) brings it down
			limit := 16*uint64(len(body)) + 2<<20
			t.Logf("%s L%d: %d KB body, %d KB allocated (%.1fx)", size.name, level, len(body)>>10, allocated>>10, float64(allocated)/float64(len(body)))
			if allocated > limit {
				t.Errorf("%s L%d: allocated %d bytes for a %d-byte body (limit %d)", size.name, level, allocated, len(body), limit)
			}
		}
	}
}

// The request's token counts come from a tokenizer sample of its own text:
// close to counting every leaf (within 15% on the agent bodies), and the
// same for the same request.
func TestCalibratedCountsAreClose(t *testing.T) {
	exact := EstimatorFor("openai", "gpt-4o")
	for _, size := range []struct{ turns, toolBytes int }{{2, 4096}, {12, 163840}} {
		body := agentBody(size.turns, size.toolBytes)
		p := config.DefaultPolicy()
		p.Level, p.MinUnitTokens, p.ProtectRecentTurns = 1, 0, 0
		_, rep := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p})
		_, again := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p})
		if rep.TokensBefore != again.TokensBefore || rep.TokensAfter != again.TokensAfter {
			t.Fatal("counts differ between two runs")
		}
		units, err := segments.ExtractUnits("openai", body)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		for _, u := range units {
			for _, l := range u.Leaves {
				if u.Kind == "tool_result" {
					want += exact.Count(l.Text)
				}
			}
		}
		if d := float64(rep.TokensBefore-want) / float64(want); d < -0.15 || d > 0.15 {
			t.Errorf("%d turns: %d tokens estimated, %d counted (%.0f%%)", size.turns, rep.TokensBefore, want, d*100)
		}
	}
}
