package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// A called name the request never offered is model output: a
// prompt-injected model can "call" a tool named after a secret it saw. It
// is recorded only as "other", never by name, on the edge and in Studio.
func TestCalledToolsMustBeOffered(t *testing.T) {
	p, ctx := hookPlugin(t, `{"mode":"audit"}`)
	rec := &recorded{}
	p.SetRecorder(rec)
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"go"}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{}}}]}`)
	p.HandlePostAuth(ctx, request(body, nil))
	resp := `{"choices":[{"message":{"tool_calls":[` +
		`{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}},` +
		`{"id":"c2","type":"function","function":{"name":"sk-live_SECRET123","arguments":"{}"}},` +
		`{"id":"c3","type":"function","function":{"name":"ghp_SECRET456","arguments":"{}"}}]}}]}`
	p.OnBeforeWrite(ctx, &pb.ResponseWriteRequest{Body: []byte(resp)})
	if len(rec.reqs) != 1 || !reflect.DeepEqual(rec.reqs[0].CalledTools, []string{records.OtherTool, "read_file"}) && !reflect.DeepEqual(rec.reqs[0].CalledTools, []string{"read_file", records.OtherTool}) {
		t.Fatalf("called %v", rec.reqs)
	}
	// Studio folds them too, whatever an edge sends
	r := rec.reqs[0]
	r.CalledTools = []string{"sk-live_SECRET123", "read_file"}
	d := records.NewDelta(records.KeyOf(r))
	d.AddRequest(r, records.Price{}, false)
	if _, ok := d.Tools["sk-live_SECRET123"]; ok || d.Tools[records.OtherTool].Called != 1 || d.Tools["read_file"].Called != 1 {
		t.Errorf("tools %+v", d.Tools)
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "SECRET") {
		t.Errorf("the delta carries a called-only name: %s", raw)
	}
}

// A definition drop_tools removed never reached the vendor: it is not
// counted against the vendor's prompt tokens (no share over 100%), and it
// is listed as dropped by policy, not as a never-called suggestion with a
// saving that is already realised (eac.81). In audit mode nothing is
// removed, so it counts as sent.
func TestDroppedToolsAreNotCountedAsSent(t *testing.T) {
	def := func(name string) string {
		return `{"type":"function","function":{"name":"` + name + `","description":"` + strings.Repeat("d", 400) + `","parameters":{}}}`
	}
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"go"}],"tools":[` + def("read_file") + `,` + def("deploy_prod") + `]}`)
	resp := `{"choices":[{"message":{"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}]}}],"usage":{"prompt_tokens":400}}`
	for _, mode := range []string{"enforce", "audit"} {
		p, ctx := hookPlugin(t, `{"mode":"`+mode+`","policies":{"default":{"drop_tools":["deploy_*"]}}}`)
		rec := &recorded{}
		p.SetRecorder(rec)
		p.HandlePostAuth(ctx, request(body, nil))
		p.OnBeforeWrite(ctx, &pb.ResponseWriteRequest{Body: []byte(resp)})
		if len(rec.reqs) != 1 {
			t.Fatalf("%s: %d records", mode, len(rec.reqs))
		}
		r := rec.reqs[0]
		dropped := mode == "enforce"
		if len(r.OfferedTools) != 2 || r.OfferedTools[1].Name != "deploy_prod" || r.OfferedTools[1].Dropped != dropped || r.OfferedTools[0].Dropped {
			t.Fatalf("%s: offered %+v", mode, r.OfferedTools)
		}
		wantDefs, wantTokens := 2, r.OfferedTools[0].Tokens+r.OfferedTools[1].Tokens
		if dropped {
			wantDefs, wantTokens = 1, r.OfferedTools[0].Tokens
		}
		if r.ToolDefs != wantDefs || r.ToolDefTokens != wantTokens {
			t.Errorf("%s: %d definitions, %d tokens; want %d, %d", mode, r.ToolDefs, r.ToolDefTokens, wantDefs, wantTokens)
		}

		d := records.NewDelta(records.KeyOf(r))
		d.AddRequest(r, records.Price{Currency: "USD", Input: 1e-6}, true)
		v := summarise([]*records.Delta{d}, nil, d.Key.Hour, d.Key.Hour, savingsQuery{}, 0, nil, nil)
		a := v.Tools[0]
		if a.Share != float64(wantTokens)/400 || a.Share > 1 {
			t.Errorf("%s: share %v", mode, a.Share)
		}
		if dropped {
			if len(a.NeverCalled) != 0 || !reflect.DeepEqual(a.DroppedByPolicy, []string{"deploy_prod"}) || a.EstimatedSaving["USD"] != 0 {
				t.Errorf("enforce: never called %v, dropped %v, saving %v", a.NeverCalled, a.DroppedByPolicy, a.EstimatedSaving)
			}
		} else if !reflect.DeepEqual(a.NeverCalled, []string{"deploy_prod"}) || len(a.DroppedByPolicy) != 0 || a.EstimatedSaving["USD"] <= 0 {
			t.Errorf("audit: never called %v, dropped %v, saving %v", a.NeverCalled, a.DroppedByPolicy, a.EstimatedSaving)
		}
	}
}

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
