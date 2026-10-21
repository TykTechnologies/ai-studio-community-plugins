package compress

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// bedrockBody offers tools in the given order ("cp" is a cachePoint); its
// history holds a toolUse/toolResult pair for read_file when history is set.
func bedrockBody(history bool, tools ...string) string {
	var items []string
	for _, t := range tools {
		if t == "cp" {
			items = append(items, `{"cachePoint":{"type":"default"}}`)
			continue
		}
		items = append(items, toolSpec("bedrock", t))
	}
	msgs := `[{"role":"user","content":[{"text":"go"}]}]`
	if history {
		msgs = `[{"role":"user","content":[{"text":"go"}]},{"role":"assistant","content":[{"toolUse":{"toolUseId":"t1","name":"read_file","input":{}}}]},{"role":"user","content":[{"toolResult":{"toolUseId":"t1","content":[{"text":"ok"}]}}]}]`
	}
	return `{"messages":` + msgs + `,"toolConfig":{"tools":[` + strings.Join(items, ",") + `]}}`
}

// A dropped Anthropic tool's cache_control moves to the nearest kept tool
// before it (else after it), so the tools and system prompt stay cached;
// a kept tool keeps its own marker and never gets a second one.
func TestDropToolsMovesTheCacheBreakpoint(t *testing.T) {
	cc := `,"cache_control":{"type":"ephemeral"}}`
	marked := func(name string) string { return strings.TrimSuffix(toolSpec("anthropic", name), "}") + cc }
	body := func(tools ...string) string {
		return `{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"go"}],"tools":[` + strings.Join(tools, ",") + `]}`
	}
	for _, tc := range []struct {
		name, in, want string
	}{
		{"last tool marked", body(toolSpec("anthropic", "read_file"), marked("deploy_prod")), body(marked("read_file"))},
		{"first tool marked", body(marked("deploy_prod"), toolSpec("anthropic", "read_file")), body(marked("read_file"))},
		{"kept tool already marked", body(marked("read_file"), marked("deploy_prod")), body(marked("read_file"))},
		{"middle", body(toolSpec("anthropic", "a"), toolSpec("anthropic", "b"), marked("deploy_prod"), toolSpec("anthropic", "c")),
			body(toolSpec("anthropic", "a"), marked("b"), toolSpec("anthropic", "c"))},
	} {
		edits, _, _ := dropTools("anthropic", []byte(tc.in), []string{"deploy_*"})
		out, err := segments.Apply([]byte(tc.in), edits)
		if err != nil || string(out) != tc.want {
			t.Errorf("%s: %v\n%s\nwant\n%s", tc.name, err, out, tc.want)
		}
		if n := strings.Count(string(out), "cache_control"); n != 1 {
			t.Errorf("%s: %d breakpoints", tc.name, n)
		}
	}
	// Other formats have no tool-level breakpoint to move
	in := `{"model":"gpt-4o","messages":[],"tools":[` + toolSpec("openai", "read_file") + `,` + strings.TrimSuffix(toolSpec("openai", "deploy_prod"), "}") + cc + `]}`
	edits, _, _ := dropTools("openai", []byte(in), []string{"deploy_*"})
	if out, _ := segments.Apply([]byte(in), edits); strings.Contains(string(out), "cache_control") {
		t.Errorf("openai: %s", out)
	}
}

// Bedrock refuses toolConfig.tools = [cachePoint], tools = [] and a missing
// toolConfig beside tool blocks, so a drop never leaves a cache marker
// alone or before every tool: the array is kept whole instead.
func TestDropToolsKeepsBedrockValid(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		dropped []string
	}{
		{"only tool before cachePoint", bedrockBody(false, "deploy_prod", "cp"), nil},
		{"only tool before cachePoint, history", bedrockBody(true, "deploy_prod", "cp"), nil},
		{"cachePoint would lead", bedrockBody(false, "deploy_prod", "cp", "read_file"), nil},
		{"tool kept before cachePoint", bedrockBody(false, "read_file", "deploy_prod", "cp"), []string{"deploy_prod"}},
		{"several", bedrockBody(true, "read_file", "deploy_prod", "deploy_stage", "cp"), []string{"deploy_prod", "deploy_stage"}},
	} {
		edits, removed, decisions := dropTools("bedrock", []byte(tc.body), []string{"deploy_*"})
		if len(removed) != len(tc.dropped) {
			t.Errorf("%s: removed %d, want %v", tc.name, len(removed), tc.dropped)
			continue
		}
		if len(tc.dropped) == 0 {
			if len(edits) != 0 {
				t.Errorf("%s: edited %s", tc.name, edits[0].Value)
			}
			for _, d := range decisions {
				if d.Name != "" && d.Reason != ToolWouldEmpty {
					t.Errorf("%s: %+v", tc.name, d)
				}
			}
			continue
		}
		out, err := segments.Apply([]byte(tc.body), edits)
		if err != nil {
			t.Fatal(err)
		}
		var b struct {
			ToolConfig struct {
				Tools []map[string]json.RawMessage `json:"tools"`
			} `json:"toolConfig"`
		}
		if json.Unmarshal(out, &b) != nil || len(b.ToolConfig.Tools) == 0 {
			t.Fatalf("%s: %s", tc.name, out)
		}
		if _, ok := b.ToolConfig.Tools[0]["toolSpec"]; !ok {
			t.Errorf("%s: the array starts with a cache marker: %s", tc.name, out)
		}
	}
	// No drop list removes the last real tool in any format
	for _, tc := range dropCases() {
		if edits, _, _ := dropTools(tc.format, []byte(tc.body), []string{"read_file", "deploy_*", "lint"}); len(edits) > 0 {
			out, _ := segments.Apply([]byte(tc.body), edits)
			if !strings.Contains(string(out), "deploy_stage") && !strings.Contains(string(out), "lint") {
				t.Errorf("%s: no real tool left: %s", tc.format, out)
			}
		}
	}
}
