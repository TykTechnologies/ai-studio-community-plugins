package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// LiveReport is a live run's result: each configuration's metric, turns,
// tokens, cache-read share and cost, and each compressed level against
// compression off on the same tasks and seeds.
type LiveReport struct {
	Suite  string `json:"suite"`
	Metric string `json:"metric"`
	Tasks  int    `json:"tasks"`
	Seeds  int    `json:"seeds"`
	// DryRun: the upstream was the oracle mock, so the scores measure
	// whether compression kept each task's evidence, not a model.
	DryRun      bool            `json:"dry_run"`
	Configs     []ConfigSummary `json:"configs"`
	Comparisons []Comparison    `json:"comparisons"`
}

// ConfigSummary is one configuration over every task and seed.
type ConfigSummary struct {
	Level  int `json:"level"`
	Runs   int `json:"runs"`
	Errors int `json:"errors"`
	// Metric is the mean score (0-100) with a 95% interval.
	Metric Interval `json:"metric"`
	// EM is the QA suite's exact-match mean; MalformedRate BFCL's share of
	// answers with a malformed call.
	EM             float64 `json:"em,omitempty"`
	MalformedRate  float64 `json:"malformed_rate,omitempty"`
	TurnsPerTask   float64 `json:"turns_per_task"`
	Usage          Usage   `json:"usage"`
	CacheReadShare float64 `json:"cache_read_share"`
	Cost           float64 `json:"cost_usd"`
}

// Comparison is one compressed level against off, paired by task and
// seed.
type Comparison struct {
	Level   int     `json:"level"`
	Verdict Verdict `json:"verdict"`
	// InputChange and CostChange are relative to off (-0.2: 20% less).
	InputChange float64 `json:"input_tokens_change"`
	CostChange  float64 `json:"cost_change"`
}

// bootstrapResamples for the paired differences.
const bootstrapResamples = 2000

// Summarise builds the report of a run's results, priced at p.
func Summarise(pl Plan, results []TaskResult, p Price, dryRun bool) LiveReport {
	rep := LiveReport{Suite: pl.Suite.Name, Metric: pl.Suite.Metric, Tasks: len(pl.Tasks), Seeds: len(pl.Seeds), DryRun: dryRun}
	byLevel := map[int][]TaskResult{}
	for _, r := range results {
		byLevel[r.Level] = append(byLevel[r.Level], r)
	}
	for _, l := range pl.Levels {
		rs := byLevel[l]
		c := ConfigSummary{Level: l, Runs: len(rs)}
		var scores []float64
		turns := 0
		malformed := 0
		for _, r := range rs {
			scores = append(scores, r.Score)
			c.EM += r.EM
			turns += r.Turns
			c.Usage = c.Usage.Add(r.Usage)
			if r.Error != "" {
				c.Errors++
			}
			if r.Malformed {
				malformed++
			}
		}
		c.Metric = meanInterval(scores)
		if n := float64(len(rs)); n > 0 {
			c.EM /= n
			c.MalformedRate = float64(malformed) / n
			c.TurnsPerTask = float64(turns) / n
		}
		c.CacheReadShare = c.Usage.CacheReadShare()
		c.Cost = p.Cost(c.Usage)
		rep.Configs = append(rep.Configs, c)
	}
	off := pairIndex(byLevel[0])
	var base ConfigSummary
	for _, c := range rep.Configs {
		if c.Level == 0 {
			base = c
		}
	}
	for _, c := range rep.Configs {
		if c.Level == 0 {
			continue
		}
		var treat, ctrl []float64
		for _, r := range byLevel[c.Level] {
			if o, ok := off[pairKey(r)]; ok {
				treat, ctrl = append(treat, r.Score), append(ctrl, o.Score)
			}
		}
		diff := PairedDiff(treat, ctrl, 0.95, bootstrapResamples, pl.Seed)
		rep.Comparisons = append(rep.Comparisons, Comparison{
			Level:       c.Level,
			Verdict:     NonInferior(pl.Suite.Metric, diff, pl.Suite.Margin, pl.Suite.MinTasks*max(pl.Suite.Seeds, 1)),
			InputChange: change(float64(c.Usage.PromptTotal()), float64(base.Usage.PromptTotal())),
			CostChange:  change(c.Cost, base.Cost),
		})
	}
	return rep
}

func pairKey(r TaskResult) string { return fmt.Sprintf("%s\x00%d", r.Task, r.Seed) }

func pairIndex(rs []TaskResult) map[string]TaskResult {
	m := make(map[string]TaskResult, len(rs))
	for _, r := range rs {
		m[pairKey(r)] = r
	}
	return m
}

func change(v, base float64) float64 {
	if base == 0 {
		return 0
	}
	return v/base - 1
}

// meanInterval is the mean with a normal-approximation 95% interval,
// clamped to the 0-100 scale.
func meanInterval(xs []float64) Interval {
	iv := Interval{Level: 0.95, N: len(xs)}
	if len(xs) == 0 {
		return iv
	}
	m := Mean(xs)
	ss := 0.0
	for _, x := range xs {
		ss += (x - m) * (x - m)
	}
	half := 0.0
	if len(xs) > 1 {
		half = zFor(0.95) * math.Sqrt(ss/float64(len(xs)-1)/float64(len(xs)))
	}
	iv.Estimate, iv.Low, iv.High = m, math.Max(0, m-half), math.Min(100, m+half)
	return iv
}

// JSON is the report as report.json holds it.
func (r *LiveReport) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Markdown is the report for people.
func (r *LiveReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Live suite: %s\n\n", r.Suite)
	if r.DryRun {
		b.WriteString("**Dry run against the oracle mock:** a task scores only if its evidence reached the upstream intact, so this measures what compression kept, not a model's quality.\n\n")
	}
	fmt.Fprintf(&b, "%d tasks × %d seeds. Metric: %s (0-100).\n\n", r.Tasks, r.Seeds, r.Metric)
	b.WriteString("| Level | Runs | Errors | Metric (95% CI) | Turns/task | Input tokens | Cache-read share | Cost |\n|---|---|---|---|---|---|---|---|\n")
	configs := append([]ConfigSummary(nil), r.Configs...)
	sort.Slice(configs, func(i, j int) bool { return configs[i].Level < configs[j].Level })
	for _, c := range configs {
		fmt.Fprintf(&b, "| %s | %d | %d | %.1f (%.1f–%.1f) | %.1f | %d | %s | $%.4f |\n", levelName(c.Level), c.Runs, c.Errors,
			c.Metric.Estimate, c.Metric.Low, c.Metric.High, c.TurnsPerTask, c.Usage.PromptTotal(), pct(c.CacheReadShare), c.Cost)
	}
	if len(r.Comparisons) > 0 {
		b.WriteString("\n| Level vs off | Difference (95% CI) | Margin | Verdict | Input tokens | Cost |\n|---|---|---|---|---|---|\n")
		for _, c := range r.Comparisons {
			v := c.Verdict
			verdict := "non-inferior"
			switch {
			case v.Inconclusive:
				verdict = fmt.Sprintf("inconclusive (%d pairs)", v.Diff.N)
			case !v.Pass:
				verdict = "fails"
			}
			fmt.Fprintf(&b, "| %s | %+.1f (%+.1f to %+.1f) | %.1f | %s | %+.1f%% | %+.1f%% |\n", levelName(c.Level),
				v.Diff.Estimate, v.Diff.Low, v.Diff.High, v.Margin, verdict, c.InputChange*100, c.CostChange*100)
		}
	}
	return b.String()
}

func levelName(l int) string {
	if l == 0 {
		return "off"
	}
	return fmt.Sprintf("level %d", l)
}
