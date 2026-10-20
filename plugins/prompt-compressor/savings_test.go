package main

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
)

const hashOld = "1111111111111111111111111111111111111111111111111111111111111111"
const hashNew = "2222222222222222222222222222222222222222222222222222222222222222"

func row(hour int64, app, llm uint32, hash, outcome string, requests, saved, input, cached int64) *records.Delta {
	d := records.NewDelta(records.Key{Hour: hour, AppID: app, LLMID: llm, PolicyHash: hash, Outcome: outcome})
	d.PolicyName, d.Level = "agents", 2
	d.Requests, d.TokensSaved, d.InputTokens, d.CacheReadTokens = requests, saved, input, cached
	if saved > 0 {
		d.CostSaved = map[string]float64{"USD": float64(saved) * 0.000003}
		d.CostSavedAtCacheRead = map[string]float64{"USD": float64(saved) * 0.0000003}
	}
	d.LatencyBuckets[2] = requests // 250µs
	return d
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestSummariseSeparatesAppliedFromProjected(t *testing.T) {
	h := int64(500000)
	rows := []*records.Delta{
		row(h, 1, 3, hashOld, records.OutcomeApplied, 10, 1000, 10000, 5000),
		row(h, 2, 3, hashOld, records.OutcomeAudit, 5, 400, 2000, 0),
		row(h+1, 1, 4, hashOld, records.OutcomeSkipped, 3, 0, 900, 900),
		row(h+1, 1, 4, hashOld, records.OutcomeRejected, 1, 0, 100, 0),
		row(h-5, 1, 3, hashOld, records.OutcomeApplied, 99, 9999, 1, 1), // before the range
	}
	v := summarise(rows, nil, h, h+2, savingsQuery{}, 0.25, map[uint32]string{1: "Agents app"}, map[uint32]string{3: "Claude"})
	tt := v.Totals
	if tt.Requests != 19 || tt.TokensSaved != 1000 || tt.ProjectedTokensSaved != 400 || tt.Rejected != 1 {
		t.Fatalf("totals = %+v", tt)
	}
	if !near(tt.CostAtInput["USD"], 1000*0.000003) || !near(tt.ProjectedCostAtInput["USD"], 400*0.000003) {
		t.Errorf("costs = %v / %v", tt.CostAtInput, tt.ProjectedCostAtInput)
	}
	// Half of the applied row's prompt was cache reads: half its saving is
	// priced at the cache-read price
	want := 0.5*1000*0.000003 + 0.5*1000*0.0000003
	if !near(tt.CostCacheAdjusted["USD"], want) || tt.CacheAdjustedNote != "" {
		t.Errorf("cache-adjusted = %v (%q), want %v", tt.CostCacheAdjusted, tt.CacheAdjustedNote, want)
	}
	if !tt.ShareKnown || !near(tt.CacheReadShare, 5900.0/13000.0) {
		t.Errorf("cache-read share = %v", tt.CacheReadShare)
	}
	if tt.LatencyP50Micros != 250 || tt.LatencyP95Micros != 250 {
		t.Errorf("latency p50/p95 = %d/%d", tt.LatencyP50Micros, tt.LatencyP95Micros)
	}
	if v.ByOutcome[records.OutcomeApplied] != 10 || v.ByOutcome[records.OutcomeAudit] != 5 {
		t.Errorf("by outcome = %v", v.ByOutcome)
	}
	if len(v.ByApp) != 2 || v.ByApp[0].Name != "Agents app" || v.ByApp[0].TokensSaved != 1000 {
		t.Errorf("by app = %+v", v.ByApp)
	}
	if v.AuditOnly || v.Empty {
		t.Error("flags wrong")
	}
}

func TestCacheAdjustedNeedsPricesAndUsage(t *testing.T) {
	h := int64(500000)
	r := row(h, 1, 3, hashOld, records.OutcomeApplied, 1, 100, 1000, 100)
	r.NoCacheReadPriceTokensSaved = 100
	v := summarise([]*records.Delta{r}, nil, h, h, savingsQuery{}, 0, nil, nil)
	if v.Totals.CostCacheAdjusted != nil || v.Totals.CacheAdjustedNote == "" {
		t.Errorf("without a cache-read price: %v, %q", v.Totals.CostCacheAdjusted, v.Totals.CacheAdjustedNote)
	}
	r = row(h, 1, 3, hashOld, records.OutcomeApplied, 1, 100, 0, 0)
	r.UsageMissing = 1
	v = summarise([]*records.Delta{r}, nil, h, h, savingsQuery{}, 0, nil, nil)
	if v.Totals.CostCacheAdjusted != nil || v.Totals.ShareKnown {
		t.Errorf("without usage: %v, share known %v", v.Totals.CostCacheAdjusted, v.Totals.ShareKnown)
	}
}

func TestEmptyAndAuditOnly(t *testing.T) {
	v := summarise(nil, nil, 1, 24, savingsQuery{}, 0.25, nil, nil)
	if !v.Empty || v.AuditOnly || v.Totals.Requests != 0 || v.Series == nil || v.ByApp == nil || v.Alerts == nil {
		t.Errorf("empty view = %+v", v)
	}
	b, _ := json.Marshal(v)
	var back map[string]interface{}
	_ = json.Unmarshal(b, &back)
	if back["series"] == nil || back["markers"] == nil {
		t.Error("empty lists encode as null")
	}

	h := int64(500000)
	v = summarise([]*records.Delta{row(h, 1, 3, hashOld, records.OutcomeAudit, 4, 400, 100, 10)}, nil, h, h, savingsQuery{}, 0.25, nil, nil)
	if !v.AuditOnly || v.Totals.TokensSaved != 0 || v.Totals.ProjectedTokensSaved != 400 {
		t.Errorf("audit-only view = %+v", v.Totals)
	}
}

func TestFiltersAndOverflowRows(t *testing.T) {
	h := int64(500000)
	over := records.NewDelta(records.Key{Hour: h, Outcome: records.OutcomeApplied})
	over.Overflow, over.Requests, over.TokensSaved = true, 7, 70
	rows := []*records.Delta{row(h, 1, 3, hashOld, records.OutcomeApplied, 2, 20, 0, 0), row(h, 2, 4, hashOld, records.OutcomeApplied, 3, 30, 0, 0), over}

	all := summarise(rows, nil, h, h, savingsQuery{}, 0, nil, nil)
	if all.Totals.TokensSaved != 120 {
		t.Errorf("unfiltered saved %d, want 120 (overflow included)", all.Totals.TokensSaved)
	}
	var sawOverflow bool
	for _, g := range all.ByApp {
		sawOverflow = sawOverflow || (g.Overflow && g.Requests == 7)
	}
	if !sawOverflow {
		t.Error("the overflow row is not its own group")
	}

	one := summarise(rows, nil, h, h, savingsQuery{AppID: 2}, 0, nil, nil)
	if one.Totals.TokensSaved != 30 || one.OverflowExcluded != 7 {
		t.Errorf("App 2: saved %d, overflow excluded %d", one.Totals.TokensSaved, one.OverflowExcluded)
	}
	llm := summarise(rows, nil, h, h, savingsQuery{LLMID: 3}, 0, nil, nil)
	if llm.Totals.TokensSaved != 20 {
		t.Errorf("LLM 3: saved %d", llm.Totals.TokensSaved)
	}
}

func TestPolicyMarkersAndCacheDropAlert(t *testing.T) {
	h := int64(500000)
	var rows []*records.Delta
	// Six hours at a 60% cache-read share, then the new policy at 20%
	for i := int64(1); i <= 6; i++ {
		rows = append(rows, row(h-i, 1, 3, hashOld, records.OutcomeApplied, 10, 100, 1000, 600))
	}
	rows = append(rows, row(h, 1, 3, hashNew, records.OutcomeApplied, 10, 300, 1000, 200))
	markers := []records.PolicyMarker{{Hash: hashOld, FirstSeen: h - 300}, {Hash: hashNew, FirstSeen: h, Name: "max", Level: 3}}

	v := summarise(rows, markers, h-2, h+1, savingsQuery{}, 0.25, nil, nil)
	if len(v.Markers) != 1 || v.Markers[0].Hour != h || v.Markers[0].ShortHash != hashNew[:12] || v.Markers[0].Name != "max" {
		t.Errorf("markers = %+v", v.Markers)
	}
	if len(v.Alerts) != 1 || !near(v.Alerts[0].Before, 0.6) || !near(v.Alerts[0].After, 0.2) || v.Alerts[0].Policy != "max" {
		t.Errorf("alerts = %+v", v.Alerts)
	}
	// Totals only count the range, though the series fed the comparison
	if v.Totals.Requests != 30 {
		t.Errorf("requests %d, want 30 (hours h-2..h)", v.Totals.Requests)
	}
	if q := summarise(rows, markers, h-2, h+1, savingsQuery{}, 0, nil, nil); len(q.Alerts) != 0 {
		t.Error("an alert with the warning turned off")
	}
	if q := summarise(rows, markers, h-2, h+1, savingsQuery{}, 0.9, nil, nil); len(q.Alerts) != 0 {
		t.Error("a 67% drop alerted at a 90% threshold")
	}
}

func TestPercentile(t *testing.T) {
	b := make([]int64, len(records.LatencyBoundsMicros)+1)
	b[0], b[3], b[len(b)-1] = 50, 45, 5
	if p := percentile(b, 0.5); p != records.LatencyBoundsMicros[0] {
		t.Errorf("p50 = %d", p)
	}
	if p := percentile(b, 0.95); p != records.LatencyBoundsMicros[3] {
		t.Errorf("p95 = %d", p)
	}
	if p := percentile(b, 0.99); p != -1 {
		t.Errorf("p99 = %d, want above the last bound", p)
	}
	if p := percentile(make([]int64, 3), 0.5); p != 0 {
		t.Errorf("empty p50 = %d", p)
	}
}

// The RPC end to end: an edge batch applied in Studio shows on the
// dashboard, and the header sum (what the edge recorded) equals the
// dashboard's tokens saved.
func TestGetSavingsRPC(t *testing.T) {
	kv := &memKV{data: map[string][]byte{}}
	studio := studioPlugin(t, kv)
	studio.testStudio = &fakeStudio{}

	gw := &telemetryGateway{}
	edge := edgePlugin(t, gw)
	var headerSum int64
	for i := 0; i < 25; i++ {
		r := savingsRecord(uint32(i%2+1), 900+i, 500, records.OutcomeApplied)
		headerSum += int64(400 + i)
		edge.recorder().Add(r)
	}
	edge.flushRecords()
	deliver(t, studio, "edge-1", gw.payloads())

	out, err := studio.HandleRPC("getSavings", []byte(`{"range_hours": 3, "_service_broker_id": 2}`))
	if err != nil {
		t.Fatal(err)
	}
	var v savingsView
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	if v.Totals.TokensSaved != headerSum || v.Totals.Requests != 25 || v.To-v.From != 2 {
		t.Errorf("dashboard %d saved / %d requests over %d..%d; edge recorded %d / 25", v.Totals.TokensSaved, v.Totals.Requests, v.From, v.To, headerSum)
	}
	for _, p := range v.Series {
		if p.Hour < v.From {
			t.Errorf("series holds hour %d before the range", p.Hour)
		}
	}
	if _, err := studio.HandleRPC("getSavings", []byte(`not json`)); err == nil {
		t.Error("a malformed query was accepted")
	}
	// An edge has no dashboard
	if _, err := edge.HandleRPC("getSavings", nil); err == nil {
		t.Error("an edge answered getSavings")
	}
	_ = plugin_sdk.RuntimeStudio
	_ = context.Background()
	_ = time.Now()
}
