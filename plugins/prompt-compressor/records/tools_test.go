package records

import (
	"fmt"
	"testing"
	"time"
)

func toolRequest(tools ...string) Request {
	r := Request{At: time.Unix(3600*500000, 0), AppID: 1, LLMID: 2, Outcome: OutcomeApplied,
		ToolDefs: len(tools), ToolDefTokens: int64(10 * len(tools)), ToolListHash: "0123456789abcdef"}
	for _, n := range tools {
		r.OfferedTools = append(r.OfferedTools, OfferedTool{Name: n, Tokens: 10})
	}
	return r
}

// Client-chosen tool names are bounded per row: a name not shaped like a
// tool name, and names past MaxToolsPerRow, add to OtherTool.
func TestToolsAreBounded(t *testing.T) {
	d := NewDelta(KeyOf(toolRequest()))
	for i := 0; i < 3*MaxToolsPerRow; i++ {
		d.AddRequest(toolRequest(fmt.Sprintf("tool_%d", i)), Price{}, false)
	}
	r := toolRequest("<script>", "")
	r.CalledTools = []string{"tool_0", "tool_0", "x y"}
	d.AddRequest(r, Price{}, false)
	if len(d.Tools) != MaxToolsPerRow {
		t.Fatalf("%d tool names, max %d", len(d.Tools), MaxToolsPerRow)
	}
	if d.Tools["tool_0"].Called != 1 || d.Tools[OtherTool].Offered == 0 || d.Tools[OtherTool].Called != 1 {
		t.Errorf("tools %+v", d.Tools)
	}
	if _, ok := d.Tools["<script>"]; ok {
		t.Error("a malformed name was kept")
	}
	if d.ToolRequests != int64(3*MaxToolsPerRow+1) {
		t.Errorf("tool requests %d", d.ToolRequests)
	}
	for i := 0; i < 20; i++ {
		r := toolRequest("a")
		r.ToolListHash = fmt.Sprintf("%016x", i)
		d.AddRequest(r, Price{}, false)
	}
	if len(d.ToolLists) != MaxToolListsPerRow || d.ToolLists[OtherTool] == 0 {
		t.Errorf("tool lists %v", d.ToolLists)
	}
	b := &Batch{Version: BatchVersion, ID: "0123456789abcdef0123456789abcdef", Deltas: []*Delta{d}}
	if err := b.Validate(); err != nil {
		t.Errorf("a bounded delta is invalid: %v", err)
	}
}

// mergeKnown, at an hour document's size budget, takes no new tool names.
func TestToolsMergeKnown(t *testing.T) {
	a := NewDelta(KeyOf(toolRequest()))
	a.AddRequest(toolRequest("known"), Price{}, false)
	b := NewDelta(KeyOf(toolRequest()))
	b.AddRequest(toolRequest("known", "new_one"), Price{Input: 1, Currency: "USD"}, true)
	a.mergeKnown(b)
	if a.Tools["known"].Offered != 2 || a.Tools[OtherTool].Offered != 1 || a.ToolDefs != 3 {
		t.Errorf("merged %+v", a)
	}
	if _, ok := a.Tools["new_one"]; ok {
		t.Error("mergeKnown added a tool name")
	}
}

func TestValidateRefusesBadToolEntries(t *testing.T) {
	for name, mutate := range map[string]func(d *Delta){
		"name":     func(d *Delta) { d.Tools = map[string]ToolStats{"a b": {Offered: 1}} },
		"negative": func(d *Delta) { d.Tools = map[string]ToolStats{"a": {Called: -1}} },
		"too many": func(d *Delta) {
			d.Tools = map[string]ToolStats{}
			for i := 0; i <= MaxToolsPerRow; i++ {
				d.Tools[fmt.Sprint("t", i)] = ToolStats{}
			}
		},
		"list hash":   func(d *Delta) { d.ToolLists = map[string]int64{"not-hex": 1} },
		"tool counts": func(d *Delta) { d.ToolDefTokens = -5 },
		"tool cost":   func(d *Delta) { d.ToolDefCost = map[string]float64{"usd": 1} },
	} {
		d := NewDelta(KeyOf(toolRequest()))
		mutate(d)
		b := &Batch{Version: BatchVersion, ID: "0123456789abcdef0123456789abcdef", Deltas: []*Delta{d}}
		if b.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
