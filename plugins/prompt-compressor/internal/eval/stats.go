package eval

import (
	"math"
	"math/rand/v2"
	"sort"
)

// Interval is an estimate with a two-sided confidence interval.
type Interval struct {
	Estimate float64 `json:"estimate"`
	Low      float64 `json:"low"`
	High     float64 `json:"high"`
	// Level is the confidence level, e.g. 0.95.
	Level float64 `json:"level"`
	N     int     `json:"n"`
}

// Mean returns the mean of xs (0 for none).
func Mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// PairedDiff returns the mean of treatment[i]-control[i] with a percentile
// bootstrap CI over the pairs (resampling pairs, not arms, so the pairing
// the gate asks for is kept). The resampling is seeded: the same inputs and
// seed give the same interval. treatment and control must be the same
// length: index i is one task under both configs.
func PairedDiff(treatment, control []float64, level float64, resamples int, seed uint64) Interval {
	if len(treatment) != len(control) {
		panic("eval.PairedDiff: arms differ in length")
	}
	n := len(treatment)
	d := make([]float64, n)
	for i := range d {
		d[i] = treatment[i] - control[i]
	}
	iv := Interval{Estimate: Mean(d), Level: level, N: n}
	if n < 2 {
		iv.Low, iv.High = iv.Estimate, iv.Estimate
		return iv
	}
	if resamples <= 0 {
		resamples = 10000
	}
	r := rand.New(rand.NewPCG(seed, seed^0xa5a5a5a5))
	means := make([]float64, resamples)
	for b := range means {
		s := 0.0
		for i := 0; i < n; i++ {
			s += d[r.IntN(n)]
		}
		means[b] = s / float64(n)
	}
	sort.Float64s(means)
	alpha := (1 - level) / 2
	iv.Low = quantile(means, alpha)
	iv.High = quantile(means, 1-alpha)
	return iv
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	f := pos - float64(lo)
	return sorted[lo]*(1-f) + sorted[hi]*f
}

// Wilson returns the Wilson score interval for k successes in n trials
// (a resolve rate, a call accuracy).
func Wilson(k, n int, level float64) Interval {
	iv := Interval{Level: level, N: n}
	if n == 0 {
		return iv
	}
	z := zFor(level)
	p := float64(k) / float64(n)
	nf := float64(n)
	den := 1 + z*z/nf
	centre := (p + z*z/(2*nf)) / den
	half := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf)) / den
	iv.Estimate, iv.Low, iv.High = p, centre-half, centre+half
	return iv
}

// zFor is the two-sided normal quantile for a confidence level.
func zFor(level float64) float64 {
	switch level {
	case 0.90:
		return 1.6448536269514722
	case 0.95:
		return 1.959963984540054
	case 0.99:
		return 2.5758293035489004
	}
	// Acklam-free fallback: bisection on the normal CDF.
	target := 1 - (1-level)/2
	lo, hi := 0.0, 10.0
	for i := 0; i < 100; i++ {
		mid := (lo + hi) / 2
		if 0.5*math.Erfc(-mid/math.Sqrt2) < target {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// Verdict is a non-inferiority decision for one metric.
type Verdict struct {
	Metric string `json:"metric"`
	// Diff is treatment minus control, in the metric's units, oriented so
	// that higher is better (a cost or a turn count is negated first).
	Diff Interval `json:"diff"`
	// Margin is the largest loss allowed (positive, in the metric's units).
	Margin float64 `json:"margin"`
	// Pass: the whole CI lies above -Margin.
	Pass bool `json:"pass"`
	// Inconclusive: too few pairs to decide.
	Inconclusive bool `json:"inconclusive,omitempty"`
}

// NonInferior decides treatment is no worse than control by more than margin:
// the lower CI bound of the higher-is-better difference must exceed -margin.
// Fewer than minPairs pairs give an inconclusive (failing) verdict.
func NonInferior(metric string, diff Interval, margin float64, minPairs int) Verdict {
	v := Verdict{Metric: metric, Diff: diff, Margin: margin}
	if diff.N < minPairs {
		v.Inconclusive = true
		return v
	}
	v.Pass = diff.Low > -margin
	return v
}
