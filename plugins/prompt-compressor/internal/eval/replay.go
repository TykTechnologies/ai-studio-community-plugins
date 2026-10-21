// Package eval is the Prompt Compressor eval harness (eac.23): an offline
// replay of the corpus (or of a customer's own captured request bodies) that
// reports estimated token reduction per tool kind and level and the
// projected cost including prompt-cache reads and writes, plus the paired
// statistics the live suites use for the non-inferiority gate (eac.4).
//
// The replay needs no vendor keys and no network, so it is also the CI smoke
// run. Live suites (BFCL, long-context QA, SWE-bench Verified through the
// Microgateway) cost money; see live.go for the planner and its cost guard.
package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
	"github.com/TykTechnologies/midsommar/v2/guardrails"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// Compressor is what the replay runs at each level. The real one wraps the
// compress package (eac.10); Identity stands in until then and is level 0.
type Compressor interface {
	Name() string
	// Compress returns the body the gateway would forward at level, or
	// the body unchanged. It must be a pure function of its inputs.
	Compress(format, path string, body []byte, level int) ([]byte, error)
}

// Identity forwards every body unchanged: compression off.
type Identity struct{}

func (Identity) Name() string { return "identity" }
func (Identity) Compress(_, _ string, body []byte, _ int) ([]byte, error) {
	return body, nil
}

// Tokenizer counts a text's tokens.
type Tokenizer interface {
	Name() string
	Count([]byte) int
}

// ByteEstimate is a tokenizer-free estimate (one token per four bytes,
// rounded up), the usual rule of thumb for English and code. Ratios between
// levels are what the replay reports, and the estimate is applied the same
// way to both sides.
type ByteEstimate struct{}

func (ByteEstimate) Name() string { return "bytes/4" }
func (ByteEstimate) Count(b []byte) int {
	return (len(b) + 3) / 4
}

// Options configure a replay.
type Options struct {
	Levels     []int
	Compressor Compressor
	Tokens     Tokenizer
	// TokensFor, when set, picks the tokenizer per request format (the
	// compress package's estimator) and overrides Tokens.
	TokensFor func(format string) Tokenizer
	Prices    Prices
	// OutputTokens is the response length billed per session turn (the
	// corpus records requests, not responses).
	OutputTokens int
	// ProtectRecentTurns is the policy's protect_recent_turns: results in
	// that many newest turns may compress further once they leave the
	// window without counting as a prefix break. Default 1, the default
	// policy's.
	ProtectRecentTurns int
}

func (o *Options) defaults() {
	if len(o.Levels) == 0 {
		o.Levels = []int{0}
	}
	if o.Compressor == nil {
		o.Compressor = Identity{}
	}
	if o.Tokens == nil {
		o.Tokens = ByteEstimate{}
	}
	if o.Prices == nil {
		o.Prices = DefaultPrices()
	}
	if o.OutputTokens == 0 {
		o.OutputTokens = 400
	}
	if o.ProtectRecentTurns == 0 {
		o.ProtectRecentTurns = 1
	}
}

func (o Options) tok(format string) Tokenizer {
	if o.TokensFor != nil {
		return o.TokensFor(format)
	}
	return o.Tokens
}

// KindStats are the tool-result tokens of one tool kind.
type KindStats struct {
	Units  int `json:"units"`
	Before int `json:"tokens_before"`
	After  int `json:"tokens_after"`
}

// Reduction is the share of tokens removed.
func (k KindStats) Reduction() float64 {
	if k.Before == 0 {
		return 0
	}
	return 1 - float64(k.After)/float64(k.Before)
}

// SessionResult is one session replayed at one level.
type SessionResult struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Turns  int    `json:"turns"`
	// History is the session's history mode (full, previous_response_id).
	History string `json:"history"`
	// TokensBefore and TokensAfter are the input tokens summed over turns,
	// uncompressed and compressed.
	TokensBefore int     `json:"tokens_before"`
	TokensAfter  int     `json:"tokens_after"`
	Usage        Usage   `json:"usage"`
	Cost         float64 `json:"cost"`
	// BaselineCost is the same session at compression off, billed the same
	// way, so CostDelta < 0 means compression saves money after cache.
	BaselineCost   float64 `json:"baseline_cost"`
	CostDelta      float64 `json:"cost_delta"`
	CacheReadShare float64 `json:"cache_read_share"`
	// PrefixBreaks counts turns whose compressed body does not extend the
	// previous turn's compressed conversation byte for byte, beyond what
	// the uncompressed bodies already differ in (a cache-stability bug).
	PrefixBreaks int `json:"prefix_breaks"`
	// LateCompressions counts tool results sent verbatim while recent and
	// compressed on a later turn (protect_recent_turns), each of which
	// costs the cache that one result once.
	LateCompressions int `json:"late_compressions"`
	// Skipped: the session's format has no extractor yet.
	Skipped string `json:"skipped,omitempty"`
}

// LevelResult is everything measured at one level.
type LevelResult struct {
	Level        int                  `json:"level"`
	Entries      int                  `json:"entries"`
	TokensBefore int                  `json:"tokens_before"`
	TokensAfter  int                  `json:"tokens_after"`
	ByKind       map[string]KindStats `json:"by_kind"`
	// Unattributed: tokens saved in entries whose unit layout changed, so
	// savings could not be assigned to a tool kind.
	Unattributed int `json:"unattributed_saved_tokens"`
	// SessionByKind is ByKind over the last turn of each full-history
	// session; SessionUnattributed as Unattributed.
	SessionByKind       map[string]KindStats `json:"session_by_kind"`
	SessionUnattributed int                  `json:"session_unattributed_saved_tokens"`
	// SkipViolations are pass-through bodies the compressor changed, and
	// native-context-management bodies compressed beyond level 1. Any is a
	// bug.
	SkipViolations []string `json:"skip_violations"`
	// Nondeterministic are bodies that compressed differently twice.
	Nondeterministic []string        `json:"nondeterministic"`
	Errors           []string        `json:"errors"`
	Sessions         []SessionResult `json:"sessions"`
	SessionCost      float64         `json:"session_cost"`
	BaselineCost     float64         `json:"baseline_session_cost"`
	SessionUsage     Usage           `json:"session_usage"`
}

// Report is a replay's result.
type Report struct {
	Compressor string        `json:"compressor"`
	Tokenizer  string        `json:"tokenizer"`
	Output     int           `json:"output_tokens_per_turn"`
	Prices     Prices        `json:"prices"`
	Levels     []LevelResult `json:"levels"`
}

// Replay runs the corpus through the compressor at each level.
func Replay(m corpus.Manifest, o Options) (*Report, error) {
	o.defaults()
	rep := &Report{Compressor: o.Compressor.Name(), Tokenizer: o.Tokens.Name(), Output: o.OutputTokens, Prices: o.Prices}
	if o.TokensFor != nil {
		rep.Tokenizer = "per format: " + o.TokensFor("anthropic").Name() + " (anthropic), " + o.TokensFor("openai").Name() + " (openai)"
	}
	baseline := map[string]float64{}
	for _, s := range m.Sessions {
		r := replaySession(s, Identity{}, 0, o, nil)
		baseline[s.Name] = r.Cost
	}
	for _, level := range o.Levels {
		lr := LevelResult{Level: level, ByKind: map[string]KindStats{}, SessionByKind: map[string]KindStats{}, SkipViolations: []string{}, Nondeterministic: []string{}, Errors: []string{}}
		for _, e := range m.Entries {
			replayEntry(e, level, o, &lr)
		}
		for _, s := range m.Sessions {
			r := replaySession(s, o.Compressor, level, o, &lr)
			r.BaselineCost = baseline[s.Name]
			r.CostDelta = r.Cost - r.BaselineCost
			lr.Sessions = append(lr.Sessions, r)
			lr.SessionCost += r.Cost
			lr.BaselineCost += r.BaselineCost
			lr.SessionUsage = lr.SessionUsage.Add(r.Usage)
		}
		rep.Levels = append(rep.Levels, lr)
	}
	return rep, nil
}

func replayEntry(e corpus.Entry, level int, o Options, lr *LevelResult) {
	body := e.Body()
	tk := o.tok(e.Format)
	out, err := o.Compressor.Compress(e.Format, e.Path, body, level)
	if err != nil {
		lr.Errors = append(lr.Errors, fmt.Sprintf("%s: %s", e.Name, ErrorClass(err)))
		return
	}
	if again, err := o.Compressor.Compress(e.Format, e.Path, body, level); err != nil || !bytes.Equal(again, out) {
		lr.Nondeterministic = append(lr.Nondeterministic, e.Name)
	}
	mustSkip := e.Expect == corpus.ExpectSkip || !e.Readable(segments.Supports)
	if mustSkip {
		if !bytes.Equal(out, body) {
			lr.SkipViolations = append(lr.SkipViolations, e.Name)
		}
		return
	}
	// Planted canaries sit in text the compressor must never rewrite
	// (forged reasoning and built-in tool calls).
	for _, c := range e.Canaries {
		if bytes.Contains(body, []byte(c)) && !bytes.Contains(out, []byte(c)) {
			lr.SkipViolations = append(lr.SkipViolations, e.Name+" (rewrote "+c+")")
		}
	}
	// A body whose context the vendor manages is capped at level 1
	// (lossless): above it, the output must be what level 1 gives.
	if e.NativeContextManagement && level > 1 {
		if l1, err := o.Compressor.Compress(e.Format, e.Path, body, 1); err == nil && !bytes.Equal(out, l1) {
			lr.SkipViolations = append(lr.SkipViolations, e.Name+" (native context management above level 1)")
		}
	}
	lr.Entries++
	before, after := tk.Count(body), tk.Count(out)
	lr.TokensBefore += before
	lr.TokensAfter += after

	ok, err := attributeKinds(lr.ByKind, e.Tools, e.Format, body, out, tk)
	if err != nil {
		lr.Errors = append(lr.Errors, fmt.Sprintf("%s: units: %s", e.Name, ErrorClass(err)))
	} else if !ok {
		lr.Unattributed += before - after
	}
}

// attributeKinds adds each tool result's tokens before and after
// compression to its kind; false when compression changed the unit layout,
// so results cannot be paired.
func attributeKinds(by map[string]KindStats, refs []corpus.ToolRef, format string, in, out []byte, tk Tokenizer) (bool, error) {
	ub, err1 := segments.ExtractUnits(format, in)
	ua, err2 := segments.ExtractUnits(format, out)
	if err1 != nil || err2 != nil {
		return false, errors.Join(err1, err2)
	}
	tb, ta := toolUnits(ub), toolUnits(ua)
	if len(tb) != len(ta) {
		return false, nil
	}
	for i, u := range tb {
		kind := kindOf(refs, u.Text)
		ks := by[kind]
		ks.Units++
		ks.Before += tk.Count([]byte(u.Text))
		ks.After += tk.Count([]byte(ta[i].Text))
		by[kind] = ks
	}
	return true, nil
}

func toolUnits(units []segments.Unit) []segments.Unit {
	var out []segments.Unit
	for _, u := range units {
		if u.Kind == guardrails.KindToolResult {
			out = append(out, u)
		}
	}
	return out
}

// kindOf names the tool kind of a tool result by the corpus markers, also
// inside a JSON string (Codex wraps shell output as {"output": "..."}); a
// result no marker matches (a patch confirmation) is "other".
func kindOf(refs []corpus.ToolRef, text string) string {
	for _, ref := range refs {
		if ref.Via != "tool_result" {
			continue
		}
		esc, _ := json.Marshal(ref.Marker)
		if strings.Contains(text, ref.Marker) || strings.Contains(text, string(esc[1:len(esc)-1])) {
			return string(ref.Kind)
		}
	}
	return "other"
}

func replaySession(s corpus.Session, c Compressor, level int, o Options, lr *LevelResult) SessionResult {
	r := SessionResult{Name: s.Name, Format: s.Format, Turns: s.Turns, History: s.History}
	if s.Pending != "" && !segments.Supports(s.Format) {
		r.Skipped = "format not supported yet (" + s.Pending + ")"
		return r
	}
	rule := CacheRules[s.Format]
	tk := o.tok(s.Format)
	price := o.Prices[s.Format]
	var prevIn, prevOut []byte
	var prevLeaves map[string]leaf
	for k, in := range s.Bodies() {
		out, err := c.Compress(s.Format, s.Path, in, level)
		if err != nil {
			if lr != nil {
				lr.Errors = append(lr.Errors, fmt.Sprintf("%s turn %d: %s", s.Name, k+1, ErrorClass(err)))
			}
			out = in
		}
		tokens := tk.Count(out)
		r.TokensBefore += tk.Count(in)
		r.TokensAfter += tokens
		shared := 0
		var leaves map[string]leaf
		if lr != nil { // the baseline pass only prices; it checks nothing
			leaves = leafMap(s.Format, in, out, o.ProtectRecentTurns)
		}
		if prevOut != nil {
			n := commonPrefix(prevOut, out)
			shared = tk.Count(out[:n])
			if prefixBroken(prevLeaves, leaves, tk.Count(in[:commonPrefix(prevIn, in)]), shared, tk.Count(in), tokens, &r.LateCompressions) {
				r.PrefixBreaks++
			}
		}
		prevLeaves = leaves
		explicit := bytes.Contains(out, []byte(`"cache_control"`)) || bytes.Contains(out, []byte(`"cachePoint"`))
		if s.History == "previous_response_id" {
			// The vendor holds the history; the request carries only new
			// items, so there is no client-side prefix to keep stable.
			shared = 0
		}
		r.Usage = r.Usage.Add(TurnUsage(rule, tokens, shared, explicit, o.OutputTokens))
		prevIn, prevOut = in, out
	}
	// The last turn of a full-history session holds every tool result,
	// all but the newest past the protected turns: what each kind's
	// reducer does to session-shaped output.
	if lr != nil && s.History == "full" && prevIn != nil {
		if ok, err := attributeKinds(lr.SessionByKind, s.Tools, s.Format, prevIn, prevOut, tk); err != nil || !ok {
			lr.SessionUnattributed += tk.Count(prevIn) - tk.Count(prevOut)
		}
	}
	r.Cost = price.Cost(r.Usage)
	r.CacheReadShare = r.Usage.CacheReadShare()
	return r
}

// ErrorClass names an error for a report without its message: compressor and
// segmenter errors can quote the body text they failed on, and a report of
// customer traffic must never carry that text. An error type can name its
// own class with an ErrorClass() string method.
func ErrorClass(err error) string {
	var c interface{ ErrorClass() string }
	switch {
	case err == nil:
		return "none"
	case errors.As(err, &c):
		return c.ErrorClass()
	case errors.Is(err, segments.ErrDuplicateKey):
		return "duplicate_key"
	case errors.Is(err, segments.ErrUnrecognisedBody):
		return "unrecognised_body"
	case errors.Is(err, segments.ErrUnsupportedFormat):
		return "unsupported_format"
	}
	return "compress_error"
}

// leaf is one tool-result leaf of a request: its text before and after
// compression, and whether it sat in the request's protected newest turns.
type leaf struct {
	in, out   string
	protected bool
}

// leafMap maps each tool-result leaf path of a request to a leaf, when
// compression kept the unit layout (nil otherwise). protect is the number of
// newest turns the policy keeps at level 1 at most (protect_recent_turns).
func leafMap(format string, in, out []byte, protect int) map[string]leaf {
	ui, err1 := segments.ExtractUnits(format, in)
	uo, err2 := ui, err1
	if !bytes.Equal(in, out) {
		uo, err2 = segments.ExtractUnits(format, out)
	}
	if err1 != nil || err2 != nil {
		return nil
	}
	newest := 0
	for _, u := range ui {
		newest = max(newest, u.Turn)
	}
	ti, to := toolUnits(ui), toolUnits(uo)
	if len(ti) != len(to) {
		return nil
	}
	m := map[string]leaf{}
	for i := range ti {
		if len(ti[i].Leaves) != len(to[i].Leaves) {
			return nil
		}
		for j, l := range ti[i].Leaves {
			m[l.Path] = leaf{in: l.Text, out: to[i].Leaves[j].Text, protected: ti[i].Turn > newest-protect}
		}
	}
	return m
}

// prefixBroken reports a turn that loses prompt-cache prefix the
// uncompressed history kept:
//   - a tool result already sent compressed in the previous turn, with the
//     same text, compressed differently this time (output is not a pure
//     function of the unit text: the R1 rule), or
//   - the compressed shared prefix is under half of what the uncompressed
//     shared prefix becomes at this turn's compression ratio (something
//     before the history changed, such as a stamp or a reordering).
//
// A result the previous turn sent in its protected newest turns (level 1 at
// most, by protect_recent_turns) or uncompressed, and this turn compresses
// further, is not a break: the design costs the cache that result once, as
// it leaves the protected window (counted in late).
func prefixBroken(prev, cur map[string]leaf, sharedIn, sharedOut, tokensIn, tokensOut int, late *int) bool {
	broken, lateNow := false, false
	for path, c := range cur {
		p, ok := prev[path]
		if !ok || p.in != c.in || p.out == c.out {
			continue
		}
		if p.in == p.out || (p.protected && !c.protected) {
			*late++
			lateNow = true
			continue
		}
		broken = true
	}
	if broken {
		return true
	}
	if lateNow {
		// The shared prefix ends at the late-compressed result; that loss
		// is the late compression's, not a break before the history.
		return false
	}
	if tokensIn == 0 {
		return false
	}
	expected := float64(sharedIn) * float64(tokensOut) / float64(tokensIn)
	return float64(sharedOut) < expected/2
}

func commonPrefix(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// Kinds returns the tool kinds of a level in a fixed order.
func (lr LevelResult) Kinds() []string { return sortedKinds(lr.ByKind) }

func sortedKinds(by map[string]KindStats) []string {
	var ks []string
	for k := range by {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
