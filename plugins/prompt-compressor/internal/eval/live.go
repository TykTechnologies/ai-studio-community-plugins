package eval

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"time"
)

// Live suites run real tasks against real LLMs through the Microgateway,
// once per configuration (compression off, then each level) on the same
// tasks and seeds, and feed the paired results to PairedDiff and NonInferior.
// They cost money, so a run needs a plan, a cost estimate and an explicit
// approval that covers the estimate. This file holds the planner and the
// guard; the suite runners land with eac.24.

// SuiteSpec is a live suite and its gate from the eval decision (eac.4).
type SuiteSpec struct {
	Name string `json:"name"`
	// MinTasks and Seeds are what the gate requires for a verdict.
	MinTasks int `json:"min_tasks"`
	Seeds    int `json:"seeds"`
	// Metric and Margin: the non-inferiority margin, in metric points.
	Metric string  `json:"metric"`
	Margin float64 `json:"margin"`
	// Per-task token assumptions for the cost estimate, per run of one
	// task under one configuration and seed. Replace them with measured
	// values once a pilot run reports them.
	InputTokens  int           `json:"input_tokens_per_task"`
	OutputTokens int           `json:"output_tokens_per_task"`
	CacheShare   float64       `json:"assumed_cache_read_share"`
	TaskDuration time.Duration `json:"task_duration"`
}

// Suites are the gate's suites. Token figures are planning assumptions
// (BFCL single calls, long-context QA with a ~30k-token document, SWE-bench
// agent runs of ~40 turns), not measurements.
var Suites = map[string]SuiteSpec{
	"bfcl":     {Name: "bfcl", MinTasks: 1000, Seeds: 1, Metric: "call accuracy", Margin: 1, InputTokens: 1500, OutputTokens: 150, CacheShare: 0.3, TaskDuration: 4 * time.Second},
	"lcqa":     {Name: "lcqa", MinTasks: 500, Seeds: 1, Metric: "F1", Margin: 2, InputTokens: 30000, OutputTokens: 80, CacheShare: 0, TaskDuration: 15 * time.Second},
	"swebench": {Name: "swebench", MinTasks: 200, Seeds: 3, Metric: "resolve rate", Margin: 5, InputTokens: 1_200_000, OutputTokens: 25_000, CacheShare: 0.8, TaskDuration: 12 * time.Minute},
}

// Plan is a reproducible live run: the same suite, task list, size and seed
// always select the same tasks.
type Plan struct {
	Suite   SuiteSpec `json:"suite"`
	Tasks   []string  `json:"tasks"`
	Seeds   []uint64  `json:"seeds"`
	Levels  []int     `json:"levels"`
	Format  string    `json:"format"`
	Seed    uint64    `json:"selection_seed"`
	Runs    int       `json:"runs"`
	Approve *Approval `json:"approval,omitempty"`
}

// NewPlan selects n tasks from all (sorted first, so the input order does
// not matter) with seed, and runs each at level 0 plus every level, for the
// suite's number of seeds.
func NewPlan(suite SuiteSpec, all []string, n int, seed uint64, levels []int, format string) (Plan, error) {
	if n <= 0 || n > len(all) {
		return Plan{}, fmt.Errorf("want 1..%d tasks, got %d", len(all), n)
	}
	ids := append([]string(nil), all...)
	sort.Strings(ids)
	r := rand.New(rand.NewPCG(seed, seed^0x5eed))
	r.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	ids = ids[:n]
	sort.Strings(ids)
	cfgs := append([]int{0}, levels...)
	cfgs = uniqueInts(cfgs)
	seeds := make([]uint64, max(suite.Seeds, 1))
	for i := range seeds {
		seeds[i] = seed + uint64(i)
	}
	return Plan{Suite: suite, Tasks: ids, Seeds: seeds, Levels: cfgs, Format: format, Seed: seed, Runs: n * len(seeds) * len(cfgs)}, nil
}

func uniqueInts(xs []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Ints(out)
	return out
}

// Estimate is a plan's projected spend.
type Estimate struct {
	Runs         int           `json:"runs"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	USD          float64       `json:"usd"`
	Duration     time.Duration `json:"duration"`
	Concurrency  int           `json:"concurrency"`
	// MeetsGate: the plan is large enough for a gate verdict.
	MeetsGate bool `json:"meets_gate"`
}

// Estimate prices the plan at p, assuming the suite's per-task tokens and
// cache share (cache reads billed at the cache price, the rest as input;
// compressed configurations are priced like the baseline, so the estimate
// is an upper bound).
func (pl Plan) Estimate(p Price, concurrency int) Estimate {
	concurrency = max(concurrency, 1)
	s := pl.Suite
	e := Estimate{Runs: pl.Runs, Concurrency: concurrency}
	e.InputTokens = int64(pl.Runs) * int64(s.InputTokens)
	e.OutputTokens = int64(pl.Runs) * int64(s.OutputTokens)
	read := int(float64(s.InputTokens) * s.CacheShare)
	perRun := p.Cost(Usage{Input: s.InputTokens - read, CacheRead: read, Output: s.OutputTokens})
	e.USD = perRun * float64(pl.Runs)
	e.Duration = time.Duration(int64(s.TaskDuration) * int64(pl.Runs) / int64(concurrency))
	e.MeetsGate = len(pl.Tasks) >= s.MinTasks && len(pl.Seeds) >= s.Seeds
	return e
}

// Approval is the lead's go-ahead for a paid run, recorded with the plan.
type Approval struct {
	By     string  `json:"by"`
	Note   string  `json:"note"`
	MaxUSD float64 `json:"max_usd"`
}

// ErrNeedsApproval: a live run without an approval covering its estimate.
var ErrNeedsApproval = errors.New("eval: live run needs an approval covering the estimated cost (send the plan and estimate to the lead first)")

// ErrNoRunner: the suite runner is not built yet.
var ErrNoRunner = errors.New("eval: no runner for this suite yet")

// Authorize checks a plan may spend money: an approval with a name and a
// cap at or above the estimate. Dry runs never call it.
func (pl Plan) Authorize(est Estimate) error {
	a := pl.Approve
	if a == nil || a.By == "" || a.MaxUSD <= 0 || est.USD > a.MaxUSD {
		return ErrNeedsApproval
	}
	return nil
}
