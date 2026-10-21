package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

const evidence = "EVIDENCE-7"

// oracle is a stand-in for the oracle mock: it answers right only when the
// evidence reached it, by the request's model: "call" (one tool call),
// "qa" (an answer) or "agent" (tool calls until two results are in the
// history, then a patch). It answers in the shape of the path.
func oracle(mark bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if mark {
			w.Header().Set(MockHeader, MockOracle)
		}
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		ok := bytes.Contains(body, []byte(evidence))
		anthropic := strings.HasSuffix(r.URL.Path, "/messages")
		results := bytes.Count(body, []byte(`"role":"tool"`)) + bytes.Count(body, []byte(`"type":"tool_result"`))
		var text, tool, args string
		switch {
		case req.Model == "call":
			tool, args = "get_weather", `{"city":"Rome"}`
			if ok {
				args = `{ "city" : "Paris" }`
			}
		case req.Model == "agent" && results < 2:
			tool, args = "run", `{"cmd":"pytest"}`
		case req.Model == "agent":
			text = "no fix"
			if ok {
				text = "patch: FIX-42 applied"
			}
		default:
			text = "Rome"
			if ok {
				text = "Paris."
			}
		}
		in := len(body) / 4
		var out map[string]any
		if anthropic {
			content := []any{}
			if tool != "" {
				var a any
				_ = json.Unmarshal([]byte(args), &a)
				content = append(content, map[string]any{"type": "tool_use", "id": "tu1", "name": tool, "input": a})
			} else {
				content = append(content, map[string]any{"type": "text", "text": text})
			}
			out = map[string]any{"content": content, "usage": map[string]any{"input_tokens": in, "output_tokens": 5}}
		} else {
			msg := map[string]any{"role": "assistant", "content": text}
			if tool != "" {
				msg = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
					map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": tool, "arguments": args}}}}
			}
			out = map[string]any{"choices": []any{map[string]any{"message": msg}}, "usage": map[string]any{"prompt_tokens": in, "completion_tokens": 5}}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

// noisy is tool output level 2 shrinks; the evidence travels in a short
// result of its own.
var noisy = strings.Repeat("\u001b[32mPASSED\u001b[0m tests/test_x.py::case   \n\n\n", 300)

func writeTasks(t *testing.T, tasks ...Task) string {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("# fixture\n\n")
	for _, tk := range tasks {
		line, err := json.Marshal(tk)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "tasks.jsonl")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func openaiRequest(t *testing.T, model string) json.RawMessage {
	t.Helper()
	return mustMarshal(t, map[string]any{"model": model, "max_tokens": 64,
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "get_weather", "parameters": map[string]any{}}}},
		"messages": []any{
			map[string]any{"role": "user", "content": "where?"},
			map[string]any{"role": "assistant", "tool_calls": []any{
				map[string]any{"id": "a", "type": "function", "function": map[string]any{"name": "logs", "arguments": "{}"}},
				map[string]any{"id": "b", "type": "function", "function": map[string]any{"name": "note", "arguments": "{}"}}}},
			map[string]any{"role": "tool", "tool_call_id": "a", "content": noisy},
			map[string]any{"role": "tool", "tool_call_id": "b", "content": "city: " + evidence},
			map[string]any{"role": "user", "content": "now answer"},
		}})
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A dry run through the compressing gateway: every suite's scorer, the
// agent loop, paired comparisons and the report.
func TestLiveDryRun(t *testing.T) {
	l2 := config.DefaultPolicy()
	l2.Level, l2.ProtectRecentTurns = 2, 0
	srv := httptest.NewServer(CompressingGateway(oracle(true), map[string]*config.Policy{"pc-l2": l2}))
	defer srv.Close()
	g := Gateway{BaseURL: srv.URL, Secret: "s", Slugs: map[int]string{0: "pc-off", 2: "pc-l2"}}
	if err := g.Preflight(context.Background(), FormatOpenAI, true); err != nil {
		t.Fatal(err)
	}

	agentReq := mustMarshal(t, map[string]any{"model": "agent", "max_tokens": 64,
		"tools":    []any{map[string]any{"name": "run", "input_schema": map[string]any{"type": "object"}}},
		"messages": []any{map[string]any{"role": "user", "content": "fix the failing test"}}})
	for _, tc := range []struct {
		suite string
		task  Task
		turns float64
	}{
		{"bfcl", Task{ID: "b1", Format: FormatOpenAI, Request: openaiRequest(t, "call"),
			Expect: Expect{Calls: []ExpectedCall{{Name: "get_weather", Arguments: json.RawMessage(`{"city":"Paris"}`)}}}}, 1},
		{"lcqa", Task{ID: "q1", Format: FormatOpenAI, Request: openaiRequest(t, "qa"), Expect: Expect{Answers: []string{"Paris"}}}, 1},
		{"swebench", Task{ID: "s1", Format: FormatAnthropic, Request: agentReq, MaxTurns: 6,
			ToolOutputs: []string{noisy, "FAILED: " + evidence}, Expect: Expect{ResolvedIf: "FIX-42"}}, 3},
	} {
		t.Run(tc.suite, func(t *testing.T) {
			tasks, err := LoadTasks(writeTasks(t, tc.task))
			if err != nil {
				t.Fatal(err)
			}
			pl, err := NewPlan(Suites[tc.suite], []string{tc.task.ID}, 1, 7, []int{2}, tc.task.Format)
			if err != nil {
				t.Fatal(err)
			}
			res, err := Run(context.Background(), g, pl, map[string]Task{tasks[0].ID: tasks[0]}, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range res {
				if r.Error != "" || r.Score != 100 || float64(r.Turns) != tc.turns {
					t.Errorf("level %d: %+v", r.Level, r)
				}
			}
			rep := Summarise(pl, res, DefaultPrices()["openai"], true)
			if len(rep.Configs) != 2 || len(rep.Comparisons) != 1 {
				t.Fatalf("report %+v", rep)
			}
			cmp := rep.Comparisons[0]
			if !cmp.Verdict.Inconclusive || cmp.Verdict.Diff.Estimate != 0 || cmp.InputChange >= 0 {
				t.Errorf("comparison %+v", cmp)
			}
			if md := rep.Markdown(); !strings.Contains(md, "Dry run") || !strings.Contains(md, "inconclusive") {
				t.Errorf("markdown:\n%s", md)
			}
		})
	}
}

// When compression cuts the evidence, the task fails at that level only:
// the paired difference sees it.
func TestLiveDryRunSeesLostEvidence(t *testing.T) {
	cut := config.DefaultPolicy()
	cut.Level, cut.ProtectRecentTurns, cut.MinUnitTokens, cut.MaxUnitTokens = 3, 0, 0, 50
	srv := httptest.NewServer(CompressingGateway(oracle(true), map[string]*config.Policy{"pc-l3": cut}))
	defer srv.Close()
	g := Gateway{BaseURL: srv.URL, Slugs: map[int]string{0: "pc-off", 3: "pc-l3"}}
	req := mustMarshal(t, map[string]any{"model": "qa", "messages": []any{
		map[string]any{"role": "user", "content": "q"},
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "a", "type": "function", "function": map[string]any{"name": "doc", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "a", "content": noisy + strings.Repeat("filler sentence about nothing in particular. ", 200) + evidence + strings.Repeat(" tail words", 300)},
	}})
	tasks := map[string]Task{"q": {ID: "q", Format: FormatOpenAI, Request: req, Expect: Expect{Answers: []string{"Paris"}}}}
	pl, _ := NewPlan(Suites["lcqa"], []string{"q"}, 1, 1, []int{3}, FormatOpenAI)
	res, err := Run(context.Background(), g, pl, tasks, 1)
	if err != nil {
		t.Fatal(err)
	}
	rep := Summarise(pl, res, Price{}, true)
	if rep.Configs[0].Metric.Estimate != 100 || rep.Configs[1].Metric.Estimate != 0 || rep.Comparisons[0].Verdict.Diff.Estimate != -100 {
		t.Errorf("report %+v %+v", rep.Configs, rep.Comparisons)
	}
}

// A dry run refuses an upstream that is not the mock before sending a task.
func TestLivePreflightRefusesARealUpstream(t *testing.T) {
	srv := httptest.NewServer(CompressingGateway(oracle(false), nil))
	defer srv.Close()
	g := Gateway{BaseURL: srv.URL, Slugs: map[int]string{0: "off"}}
	if err := g.Preflight(context.Background(), FormatOpenAI, true); !errors.Is(err, ErrNotMock) {
		t.Fatalf("preflight: %v", err)
	}
	if err := g.Preflight(context.Background(), FormatOpenAI, false); err != nil {
		t.Fatalf("a paid run's preflight checks reachability only: %v", err)
	}
}

func TestLoadTasksRefusesBadLines(t *testing.T) {
	for name, line := range map[string]string{
		"no id":      `{"format":"openai","request":{}}`,
		"bad format": `{"id":"a","format":"gemini","request":{}}`,
		"not json":   `{"id":`,
	} {
		path := filepath.Join(t.TempDir(), "t.jsonl")
		os.WriteFile(path, []byte(line+"\n"), 0o600)
		if _, err := LoadTasks(path); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	path := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(path, []byte(`{"id":"a","format":"openai","request":{}}`+"\n"+`{"id":"a","format":"openai","request":{}}`+"\n"), 0o600)
	if _, err := LoadTasks(path); err == nil {
		t.Error("a repeated id was accepted")
	}
}

func TestScorers(t *testing.T) {
	task := Task{Format: FormatOpenAI, Request: json.RawMessage(`{"tools":[{"type":"function","function":{"name":"f"}}]}`),
		Expect: Expect{Calls: []ExpectedCall{{Name: "f", Arguments: json.RawMessage(`{"a":1,"b":[2]}`)}}}}
	for args, want := range map[string]score{
		`{"b":[2], "a":1.0}`: {Value: 100},
		`{"a":2,"b":[2]}`:    {},
		`{"a":`:              {Malformed: true},
	} {
		if got := scoreCalls(task, reply{Calls: []call{{Name: "f", Arguments: args}}}); got != want {
			t.Errorf("%s: %+v, want %+v", args, got, want)
		}
	}
	if got := scoreCalls(task, reply{Calls: []call{{Name: "g", Arguments: `{}`}}}); !got.Malformed {
		t.Error("a call to a tool not offered is not malformed")
	}
	qa := Task{Expect: Expect{Answers: []string{"the Eiffel Tower", "Eiffel"}}}
	if s := scoreAnswer(qa, reply{Text: "The Eiffel Tower."}); s.EM != 100 || s.Value != 100 {
		t.Errorf("exact: %+v", s)
	}
	if s := scoreAnswer(qa, reply{Text: "the tall Eiffel tower"}); s.EM != 0 || s.Value < 79 || s.Value > 81 {
		t.Errorf("partial: %+v", s)
	}
}

// The agent loop's history has each assistant turn and one tool result per
// call, in each vendor's shape.
func TestAppendTurn(t *testing.T) {
	r := reply{Calls: []call{{ID: "c1", Name: "run"}}, assistant: json.RawMessage(`{"role":"assistant","tool_calls":[{"id":"c1"}]}`)}
	out, err := appendTurn(FormatOpenAI, []byte(`{"model":"m","messages":[{"role":"user","content":"x"}]}`), r, []string{"ok"})
	if err != nil || !strings.Contains(string(out), `{"content":"ok","role":"tool","tool_call_id":"c1"}`) || !strings.Contains(string(out), `"model":"m"`) {
		t.Fatalf("openai: %s %v", out, err)
	}
	r.assistant = json.RawMessage(`{"role":"assistant","content":[{"type":"tool_use","id":"c1"}]}`)
	out, err = appendTurn(FormatAnthropic, []byte(`{"messages":[]}`), r, []string{"ok"})
	if err != nil || !strings.Contains(string(out), `{"content":[{"content":"ok","tool_use_id":"c1","type":"tool_result"}],"role":"user"}`) {
		t.Fatalf("anthropic: %s %v", out, err)
	}
}
