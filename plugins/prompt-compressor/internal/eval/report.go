package eval

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSON renders the report.
func (r *Report) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Markdown renders the report as tables: per level, the tool-kind
// reductions, then the sessions with projected cost including cache.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Prompt Compressor offline replay\n\n")
	fmt.Fprintf(&b, "Compressor `%s`, tokens by `%s`, %d output tokens billed per session turn. Costs use the gateway's ChatRecordCost formula at the prices listed in the JSON report; they are projections, not invoices.\n\n", r.Compressor, r.Tokenizer, r.Output)
	b.WriteString("## Summary\n\n| Level | Bodies | Input tokens before | after | Reduction | Session cost | at off | Change | Cache-read share | Skip violations | Nondeterministic | Errors |\n|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, l := range r.Levels {
		fmt.Fprintf(&b, "| %d | %d | %d | %d | %s | $%.4f | $%.4f | %s | %s | %d | %d | %d |\n",
			l.Level, l.Entries, l.TokensBefore, l.TokensAfter, pct(reduction(l.TokensBefore, l.TokensAfter)),
			l.SessionCost, l.BaselineCost, signedPct(l.SessionCost, l.BaselineCost), pct(l.SessionUsage.CacheReadShare()),
			len(l.SkipViolations), len(l.Nondeterministic), len(l.Errors))
	}
	for _, l := range r.Levels {
		fmt.Fprintf(&b, "\n## Level %d\n\n### Tool output by kind\n\n| Kind | Units | Tokens before | after | Reduction |\n|---|---|---|---|---|\n", l.Level)
		for _, k := range l.Kinds() {
			ks := l.ByKind[k]
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %s |\n", k, ks.Units, ks.Before, ks.After, pct(ks.Reduction()))
		}
		if l.Unattributed != 0 {
			fmt.Fprintf(&b, "\n%d saved tokens could not be attributed to a kind (the unit layout changed).\n", l.Unattributed)
		}
		if len(l.SessionByKind) > 0 {
			b.WriteString("\n### Tool output by kind, sessions (last turn of each full-history session)\n\n| Kind | Units | Tokens before | after | Reduction |\n|---|---|---|---|---|\n")
			for _, k := range sortedKinds(l.SessionByKind) {
				ks := l.SessionByKind[k]
				fmt.Fprintf(&b, "| %s | %d | %d | %d | %s |\n", k, ks.Units, ks.Before, ks.After, pct(ks.Reduction()))
			}
			if l.SessionUnattributed != 0 {
				fmt.Fprintf(&b, "\n%d saved tokens could not be attributed to a kind (the unit layout changed).\n", l.SessionUnattributed)
			}
		}
		b.WriteString("\n### Sessions (30 turns each)\n\n| Session | Format | Input tokens before | after | Cache read | Cache write | Uncached | Cache-read share | Cost | at off | Change | Prefix breaks | Late compressions |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, s := range l.Sessions {
			if s.Skipped != "" {
				fmt.Fprintf(&b, "| %s | %s | skipped: %s ||||||||||\n", s.Name, s.Format, s.Skipped)
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %d | %s | $%.4f | $%.4f | %s | %d | %d |\n",
				s.Name, s.Format, s.TokensBefore, s.TokensAfter, s.Usage.CacheRead, s.Usage.CacheWrite, s.Usage.Input,
				pct(s.CacheReadShare), s.Cost, s.BaselineCost, signedPct(s.Cost, s.BaselineCost), s.PrefixBreaks, s.LateCompressions)
		}
		for _, list := range []struct {
			title string
			items []string
		}{{"Skip violations (bodies that must pass through untouched)", l.SkipViolations}, {"Nondeterministic", l.Nondeterministic}, {"Errors", l.Errors}} {
			if len(list.items) > 0 {
				fmt.Fprintf(&b, "\n**%s:** %s\n", list.title, strings.Join(list.items, ", "))
			}
		}
	}
	b.WriteString("\nSessions chained with previous_response_id send only new items, so their client-side input (and this projection) leaves out the history the vendor bills from its store.\n")
	return b.String()
}

func reduction(before, after int) float64 {
	if before == 0 {
		return 0
	}
	return 1 - float64(after)/float64(before)
}

func pct(f float64) string { return fmt.Sprintf("%.1f%%", f*100) }

func signedPct(v, base float64) string {
	if base == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%+.1f%%", (v/base-1)*100)
}
