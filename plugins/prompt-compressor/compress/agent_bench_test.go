package compress

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

// agentBody is an OpenAI agent conversation of turns tool calls whose
// results hold about toolBytes of noisy test-runner output, the shape of
// the gateway benchmark's S8 bodies (benchmarks/gateway, probe.AgentSpec).
func agentBody(turns, toolBytes int) []byte {
	per := max(toolBytes/turns, 256)
	msgs := []any{
		map[string]any{"role": "system", "content": "You are a coding agent. Use the shell tool to run commands."},
		map[string]any{"role": "user", "content": "Run the test suite and fix the failures."},
	}
	for t := 0; t < turns; t++ {
		id := fmt.Sprintf("call_%04d", t)
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[1m============================= test session starts ==============================\x1b[0m   \nplatform linux -- Python 3.12.3, pytest-8.3.2\nrootdir: /srv/app\ncollected %d items\n\n\n\n", 400+t)
		i := 0
		for b.Len() < per*7/10 {
			fmt.Fprintf(&b, "tests/suite_%d/test_module_%03d.py::test_case_%04d \x1b[32mPASSED\x1b[0m        [%3d%%]   \n", t, i/20, i, i%100)
			if i%25 == 24 {
				b.WriteString("\n\n\n")
			}
			i++
		}
		fmt.Fprintf(&b, "=================================== FAILURES ===================================\n___ test_case_%04d ___\n\n    def test_case():\n>       assert ledger.balance(\"ACC-%d\") == 120\nE       AssertionError: assert 100 == 120\n\ntests/suite_%d/test_ledger.py:88: AssertionError\n{\"files\": [", i, t, t)
		for j := 0; b.Len() < per; j++ {
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"path": "src/module_%d/file_%d.py", "lines": %d, "covered": %d, "status": "ok"}`, t, j, 100+j%300, 90+j%200)
		}
		b.WriteString("]}\n")
		msgs = append(msgs,
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": id, "type": "function",
				"function": map[string]any{"name": "shell", "arguments": fmt.Sprintf(`{"cmd":"pytest -q tests/suite_%d"}`, t)}}}},
			map[string]any{"role": "tool", "tool_call_id": id, "content": b.String()})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": "Continue."})
	body, _ := json.Marshal(map[string]any{"model": "mock-model", "messages": msgs, "max_tokens": 16})
	return body
}

// BenchmarkAgentBody is the Compress call the post_auth hook makes, per
// body size and level, with a cold memo (every leaf compressed: the first
// time a tool output is seen) and a warm one (every leaf from the memo:
// the same history on a later turn).
func BenchmarkAgentBody(b *testing.B) {
	for _, size := range []struct {
		name             string
		turns, toolBytes int
	}{{"small", 2, 4096}, {"typical", 12, 163840}, {"large", 40, 3400000}} {
		body := agentBody(size.turns, size.toolBytes)
		for _, level := range []int{1, 2, 3} {
			p := config.DefaultPolicy()
			p.Level, p.MinUnitTokens, p.ProtectRecentTurns = level, 0, 0
			if level == 3 {
				p.MaskAfterTurns, p.MaskStepTokens = 2, 1000
			}
			in := Input{Format: "openai", Body: body, Policy: p}
			b.Run(fmt.Sprintf("%s/L%d/miss", size.name, level), func(b *testing.B) {
				b.SetBytes(int64(len(body)))
				for i := 0; i < b.N; i++ {
					c := New(Options{})
					if _, rep := c.Compress(in); rep.Outcome != OutcomeCompressed {
						b.Fatalf("%s", rep.Outcome)
					}
				}
			})
			b.Run(fmt.Sprintf("%s/L%d/hit", size.name, level), func(b *testing.B) {
				c := New(Options{})
				c.Compress(in)
				b.SetBytes(int64(len(body)))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					c.Compress(in)
				}
			})
		}
	}
}
