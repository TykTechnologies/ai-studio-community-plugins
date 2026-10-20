package compress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
)

// Tracing never changes the body or the rest of the report, and traces
// every unit.
func TestTraceChangesNothing(t *testing.T) {
	m := loadCorpus(t)
	for _, level := range []int{1, 2} {
		p := config.DefaultPolicy()
		p.Level = level
		for _, e := range m.Entries {
			plain, prep := New(Options{}).Compress(Input{Format: e.Format, Body: e.Body(), Policy: p})
			traced, trep := New(Options{}).Compress(Input{Format: e.Format, Body: e.Body(), Policy: p, Trace: true})
			if !bytes.Equal(plain, traced) {
				t.Fatalf("level %d %s: tracing changed the body", level, e.Name)
			}
			units := trep.Units
			trep.Units = nil
			if !reflect.DeepEqual(prep, trep) {
				t.Fatalf("level %d %s: tracing changed the report:\n%+v\n%+v", level, e.Name, prep, trep)
			}
			if prep.UnitsSeen > 0 && len(units) != prep.UnitsSeen {
				t.Errorf("level %d %s: %d traces for %d units", level, e.Name, len(units), prep.UnitsSeen)
			}
			selected, changed := 0, 0
			for _, u := range units {
				if u.Selected {
					selected++
				} else if u.Reason == "" {
					t.Errorf("level %d %s: unit %d unselected without a reason", level, e.Name, u.Index)
				}
				if u.Changed {
					changed++
				}
			}
			if selected != prep.UnitsSelected || changed != prep.UnitsCompressed {
				t.Errorf("level %d %s: traced %d selected / %d changed, report %d / %d", level, e.Name, selected, changed, prep.UnitsSelected, prep.UnitsCompressed)
			}
		}
	}
}

func TestTraceReasons(t *testing.T) {
	noisy := strings.Repeat("\u001b[32mok\u001b[0m line   \n\n\n", 200)
	call := func(id, name string) map[string]any {
		return map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": "{}"}}}}
	}
	result := func(id, text string) map[string]any {
		return map[string]any{"role": "tool", "tool_call_id": id, "content": text}
	}
	body, _ := json.Marshal(map[string]any{"model": "gpt-4o", "messages": []any{
		map[string]any{"role": "user", "content": noisy},
		call("c1", "shell"), result("c1", noisy),
		call("c2", "shell"), result("c2", "short"),
		call("c3", "secret"), result("c3", noisy),
		call("c4", "shell"), result("c4", noisy),
	}})
	p := config.DefaultPolicy()
	p.Level = 2
	p.ToolOverrides = map[string]*config.ToolOverride{"secret": {Exclude: true}}
	_, rep := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p, Trace: true})
	byText := map[string]UnitTrace{}
	for _, u := range rep.Units {
		byText[u.Role+"/"+u.ToolName+"/"+strconv.Itoa(u.Turn)] = u
	}
	want := map[string]struct {
		selected bool
		reason   string
		level    int
		capped   []string
	}{
		"user//0":       {false, TraceRole, 0, nil},
		"tool/shell/1":  {true, "", 2, nil},
		"tool/shell/2":  {false, TraceBelowMinTokens, 2, nil},
		"tool/secret/3": {false, TraceExcludedTool, 0, nil},
		"tool/shell/4":  {true, "", 1, []string{TraceCapProtectedTurn}},
	}
	for key, w := range want {
		u, ok := byText[key]
		if !ok {
			t.Errorf("%s: no trace (have %v)", key, keys(byText))
			continue
		}
		if u.Selected != w.selected || u.Reason != w.reason || u.Level != w.level || !reflect.DeepEqual(u.Capped, w.capped) {
			t.Errorf("%s: %+v", key, u)
		}
	}
	u := byText["tool/shell/1"]
	if !u.Changed || len(u.Leaves) != 1 || u.Leaves[0].Before != noisy || u.BytesAfter >= u.BytesBefore || len(u.Strategies) == 0 {
		t.Errorf("changed unit: %+v", u)
	}
}

func keys(m map[string]UnitTrace) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDetectors(t *testing.T) {
	if got := Detectors(caseOutput(corpus.Pytest, "", 6000).Text); len(got) == 0 || got[0] != "pytest" {
		t.Errorf("pytest output: %v", got)
	}
	if got := Detectors(`[{"a":1}]`); !reflect.DeepEqual(got, []string{"json"}) {
		t.Errorf("JSON: %v", got)
	}
	var prose strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&prose, "Region %d grew revenue by %d percent this quarter.\n", i, i*3)
	}
	if got := Detectors(prose.String()); len(got) != 0 {
		t.Errorf("prose: %v", got)
	}
}
