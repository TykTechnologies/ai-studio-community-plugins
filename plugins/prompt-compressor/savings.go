package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
)

// The savings dashboard (Studio UI) reads the hourly rollups through the
// getSavings RPC. It returns aggregates only: counts, sums, App and LLM
// names. Applied (enforce) and audit (projected) savings are kept apart.

type savingsQuery struct {
	RangeHours int    `json:"range_hours"`
	AppID      uint32 `json:"app_id"`
	LLMID      uint32 `json:"llm_id"`

	// bucket is the hours per series point (bucketFor the range).
	bucket int64
}

// bucketFor picks the series resolution for a range: hourly up to three
// days, six-hourly up to 30 days, daily beyond, so a long range neither
// reads nor draws thousands of points.
func bucketFor(rangeHours int) int64 {
	switch {
	case rangeHours <= 72:
		return 1
	case rangeHours <= 720:
		return 6
	default:
		return 24
	}
}

const (
	defaultRangeHours = 24
	maxRangeHours     = 90 * 24
	// alertBeforeHours is the window before a policy change the cache-read
	// share is compared with.
	alertBeforeHours = 6
)

type savingsTotals struct {
	Requests int64 `json:"requests"`
	// TokensSaved is what enforce mode saved; ProjectedTokensSaved what
	// audit mode would have saved. Both are estimates.
	TokensSaved          int64 `json:"tokens_saved"`
	ProjectedTokensSaved int64 `json:"projected_tokens_saved"`
	// CostAtInput prices the saved tokens at the model's input price: an
	// upper bound. CostCacheAdjusted prices the cache-read share of them
	// at the cache-read price; it is absent when a model has no cache-read
	// price or the share is unknown (CacheAdjustedNote says why).
	CostAtInput          map[string]float64 `json:"cost_at_input"`
	CostCacheAdjusted    map[string]float64 `json:"cost_cache_adjusted,omitempty"`
	CacheAdjustedNote    string             `json:"cache_adjusted_note,omitempty"`
	ProjectedCostAtInput map[string]float64 `json:"projected_cost_at_input"`
	UnpricedTokensSaved  int64              `json:"unpriced_tokens_saved"`

	InputTokens     int64   `json:"input_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CacheReadShare  float64 `json:"cache_read_share"`
	ShareKnown      bool    `json:"share_known"`
	UsageMissing    int64   `json:"usage_missing"`

	Rejected   int64 `json:"rejected"`
	FailedOpen int64 `json:"failed_open"`
	// FailedClosed counts requests refused with a 502 because the engine
	// failed under failure_mode closed.
	FailedClosed int64 `json:"failed_closed"`
	// Added latency of the Compress call, as the upper bound of the
	// histogram bucket holding the percentile; -1 is above the last bound.
	LatencyP50Micros int64 `json:"latency_p50_micros"`
	LatencyP95Micros int64 `json:"latency_p95_micros"`
}

type hourPoint struct {
	Hour                 int64 `json:"hour"`
	Requests             int64 `json:"requests"`
	TokensSaved          int64 `json:"tokens_saved"`
	ProjectedTokensSaved int64 `json:"projected_tokens_saved"`
	InputTokens          int64 `json:"input_tokens"`
	CacheReadTokens      int64 `json:"cache_read_tokens"`
}

type groupRow struct {
	ID                   uint32             `json:"id"`
	Name                 string             `json:"name"`
	Requests             int64              `json:"requests"`
	TokensSaved          int64              `json:"tokens_saved"`
	ProjectedTokensSaved int64              `json:"projected_tokens_saved"`
	CostAtInput          map[string]float64 `json:"cost_at_input"`
	InputTokens          int64              `json:"input_tokens"`
	CacheReadTokens      int64              `json:"cache_read_tokens"`
	Overflow             bool               `json:"overflow,omitempty"`
}

type markerView struct {
	Hour      int64  `json:"hour"`
	Name      string `json:"name"`
	ShortHash string `json:"short_hash"`
	Level     int    `json:"level"`
}

type cacheAlert struct {
	Hour   int64   `json:"hour"`
	Policy string  `json:"policy"`
	Before float64 `json:"before"`
	After  float64 `json:"after"`
}

type savingsView struct {
	From      int64            `json:"from"`
	To        int64            `json:"to"`
	Totals    savingsTotals    `json:"totals"`
	ByOutcome map[string]int64 `json:"by_outcome"`
	// ByReason counts requests by skip or reject reason.
	ByReason   map[string]int64                 `json:"by_reason"`
	Series     []hourPoint                      `json:"series"`
	ByApp      []groupRow                       `json:"by_app"`
	ByLLM      []groupRow                       `json:"by_llm"`
	Strategies map[string]records.StrategyStats `json:"strategies"`
	Markers    []markerView                     `json:"markers"`
	Alerts     []cacheAlert                     `json:"alerts"`
	// AuditOnly: requests were only audited, so every saving is projected.
	AuditOnly bool `json:"audit_only"`
	// Empty: no records in the range.
	Empty bool `json:"empty"`
	// OverflowExcluded counts requests in overflow rows a filter left out:
	// they cannot be attributed to an App or LLM.
	OverflowExcluded int64   `json:"overflow_excluded"`
	AlertThreshold   float64 `json:"alert_threshold"`
	// BucketHours is the hours per series point.
	BucketHours int64 `json:"bucket_hours"`
	// Partial: the query stopped at its read budget; hours before
	// PartialFrom are missing or incomplete.
	Partial     bool  `json:"partial"`
	PartialFrom int64 `json:"partial_from,omitempty"`
	// FoldedRows counts rows the query merged into "other" past its row
	// budget.
	FoldedRows int64 `json:"folded_rows,omitempty"`
	// Tools is the tool-definition cost per App (eac.39), the Apps whose
	// definitions cost most first.
	Tools []appTools `json:"tools"`
}

// appTools is what one App's tool definitions cost in the range, and
// which tools its models never called.
type appTools struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	// Requests offered at least one tool; ToolDefs and ToolDefTokens sum
	// their definitions, InputTokens the App's prompt tokens.
	Requests      int64 `json:"requests"`
	ToolDefs      int64 `json:"tool_defs"`
	ToolDefTokens int64 `json:"tool_def_tokens"`
	InputTokens   int64 `json:"input_tokens"`
	// Share is ToolDefTokens / InputTokens, when the vendor reported usage.
	Share      float64   `json:"share"`
	ShareKnown bool      `json:"share_known"`
	Tools      []toolRow `json:"tools"`
	// NeverCalled are the tools offered but never called in the range
	// ("other", the folded names, is never listed).
	NeverCalled          []string `json:"never_called"`
	NeverCalledDefTokens int64    `json:"never_called_def_tokens"`
	// EstimatedSaving is the never-called tools' share of the definitions'
	// cost at the model's input price, and at its cache-read price, by
	// currency: what dropping them would have saved.
	EstimatedSaving            map[string]float64 `json:"estimated_saving"`
	EstimatedSavingAtCacheRead map[string]float64 `json:"estimated_saving_at_cache_read"`
	// ToolLists is the number of tool list versions seen (a change breaks
	// the prompt cache).
	ToolLists int `json:"tool_lists"`
}

type toolRow struct {
	Name      string `json:"name"`
	Offered   int64  `json:"offered"`
	Called    int64  `json:"called"`
	DefTokens int64  `json:"def_tokens"`
}

// Bounds on the tools section.
const (
	maxToolApps    = 50
	maxToolsPerApp = 100
)

// toolAcc sums one App's tool rows.
type toolAcc struct {
	v      appTools
	tools  map[string]records.ToolStats
	cost   map[string]float64
	costCR map[string]float64
	lists  map[string]bool
}

func (a *toolAcc) add(d *records.Delta) {
	a.v.InputTokens += d.InputTokens
	if d.ToolRequests == 0 {
		return
	}
	a.v.Requests += d.ToolRequests
	a.v.ToolDefs += d.ToolDefs
	a.v.ToolDefTokens += d.ToolDefTokens
	for n, s := range d.Tools {
		cur := a.tools[n]
		cur.Offered += s.Offered
		cur.Called += s.Called
		cur.DefTokens += s.DefTokens
		a.tools[n] = cur
	}
	for c, x := range d.ToolDefCost {
		a.cost[c] += x
	}
	for c, x := range d.ToolDefCostAtCacheRead {
		a.costCR[c] += x
	}
	for h := range d.ToolLists {
		a.lists[h] = true
	}
}

func (a *toolAcc) view() appTools {
	v := a.v
	v.ToolLists = len(a.lists)
	if v.InputTokens > 0 {
		v.Share, v.ShareKnown = float64(v.ToolDefTokens)/float64(v.InputTokens), true
	}
	v.Tools, v.NeverCalled = []toolRow{}, []string{}
	for n, s := range a.tools {
		v.Tools = append(v.Tools, toolRow{Name: n, Offered: s.Offered, Called: s.Called, DefTokens: s.DefTokens})
		if s.Offered > 0 && s.Called == 0 && n != records.OtherTool {
			v.NeverCalled = append(v.NeverCalled, n)
			v.NeverCalledDefTokens += s.DefTokens
		}
	}
	sort.Slice(v.Tools, func(i, j int) bool {
		if v.Tools[i].DefTokens != v.Tools[j].DefTokens {
			return v.Tools[i].DefTokens > v.Tools[j].DefTokens
		}
		return v.Tools[i].Name < v.Tools[j].Name
	})
	if len(v.Tools) > maxToolsPerApp {
		v.Tools = v.Tools[:maxToolsPerApp]
	}
	sort.Strings(v.NeverCalled)
	if len(v.NeverCalled) > maxToolsPerApp {
		v.NeverCalled = v.NeverCalled[:maxToolsPerApp]
	}
	v.EstimatedSaving, v.EstimatedSavingAtCacheRead = map[string]float64{}, map[string]float64{}
	if v.ToolDefTokens > 0 {
		share := float64(v.NeverCalledDefTokens) / float64(v.ToolDefTokens)
		for c, x := range a.cost {
			v.EstimatedSaving[c] = x * share
		}
		for c, x := range a.costCR {
			v.EstimatedSavingAtCacheRead[c] = x * share
		}
	}
	return v
}

// summarise turns rollup rows into the dashboard view. rows and markers
// are what records.Store.Query returned for from..to; names maps App and
// LLM ids to names.
func summarise(rows []*records.Delta, markers []records.PolicyMarker, from, to int64, q savingsQuery, threshold float64, appNames, llmNames map[uint32]string) *savingsView {
	v := &savingsView{
		From: from, To: to, ByOutcome: map[string]int64{}, ByReason: map[string]int64{}, Strategies: map[string]records.StrategyStats{},
		Series: []hourPoint{}, ByApp: []groupRow{}, ByLLM: []groupRow{}, Markers: []markerView{}, Alerts: []cacheAlert{},
		AlertThreshold: threshold, Tools: []appTools{},
	}
	toolApps := map[uint32]*toolAcc{}
	t := &v.Totals
	t.CostAtInput, t.ProjectedCostAtInput = map[string]float64{}, map[string]float64{}
	adjusted := map[string]float64{}
	adjustedOK := true
	latency := make([]int64, len(records.LatencyBoundsMicros)+1)
	series := map[int64]*hourPoint{}
	usedHashes := map[string]bool{}
	apps, llms := map[uint32]*groupRow{}, map[uint32]*groupRow{}

	group := func(m map[uint32]*groupRow, id uint32, names map[uint32]string, overflow bool) *groupRow {
		g := m[id]
		if g == nil {
			g = &groupRow{ID: id, Name: names[id], CostAtInput: map[string]float64{}, Overflow: overflow}
			m[id] = g
		}
		return g
	}

	for _, d := range rows {
		if d.Hour > to {
			continue
		}
		if (q.AppID != 0 && d.AppID != q.AppID) || (q.LLMID != 0 && d.LLMID != q.LLMID) {
			if d.Overflow && d.Hour >= from {
				v.OverflowExcluded += d.Requests
			}
			continue
		}
		// Hours before the range feed only the series, for the cache-drop
		// comparison; the caller trims them
		p := series[d.Hour]
		if p == nil {
			p = &hourPoint{Hour: d.Hour}
			series[d.Hour] = p
		}
		p.Requests += d.Requests
		p.InputTokens += d.InputTokens
		p.CacheReadTokens += d.CacheReadTokens
		if d.Hour < from {
			continue
		}
		if d.PolicyHash != "" {
			usedHashes[d.PolicyHash] = true
		}

		applied := d.Outcome == records.OutcomeApplied
		audit := d.Outcome == records.OutcomeAudit

		t.Requests += d.Requests
		v.ByOutcome[d.Outcome] += d.Requests
		for name, n := range d.Reasons {
			v.ByReason[name] += n
		}
		t.InputTokens += d.InputTokens
		t.CacheReadTokens += d.CacheReadTokens
		t.UsageMissing += d.UsageMissing
		switch d.Outcome {
		case records.OutcomeRejected:
			t.Rejected += d.Requests
		case records.OutcomeFailedOpen:
			t.FailedOpen += d.Requests
		case records.OutcomeFailedClosed:
			t.FailedClosed += d.Requests
		}
		for i, n := range d.LatencyBuckets {
			if i < len(latency) {
				latency[i] += n
			}
		}

		var ga, gl *groupRow
		if d.Overflow {
			ga, gl = group(apps, 0, nil, true), group(llms, 0, nil, true)
		} else {
			ga, gl = group(apps, d.AppID, appNames, false), group(llms, d.LLMID, llmNames, false)
		}
		for _, g := range []*groupRow{ga, gl} {
			g.Requests += d.Requests
			g.InputTokens += d.InputTokens
			g.CacheReadTokens += d.CacheReadTokens
		}
		if !d.Overflow {
			acc := toolApps[d.AppID]
			if acc == nil {
				acc = &toolAcc{v: appTools{ID: d.AppID, Name: appNames[d.AppID]}, tools: map[string]records.ToolStats{},
					cost: map[string]float64{}, costCR: map[string]float64{}, lists: map[string]bool{}}
				toolApps[d.AppID] = acc
			}
			acc.add(d)
		}

		if !applied && !audit {
			continue
		}
		for name, s := range d.Strategies {
			cur := v.Strategies[name]
			cur.Leaves += s.Leaves
			cur.BytesSaved += s.BytesSaved
			v.Strategies[name] = cur
		}
		if audit {
			t.ProjectedTokensSaved += d.TokensSaved
			p.ProjectedTokensSaved += d.TokensSaved
			ga.ProjectedTokensSaved += d.TokensSaved
			gl.ProjectedTokensSaved += d.TokensSaved
			for c, x := range d.CostSaved {
				t.ProjectedCostAtInput[c] += x
			}
			continue
		}
		t.TokensSaved += d.TokensSaved
		p.TokensSaved += d.TokensSaved
		ga.TokensSaved += d.TokensSaved
		gl.TokensSaved += d.TokensSaved
		t.UnpricedTokensSaved += d.UnpricedTokensSaved
		for c, x := range d.CostSaved {
			t.CostAtInput[c] += x
			ga.CostAtInput[c] += x
			gl.CostAtInput[c] += x
		}
		// The share of this row's prompt tokens that were cache reads
		// prices that share of its saving at the cache-read price
		if d.TokensSaved > 0 {
			if d.NoCacheReadPriceTokensSaved > 0 || d.InputTokens <= 0 {
				adjustedOK = false
				continue
			}
			share := float64(d.CacheReadTokens) / float64(d.InputTokens)
			for c, x := range d.CostSaved {
				adjusted[c] += (1-share)*x + share*d.CostSavedAtCacheRead[c]
			}
		}
	}

	if t.InputTokens > 0 {
		t.CacheReadShare = float64(t.CacheReadTokens) / float64(t.InputTokens)
		t.ShareKnown = true
	}
	switch {
	case t.TokensSaved == 0:
	case !adjustedOK:
		t.CacheAdjustedNote = "Not available: some saved tokens have no cache-read price or no usage from the vendor."
	default:
		t.CostCacheAdjusted = adjusted
	}
	t.LatencyP50Micros, t.LatencyP95Micros = percentile(latency, 0.50), percentile(latency, 0.95)
	v.Empty = t.Requests == 0
	v.AuditOnly = !v.Empty && v.ByOutcome[records.OutcomeAudit] > 0 && v.ByOutcome[records.OutcomeApplied] == 0

	for _, p := range series {
		v.Series = append(v.Series, *p)
	}
	sort.Slice(v.Series, func(i, j int) bool { return v.Series[i].Hour < v.Series[j].Hour })
	v.ByApp, v.ByLLM = sortedGroups(apps), sortedGroups(llms)
	for _, acc := range toolApps {
		if acc.v.Requests > 0 {
			v.Tools = append(v.Tools, acc.view())
		}
	}
	sort.Slice(v.Tools, func(i, j int) bool {
		if v.Tools[i].ToolDefTokens != v.Tools[j].ToolDefTokens {
			return v.Tools[i].ToolDefTokens > v.Tools[j].ToolDefTokens
		}
		return v.Tools[i].ID < v.Tools[j].ID
	})
	if len(v.Tools) > maxToolApps {
		v.Tools = v.Tools[:maxToolApps]
	}

	for _, m := range markers {
		// A marker belongs to the view only if its policy served a request
		// the view counts: with an App or LLM filter, another App's policy
		// change is not this App's
		if m.FirstSeen < from || m.FirstSeen > to || !usedHashes[m.Hash] {
			continue
		}
		short := m.Hash
		if len(short) > 12 {
			short = short[:12]
		}
		v.Markers = append(v.Markers, markerView{Hour: m.FirstSeen, Name: m.Name, ShortHash: short, Level: m.Level})
	}
	sort.Slice(v.Markers, func(i, j int) bool { return v.Markers[i].Hour < v.Markers[j].Hour })
	// An hourly series feeds the alert check itself; a bucketed one leaves
	// it to the caller, which reads the hours around each marker
	if q.bucket <= 1 {
		v.Alerts = alertsFor(v.Markers, to, series, threshold)
	}
	return v
}

// maxAlertMarkers bounds the policy changes a query checks for a cache
// drop (each reads its own few hours).
const maxAlertMarkers = 20

// alertsFor checks each policy change shown: the cache-read share in the
// two hours from it against the alertBeforeHours before it, in an hourly
// series. now is the current, still running hour: a change whose two
// hours are not both over is not judged yet, and each window needs
// minAlertRequests requests, so a handful of requests cannot raise it.
func alertsFor(markers []markerView, now int64, hourly map[int64]*hourPoint, threshold float64) []cacheAlert {
	alerts := []cacheAlert{}
	if threshold <= 0 {
		return alerts
	}
	for _, m := range markers {
		if m.Hour+1 >= now {
			continue
		}
		before := windowShare(hourly, m.Hour-alertBeforeHours, m.Hour-1)
		after := windowShare(hourly, m.Hour, m.Hour+1)
		if !before.ok || !after.ok || before.requests < minAlertRequests || after.requests < minAlertRequests {
			continue
		}
		if before.share > 0 && after.share < before.share*(1-threshold) {
			alerts = append(alerts, cacheAlert{Hour: m.Hour, Policy: m.Name, Before: before.share, After: after.share})
		}
	}
	return alerts
}

// minAlertRequests is the fewest requests a window needs for the
// cache-drop comparison.
const minAlertRequests = 10

type shareResult struct {
	share    float64
	requests int64
	ok       bool
}

func windowShare(series map[int64]*hourPoint, from, to int64) shareResult {
	var input, cached, requests int64
	for h := from; h <= to; h++ {
		if p := series[h]; p != nil {
			input += p.InputTokens
			cached += p.CacheReadTokens
			requests += p.Requests
		}
	}
	if input == 0 {
		return shareResult{requests: requests}
	}
	return shareResult{share: float64(cached) / float64(input), requests: requests, ok: true}
}

func percentile(buckets []int64, q float64) int64 {
	var total int64
	for _, n := range buckets {
		total += n
	}
	if total == 0 {
		return 0
	}
	target := int64(q*float64(total) + 0.5)
	if target < 1 {
		target = 1
	}
	var seen int64
	for i, n := range buckets {
		seen += n
		if seen >= target {
			if i < len(records.LatencyBoundsMicros) {
				return records.LatencyBoundsMicros[i]
			}
			return -1
		}
	}
	return -1
}

func sortedGroups(m map[uint32]*groupRow) []groupRow {
	out := make([]groupRow, 0, len(m))
	for _, g := range m {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		si, sj := out[i].TokensSaved+out[i].ProjectedTokensSaved, out[j].TokensSaved+out[j].ProjectedTokensSaved
		if si != sj {
			return si > sj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// nameCache keeps App and LLM names for the dashboard for a few minutes,
// so a refresh does not page through every App and LLM.
type nameCache struct {
	mu      sync.Mutex
	fetched time.Time
	apps    map[uint32]string
	llms    map[uint32]string
}

const nameCacheTTL = 5 * time.Minute

func (n *nameCache) get(ctx context.Context, api studioAPI) (apps, llms map[uint32]string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.apps != nil && time.Since(n.fetched) < nameCacheTTL {
		return n.apps, n.llms
	}
	apps, llms = map[uint32]string{}, map[uint32]string{}
	if api != nil {
		// Names are a convenience: without them the dashboard shows ids
		if list, _, err := listAllApps(ctx, api); err == nil {
			for _, a := range list {
				apps[a.ID] = a.Name
			}
		}
		if list, _, err := listAllLLMs(ctx, api); err == nil {
			for _, l := range list {
				llms[l.ID] = l.Name
			}
		}
	}
	n.apps, n.llms, n.fetched = apps, llms, time.Now()
	return apps, llms
}

// rpcGetSavings answers the dashboard.
func (p *PromptCompressorPlugin) rpcGetSavings(payload []byte) ([]byte, error) {
	store := p.telemetry.store
	if store == nil {
		return nil, errors.New("the savings dashboard needs Studio's plugin KV")
	}
	var q savingsQuery
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &q); err != nil {
			return nil, errors.New("getSavings: payload must be {\"range_hours\": n, \"app_id\": id, \"llm_id\": id}")
		}
	}
	if q.RangeHours <= 0 {
		q.RangeHours = defaultRangeHours
	}
	if q.RangeHours > maxRangeHours {
		q.RangeHours = maxRangeHours
	}
	q.bucket = bucketFor(q.RangeHours)
	key := fmt.Sprintf("%d/%d/%d", q.RangeHours, q.AppID, q.LLMID)
	return p.telemetry.savings.do(key, func() ([]byte, error) { return p.querySavings(store, q) })
}

// querySavings reads the rollups for one dashboard view, within the
// store's per-query bounds.
func (p *PromptCompressorPlugin) querySavings(store *records.Store, q savingsQuery) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), editorRPCTTL)
	defer cancel()
	threshold := p.state().config.CacheDropAlert

	to := records.HourOf(time.Now())
	from := to - int64(q.RangeHours) + 1
	from -= floorMod(from, q.bucket)
	opts := records.QueryOptions{From: from, To: to, Bucket: q.bucket, AppID: q.AppID, LLMID: q.LLMID}
	if q.bucket == 1 {
		// Read a little earlier than shown, for the cache-drop comparison
		opts.From -= alertBeforeHours
	}
	res, err := store.QueryWith(ctx, opts)
	if err != nil {
		return nil, err
	}
	apps, llms := p.telemetry.names.get(ctx, p.studioAPI())
	view := summarise(res.Rows, res.Markers, from, to, q, threshold, apps, llms)
	view.BucketHours = q.bucket
	view.OverflowExcluded += res.OverflowExcluded
	view.Partial, view.PartialFrom = res.Partial, res.ReadFrom
	view.FoldedRows = res.Folded
	// The comparison window before the range only feeds the alert check
	view.Series = trimSeries(view.Series, from)

	if q.bucket > 1 && threshold > 0 {
		// Read the hours around each policy change for the alert check
		hourly := map[int64]*hourPoint{}
		checked := 0
		for _, m := range view.Markers {
			if m.Hour+1 >= to || checked >= maxAlertMarkers {
				continue
			}
			checked++
			win, err := store.QueryWith(ctx, records.QueryOptions{
				From: m.Hour - alertBeforeHours, To: m.Hour + 1, AppID: q.AppID, LLMID: q.LLMID,
				MaxBytes: records.DefaultQueryBytes / maxAlertMarkers,
			})
			if err != nil {
				return nil, err
			}
			for _, d := range win.Rows {
				pt := hourly[d.Hour]
				if pt == nil {
					pt = &hourPoint{Hour: d.Hour}
					hourly[d.Hour] = pt
				}
				pt.Requests += d.Requests
				pt.InputTokens += d.InputTokens
				pt.CacheReadTokens += d.CacheReadTokens
			}
		}
		view.Alerts = alertsFor(view.Markers, to, hourly, threshold)
	}
	return json.Marshal(view)
}

func floorMod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// savingsCache answers the same dashboard view from memory for a short
// time, and runs one query per view at a time however many viewers ask.
type savingsCache struct {
	mu       sync.Mutex
	entries  map[string]savingsEntry
	inflight map[string]*savingsCall
	ttl      time.Duration
	now      func() time.Time
}

type savingsEntry struct {
	data []byte
	at   time.Time
}

type savingsCall struct {
	done chan struct{}
	data []byte
	err  error
}

const (
	savingsCacheTTL     = 30 * time.Second
	savingsCacheEntries = 64
)

func (c *savingsCache) do(key string, query func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	if c.entries == nil {
		c.entries, c.inflight = map[string]savingsEntry{}, map[string]*savingsCall{}
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.ttl == 0 {
		c.ttl = savingsCacheTTL
	}
	if e, ok := c.entries[key]; ok && c.now().Sub(e.at) < c.ttl {
		c.mu.Unlock()
		return e.data, nil
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.data, call.err
	}
	call := &savingsCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	call.data, call.err = query()

	c.mu.Lock()
	delete(c.inflight, key)
	if call.err == nil {
		if len(c.entries) >= savingsCacheEntries {
			c.entries = map[string]savingsEntry{}
		}
		c.entries[key] = savingsEntry{data: call.data, at: c.now()}
	}
	c.mu.Unlock()
	close(call.done)
	return call.data, call.err
}

func trimSeries(s []hourPoint, from int64) []hourPoint {
	out := s[:0]
	for _, p := range s {
		if p.Hour >= from {
			out = append(out, p)
		}
	}
	return out
}
