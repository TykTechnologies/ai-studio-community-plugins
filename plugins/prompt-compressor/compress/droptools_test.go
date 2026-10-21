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

// dropCase is a body per format offering read_file, deploy_prod,
// deploy_stage and lint, whose history calls deploy_stage and whose
// tool_choice names lint.
type dropCase struct {
	format, body string
}

func toolSpec(format, name string) string {
	switch format {
	case "openai":
		return `{"type":"function","function":{"name":"` + name + `","parameters":{"type":"object"}}}`
	case "anthropic":
		return `{"name":"` + name + `","input_schema":{"type":"object"}}`
	case "openai_responses":
		return `{"type":"function","name":"` + name + `","parameters":{"type":"object"}}`
	case "google_ai":
		return `{"name":"` + name + `","parameters":{"type":"object"}}`
	case "bedrock":
		return `{"toolSpec":{"name":"` + name + `","inputSchema":{"json":{"type":"object"}}}}`
	}
	panic(format)
}

func dropCases() []dropCase {
	tools := func(f string) string {
		var s []string
		for _, n := range []string{"read_file", "deploy_prod", "deploy_stage", "lint"} {
			s = append(s, toolSpec(f, n))
		}
		return strings.Join(s, ", ")
	}
	return []dropCase{
		{"openai", `{"model":"gpt-4o","messages":[{"role":"user","content":"go"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"deploy_stage","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"ok"}],"tools":[` + tools("openai") + `],"tool_choice":{"type":"function","function":{"name":"lint"}}}`},
		{"anthropic", `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"go"},{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"deploy_stage","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}],"tools":[` + tools("anthropic") + `],"tool_choice":{"type":"tool","name":"lint"}}`},
		{"openai_responses", `{"model":"gpt-5","input":[{"role":"user","content":"go"},{"type":"function_call","call_id":"c1","name":"deploy_stage","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":"ok"}],"tools":[` + tools("openai_responses") + `,{"type":"web_search"}],"tool_choice":{"type":"function","name":"lint"}}`},
		{"google_ai", `{"contents":[{"role":"user","parts":[{"text":"go"}]},{"role":"model","parts":[{"functionCall":{"name":"deploy_stage","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"deploy_stage","response":{"ok":true}}}]}],"tools":[{"functionDeclarations":[` + tools("google_ai") + `]}],"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["lint"]}}}`},
		{"bedrock", `{"messages":[{"role":"user","content":[{"text":"go"}]},{"role":"assistant","content":[{"toolUse":{"toolUseId":"t1","name":"deploy_stage","input":{}}}]},{"role":"user","content":[{"toolResult":{"toolUseId":"t1","content":[{"text":"ok"}]}}]}],"toolConfig":{"tools":[` + tools("bedrock") + `,{"cachePoint":{"type":"default"}}],"toolChoice":{"tool":{"name":"lint"}}}}`},
	}
}

// Matching tools go, by name and by prefix; tools the history calls and
// tool_choice names stay; every other byte of the body is unchanged.
func TestDropToolsPerFormat(t *testing.T) {
	for _, tc := range dropCases() {
		t.Run(tc.format, func(t *testing.T) {
			edits, removed, decisions := dropTools(tc.format, []byte(tc.body), []string{"deploy_*", "lint", "absent"})
			if len(edits) != 1 || len(removed) != 1 || !strings.Contains(string(removed[0]), "deploy_prod") {
				t.Fatalf("edits %+v, removed %s", edits, removed)
			}
			got := map[string]string{}
			for _, d := range decisions {
				got[d.Name] = d.Reason
			}
			for name, want := range map[string]string{"read_file": ToolNotListed, "deploy_prod": ToolDropped, "deploy_stage": ToolInHistory, "lint": ToolInChoice} {
				if got[name] != want {
					t.Errorf("%s: %q, want %q", name, got[name], want)
				}
			}
			out, err := segments.Apply([]byte(tc.body), edits)
			if err != nil || !json.Valid(out) {
				t.Fatalf("apply: %v", err)
			}
			// Apply identity outside the edit: the body with the one
			// definition (and its comma) cut out
			def := string(removed[0])
			want := strings.Replace(tc.body, def+", ", "", 1)
			if out := string(out); out != want {
				t.Errorf("other bytes changed:\n%s\nwant\n%s", out, want)
			}
		})
	}
}

// A drop list that would empty a tools array keeps it whole.
func TestDropToolsNeverEmptiesAnArray(t *testing.T) {
	for _, tc := range dropCases() {
		edits, removed, decisions := dropTools(tc.format, []byte(tc.body), []string{"*"})
		var n int
		for _, d := range decisions {
			if d.Dropped {
				n++
			}
		}
		// deploy_stage (history) and lint (tool_choice) stay, so the array
		// is not emptied: read_file and deploy_prod go
		if len(edits) != 1 || len(removed) != 2 || n != 2 {
			t.Errorf("%s: %d edits, %d removed", tc.format, len(edits), len(removed))
		}
	}
	only := `{"model":"gpt-4o","messages":[{"role":"user","content":"go"}],"tools":[` + toolSpec("openai", "a") + `,` + toolSpec("openai", "b") + `]}`
	edits, _, decisions := dropTools("openai", []byte(only), []string{"*"})
	if len(edits) != 0 || decisions[0].Reason != ToolWouldEmpty {
		t.Errorf("emptied: %+v %+v", edits, decisions)
	}
}

// Through Compress: the drop is reported as a strategy, passes the
// post-check and is deterministic.
func TestDropToolsInCompress(t *testing.T) {
	p := config.DefaultPolicy()
	p.DropTools = []string{"deploy_*"}
	c := New(Options{})
	for _, tc := range dropCases() {
		out, rep := c.Compress(Input{Format: tc.format, Body: []byte(tc.body), Policy: p, Trace: true})
		if rep.Outcome != OutcomeCompressed || rep.Strategies[StrategyDropTools] == nil || rep.TokensSaved() <= 0 || len(rep.ToolDecisions) < 4 {
			t.Errorf("%s: %+v", tc.format, rep)
			continue
		}
		if bytes.Contains(out, []byte("deploy_prod")) || !bytes.Contains(out, []byte("deploy_stage")) {
			t.Errorf("%s: %s", tc.format, out)
		}
		again, _ := New(Options{}).Compress(Input{Format: tc.format, Body: []byte(tc.body), Policy: p})
		if !bytes.Equal(out, again) {
			t.Errorf("%s: not deterministic", tc.format)
		}
	}
}

// A session keeps its earlier messages byte-identical with a drop list,
// and the tools array is the same every turn.
func TestDropToolsCacheStable(t *testing.T) {
	p := config.DefaultPolicy()
	p.DropTools = []string{"deploy_*"}
	c := New(Options{})
	tools := `[` + toolSpec("openai", "read_file") + `,` + toolSpec("openai", "deploy_prod") + `]`
	var msgs []string
	var prevTools string
	for turn := 0; turn < 6; turn++ {
		msgs = append(msgs, fmt.Sprintf(`{"role":"user","content":"turn %d"}`, turn),
			fmt.Sprintf(`{"role":"assistant","tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{}"}}]}`, turn),
			fmt.Sprintf(`{"role":"tool","tool_call_id":"c%d","content":"%s"}`, turn, strings.Repeat(`line   \n\n`, 100)))
		body := `{"model":"gpt-4o","messages":[` + strings.Join(msgs, ",") + `],"tools":` + tools + `}`
		out, rep := c.Compress(Input{Format: "openai", Body: []byte(body), Policy: p})
		if rep.Outcome != OutcomeCompressed {
			t.Fatalf("turn %d: %s %s", turn, rep.Outcome, rep.Reason)
		}
		var b struct {
			Tools json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal(out, &b)
		if prevTools != "" && string(b.Tools) != prevTools {
			t.Fatalf("turn %d: the tools changed", turn)
		}
		prevTools = string(b.Tools)
	}
}
