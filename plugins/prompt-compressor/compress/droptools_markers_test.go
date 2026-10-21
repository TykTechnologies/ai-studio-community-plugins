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
