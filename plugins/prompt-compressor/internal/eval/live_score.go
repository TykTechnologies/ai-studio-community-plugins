package eval

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
)

// Scores are per task, in metric points (0-100), so a suite's margin
// (points of call accuracy, F1 or resolve rate) applies to the paired
// differences as they are.

// score is one task's result under one configuration.
type score struct {
	// Value is the suite metric: call accuracy, F1 or resolved, 0-100.
	Value float64
	// EM is the exact-match score of a QA task (0 or 100).
	EM float64
	// Malformed: a call whose arguments are not JSON, or that names a tool
	// the request does not offer (BFCL).
	Malformed bool
}

// scoreCalls is BFCL's: 100 when the answer makes exactly the expected
// calls, in order, with the same arguments once canonicalised.
func scoreCalls(t Task, r reply) score {
	var s score
	offered := offeredToolNames(t.Format, t.Request)
	for _, c := range r.Calls {
		if !json.Valid([]byte(c.Arguments)) || (len(offered) > 0 && !offered[c.Name]) {
			s.Malformed = true
		}
	}
	if s.Malformed || len(r.Calls) != len(t.Expect.Calls) {
		return s
	}
	for i, want := range t.Expect.Calls {
		got := r.Calls[i]
		if got.Name != want.Name || !sameJSON([]byte(got.Arguments), want.Arguments) {
			return s
		}
	}
	s.Value = 100
	return s
}

// sameJSON compares two JSON values after canonicalising them (key order,
// whitespace, number spelling).
func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	ca, _ := json.Marshal(x)
	cb, _ := json.Marshal(y)
	return bytes.Equal(ca, cb)
}

// offeredToolNames are the tools a request offers, by name.
func offeredToolNames(format string, req []byte) map[string]bool {
	var b struct {
		Tools []struct {
			Name     string `json:"name"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if json.Unmarshal(req, &b) != nil {
		return nil
	}
	out := map[string]bool{}
	for _, t := range b.Tools {
		if format == FormatOpenAI && t.Function.Name != "" {
			out[t.Function.Name] = true
		} else if t.Name != "" {
			out[t.Name] = true
		}
	}
	return out
}

// scoreAnswer is QA's: the best F1 and exact match against any acceptable
// answer, with SQuAD normalisation (lower case, no punctuation or
// articles, single spaces).
func scoreAnswer(t Task, r reply) score {
	var s score
	got := normalizeAnswer(r.Text)
	for _, a := range t.Expect.Answers {
		want := normalizeAnswer(a)
		if got == want {
			s.EM = 100
		}
		s.Value = max(s.Value, 100*f1(strings.Fields(got), strings.Fields(want)))
	}
	return s
}

func normalizeAnswer(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) {
			return -1
		}
		return r
	}, s)
	var words []string
	for _, w := range strings.Fields(s) {
		if w != "a" && w != "an" && w != "the" {
			words = append(words, w)
		}
	}
	return strings.Join(words, " ")
}

// f1 is the token-overlap F1 of two answers.
func f1(got, want []string) float64 {
	if len(got) == 0 || len(want) == 0 {
		if len(got) == len(want) {
			return 1
		}
		return 0
	}
	counts := map[string]int{}
	for _, w := range want {
		counts[w]++
	}
	common := 0
	for _, w := range got {
		if counts[w] > 0 {
			counts[w]--
			common++
		}
	}
	if common == 0 {
		return 0
	}
	p, rc := float64(common)/float64(len(got)), float64(common)/float64(len(want))
	return 2 * p * rc / (p + rc)
}

// scoreResolved is the agent suite's: 100 when the final answer contains
// the fix.
func scoreResolved(t Task, r reply) score {
	if t.Expect.ResolvedIf != "" && strings.Contains(r.Text, t.Expect.ResolvedIf) {
		return score{Value: 100}
	}
	return score{}
}
