package main

import (
	"reflect"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
)

// The savings view reports each App's tool-definition cost, its share of
// the prompt, the tools never called and what dropping them would save.
func TestSavingsToolsSection(t *testing.T) {
	h := int64(500000)
	row := func(app uint32, outcome string, tools map[string]records.ToolStats, defTokens, input int64) *records.Delta {
		d := records.NewDelta(records.Key{Hour: h, AppID: app, LLMID: 3, Outcome: outcome})
		d.Requests, d.ToolRequests, d.ToolDefs, d.ToolDefTokens, d.InputTokens = 4, 4, 8, defTokens, input
		d.Tools = tools
		d.ToolDefCost = map[string]float64{"USD": float64(defTokens) * 2e-6}
		d.ToolDefCostAtCacheRead = map[string]float64{"USD": float64(defTokens) * 2e-7}
		d.ToolLists = map[string]int64{"0123456789abcdef": 4}
		return d
	}
	rows := []*records.Delta{
		row(1, records.OutcomeApplied, map[string]records.ToolStats{"read_file": {Offered: 4, Called: 3, DefTokens: 600}, "deploy": {Offered: 4, DefTokens: 200}}, 800, 8000),
		row(1, records.OutcomeSkipped, map[string]records.ToolStats{"deploy": {Offered: 4, Called: 1, DefTokens: 200}, "lint": {Offered: 4, DefTokens: 200}, records.OtherTool: {Offered: 4, DefTokens: 400}}, 800, 2000),
		row(2, records.OutcomeAudit, map[string]records.ToolStats{"search": {Offered: 4, Called: 4, DefTokens: 100}}, 100, 1000),
	}
	v := summarise(rows, nil, h, h, savingsQuery{}, 0, map[uint32]string{1: "Agents"}, nil)
	if len(v.Tools) != 2 || v.Tools[0].ID != 1 || v.Tools[0].Name != "Agents" {
		t.Fatalf("tools %+v", v.Tools)
	}
	a := v.Tools[0]
	if a.ToolDefTokens != 1600 || a.InputTokens != 10000 || a.Share != 0.16 || !a.ShareKnown || a.ToolLists != 1 {
		t.Errorf("app 1: %+v", a)
	}
	if !reflect.DeepEqual(a.NeverCalled, []string{"lint"}) || a.NeverCalledDefTokens != 200 {
		t.Errorf("never called %v (%d tokens)", a.NeverCalled, a.NeverCalledDefTokens)
	}
	// lint is 200 of 1,600 definition tokens: an eighth of their cost
	if got, want := a.EstimatedSaving["USD"], 1600*2e-6/8; got < want*0.999 || got > want*1.001 {
		t.Errorf("saving %v, want %v", got, want)
	}
	if got, want := a.EstimatedSavingAtCacheRead["USD"], 1600*2e-7/8; got < want*0.999 || got > want*1.001 {
		t.Errorf("saving at cache read %v, want %v", got, want)
	}
	if v.Tools[1].ID != 2 || len(v.Tools[1].NeverCalled) != 0 {
		t.Errorf("app 2: %+v", v.Tools[1])
	}
}
