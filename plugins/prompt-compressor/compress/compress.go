// Package compress shrinks the text of an LLM request body without
// touching anything else in it. It has no SDK dependency, so it can be
// tested, fuzzed and benchmarked on its own.
//
// Compress reads the body with guardrails/segments in its request format,
// picks the units the policy selects, rewrites their string leaves in place
// (segments.Apply), and checks the result before returning it. Tool call
// ids, images, signatures, thinking blocks and cache_control are never
// touched: they are not leaves.
//
// The core rule (R1) is that the output is a pure function of the unit text
// and the policy. Nothing depends on time, load or earlier requests, and
// limits are by size, so a compressed prefix is byte-identical turn after
// turn and the provider's prompt cache keeps hitting.
package compress

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/guardrails"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// Outcome says what Compress did with a body.
type Outcome string

const (
	// OutcomeCompressed: the body was rewritten.
	OutcomeCompressed Outcome = "compressed"
	// OutcomeUnchanged: the body was read, but nothing selected could be
	// made smaller.
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeSkipped: the body was not compressed (Report.Reason says why).
	OutcomeSkipped Outcome = "skipped"
	// OutcomeRejected: a rewrite failed its post-check, and the original
	// body is returned.
	OutcomeRejected Outcome = "rejected"
)

// Reasons a body is skipped or rejected.
const (
	ReasonOff               = "policy_off"
	ReasonTooLarge          = "body_too_large"
	ReasonUnsupportedFormat = "unsupported_format"
	ReasonUnreadable        = "unreadable_body"
	ReasonPostCheck         = "post_check_failed"
)

// DefaultMemoBytes bounds the memo of compressed leaves.
const DefaultMemoBytes = 64 << 20

// Input is one body to compress.
type Input struct {
	// Format is the request filter format (pkg/requestformat.For).
	Format string
	Body   []byte
	// Policy is the resolved policy; nil or level 0 skips the body.
	Policy *config.Policy
	// MaxBodyBytes skips larger bodies; 0 means config.DefaultMaxBodyBytes.
	MaxBodyBytes int
	// Model names the tokenizer for the token estimate; empty reads the
	// body's "model".
	Model string
}

// StrategyStats counts one strategy's work.
type StrategyStats struct {
	Leaves     int `json:"leaves"`
	BytesSaved int `json:"bytes_saved"`
}

// Report describes one Compress call.
type Report struct {
	Outcome    Outcome `json:"outcome"`
	Reason     string  `json:"reason,omitempty"`
	PolicyHash string  `json:"policy_hash,omitempty"`
	Level      int     `json:"level"`
	Format     string  `json:"format"`

	UnitsSeen       int `json:"units_seen"`
	UnitsSelected   int `json:"units_selected"`
	UnitsCompressed int `json:"units_compressed"`

	Strategies map[string]*StrategyStats `json:"strategies,omitempty"`

	BytesBefore int `json:"bytes_before"`
	BytesAfter  int `json:"bytes_after"`
	// TokensBefore and TokensAfter are estimates (Estimator) over the
	// leaves that changed; the rest of the body is the same on both sides.
	TokensBefore int    `json:"tokens_before"`
	TokensAfter  int    `json:"tokens_after"`
	Estimator    string `json:"estimator"`
	// Model is the model the request names, for pricing at the edge. It is
	// never serialised.
	Model string `json:"-"`

	// NativeContextManagement: the client asked the vendor to manage the
	// context, so the level was capped at 1.
	NativeContextManagement bool `json:"native_context_management,omitempty"`
	// ServerToolSkipped counts vendor-produced tool results left alone.
	ServerToolSkipped int `json:"server_tool_skipped,omitempty"`
	// CacheSkipped counts units left alone under cache_mode skip_cached.
	CacheSkipped int `json:"cache_skipped,omitempty"`
}

// TokensSaved is the estimated saving.
func (r *Report) TokensSaved() int { return r.TokensBefore - r.TokensAfter }

func (r *Report) count(strategy string, saved int) {
	if r.Strategies == nil {
		r.Strategies = map[string]*StrategyStats{}
	}
	s := r.Strategies[strategy]
	if s == nil {
		s = &StrategyStats{}
		r.Strategies[strategy] = s
	}
	s.Leaves++
	s.BytesSaved += saved
}

// Compressor compresses bodies. It is safe for concurrent use; its memo of
// compressed leaves changes how fast it is, never what it returns.
type Compressor struct {
	memo *memo
	// tokens memoises token counts by estimator and text: tokenizing is
	// most of the cost of a call, and history leaves recur every turn.
	tokens *memo
}

// Options configures a Compressor.
type Options struct {
	// MemoBytes bounds the memo; 0 is DefaultMemoBytes, negative disables it.
	MemoBytes int
}

// New returns a Compressor.
func New(opts Options) *Compressor {
	n := opts.MemoBytes
	if n == 0 {
		n = DefaultMemoBytes
	}
	return &Compressor{memo: newMemo(n), tokens: newMemo(n / 8)}
}

// countTokens is est.Count through the token-count memo.
func (c *Compressor) countTokens(est *Estimator, text string) int {
	key := newMemoKey("tokens/"+est.Name(), text)
	if v, ok := c.tokens.get(key); ok {
		return v.Tokens
	}
	n := est.Count(text)
	c.tokens.put(key, memoValue{Tokens: n})
	return n
}

// Compress returns the compressed body and a report. On any outcome but
// OutcomeCompressed the body returned is in.Body itself.
func (c *Compressor) Compress(in Input) ([]byte, Report) {
	rep := Report{Format: in.Format, BytesBefore: len(in.Body), BytesAfter: len(in.Body)}
	skip := func(reason string) ([]byte, Report) {
		rep.Outcome, rep.Reason = OutcomeSkipped, reason
		return in.Body, rep
	}
	p := in.Policy
	if p == nil || p.Level <= 0 {
		return skip(ReasonOff)
	}
	rep.PolicyHash, rep.Level = p.Hash(), p.Level
	limit := in.MaxBodyBytes
	if limit <= 0 {
		limit = config.DefaultMaxBodyBytes
	}
	if len(in.Body) > limit {
		return skip(ReasonTooLarge)
	}
	// Only a format segmenter's read is rewritten: segments.Generic walks
	// every string, encrypted payloads and signatures included.
	if !segments.Supports(in.Format) {
		return skip(ReasonUnsupportedFormat)
	}
	units, err := segments.ExtractUnits(in.Format, in.Body)
	if err != nil {
		return skip(ReasonUnreadable)
	}
	rep.UnitsSeen = len(units)

	sel := c.selectUnits(in, p, units, &rep)
	rep.Model = modelOf(in)
	est := EstimatorFor(in.Format, rep.Model)
	rep.Estimator = est.Name()

	edits := c.edit(p, rep.PolicyHash, sel, est, &rep)
	if len(edits) == 0 {
		rep.Outcome = OutcomeUnchanged
		return in.Body, rep
	}
	out, err := segments.Apply(in.Body, edits)
	if err == nil {
		err = postCheck(in.Format, in.Body, out, edits)
	}
	if err != nil {
		rep.Outcome, rep.Reason = OutcomeRejected, ReasonPostCheck
		rep.UnitsCompressed, rep.Strategies, rep.TokensBefore, rep.TokensAfter = 0, nil, 0, 0
		return in.Body, rep
	}
	rep.Outcome = OutcomeCompressed
	rep.BytesAfter = len(out)
	return out, rep
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// selected is a unit to compress at a level.
type selected struct {
	unit   segments.Unit
	level  int
	budget int // the tool's size budget, tokens; 0 for none
}

// selectUnits picks the units the policy compresses, with the level each
// gets.
func (c *Compressor) selectUnits(in Input, p *config.Policy, units []segments.Unit, rep *Report) []selected {
	maxLevel := p.Level
	if segments.NativeContextManagement(in.Format, in.Body) {
		rep.NativeContextManagement = true
		maxLevel = min(maxLevel, 1)
	}
	var cached *segments.Breakpoint
	if p.CacheMode == config.CacheSkipCached {
		if bps, err := segments.CacheBreakpoints(in.Format, in.Body); err == nil && len(bps) > 0 {
			cached = &bps[len(bps)-1]
		}
	}
	lastTurn := 0
	for _, u := range units {
		lastTurn = max(lastTurn, u.Turn)
	}

	var out []selected
	for _, u := range units {
		// A result the vendor produced is replayed to it as it was: the
		// vendor may check it against encrypted or signed parts beside it.
		if u.Undecodable || len(u.Leaves) == 0 || u.ServerTool {
			rep.ServerToolSkipped += btoi(u.ServerTool)
			continue
		}
		role := u.Role
		if u.Kind == guardrails.KindToolResult {
			role = config.RoleTool
		}
		if !p.HasRole(role) {
			continue
		}
		level, budget := maxLevel, 0
		if o := p.ToolOverride(u.ToolName); u.Kind == guardrails.KindToolResult && o != nil {
			if o.Exclude {
				continue
			}
			if o.Level != nil {
				level = min(*o.Level, maxLevel)
			}
			budget = o.MaxUnitTokens
		}
		if p.ProtectRecentTurns > 0 && u.Turn > lastTurn-p.ProtectRecentTurns {
			level = min(level, 1)
		}
		if level <= 0 || selectionTokens(u.Text) < p.MinUnitTokens {
			continue
		}
		if cached != nil && u.Cached(*cached) {
			rep.CacheSkipped++
			continue
		}
		out = append(out, selected{unit: u, level: level, budget: budget})
	}
	rep.UnitsSelected = len(out)
	return out
}

// edit returns the leaf edits for the selected units.
func (c *Compressor) edit(p *config.Policy, policyHash string, sel []selected, est *Estimator, rep *Report) []segments.Edit {
	var edits []segments.Edit
	seen := map[string]segments.Unit{} // tool output text -> first unit with it
	keep := protectFunc(p)
	for _, s := range sel {
		u := s.unit
		if p.DedupeReferences && u.Kind == guardrails.KindToolResult && len(u.Leaves) == 1 {
			if first, ok := seen[u.Text]; ok {
				ref := dedupeReference(first.ToolName, first.ToolCallID)
				old := u.Leaves[0].Text
				if len(ref) < len(old) {
					edits = append(edits, segments.Edit{Path: u.Leaves[0].Path, Old: old, New: ref})
					rep.count(StrategyDedupe, len(old)-len(ref))
					rep.TokensBefore += c.countTokens(est, old)
					rep.TokensAfter += c.countTokens(est, ref)
					rep.UnitsCompressed++
					continue
				}
			} else {
				seen[u.Text] = u
			}
		}
		changed := false
		for _, leaf := range u.Leaves {
			v := c.compressLeaf(policyHash, pass{level: s.level, keep: keep, budget: s.budget}, leaf.Text)
			if v.Same {
				continue
			}
			edits = append(edits, segments.Edit{Path: leaf.Path, Old: leaf.Text, New: v.Text})
			for _, st := range v.Steps {
				rep.count(st.Strategy, st.Saved)
			}
			rep.TokensBefore += c.countTokens(est, leaf.Text)
			rep.TokensAfter += c.countTokens(est, v.Text)
			changed = true
		}
		if changed {
			rep.UnitsCompressed++
		}
	}
	return edits
}

// protectFunc reports lines a policy's protect patterns match; nil when it
// has none.
func protectFunc(p *config.Policy) func(string) bool {
	res := p.ProtectRegexps()
	if len(res) == 0 {
		return nil
	}
	return func(line string) bool {
		for _, re := range res {
			if re.MatchString(line) {
				return true
			}
		}
		return false
	}
}

// compressLeaf runs a pass on one leaf, through the memo. The key holds the
// effective level and the tool's budget: the same text compresses
// differently at level 1 (a protected recent turn) and level 2, and under
// two tools' budgets.
// A result that is not shorter, empty or not valid UTF-8 leaves it as is.
func (c *Compressor) compressLeaf(policyHash string, ps pass, text string) memoValue {
	key := newMemoKey(policyHash+"/L"+strconv.Itoa(ps.level)+"/B"+strconv.Itoa(ps.budget), text)
	if v, ok := c.memo.get(key); ok {
		return v
	}
	out, steps := ps.run(text, 0)
	v := memoValue{Same: true}
	if len(steps) > 0 && len(out) < len(text) && validText(out) {
		v = memoValue{Text: out, Steps: steps}
	}
	c.memo.put(key, v)
	return v
}

var errPostCheck = errors.New("compress: post-check failed")

// postCheck re-reads the rewritten body: it must be no larger, read in its
// format, keep its tool calls paired if they were, and hold each edited
// leaf exactly as written.
func postCheck(format string, before, after []byte, edits []segments.Edit) error {
	if len(after) > len(before) {
		return errPostCheck
	}
	units, err := segments.ExtractUnits(format, after)
	if err != nil {
		return errPostCheck
	}
	leaves := map[string]string{}
	for _, u := range units {
		for _, l := range u.Leaves {
			leaves[l.Path] = l.Text
		}
	}
	for _, e := range edits {
		if got, ok := leaves[e.Path]; !ok || got != e.New {
			return errPostCheck
		}
	}
	// The input is read only when the output fails: a body that did not
	// pair before may not pair after.
	if segments.CheckToolPairing(format, after) != nil && segments.CheckToolPairing(format, before) == nil {
		return errPostCheck
	}
	return nil
}

// modelPattern finds a "model" member. It only picks the tokenizer for the
// estimate, so a scan of the start of the body (where clients put it) is
// enough; decoding a 4 MB body for it cost more than compressing it.
var modelPattern = regexp.MustCompile(`"model"\s*:\s*"([^"\\]{1,200})"`)

const modelScanBytes = 64 << 10

// modelOf is the model the request names: in.Model, or a "model" member
// near the start of the body.
func modelOf(in Input) string {
	if in.Model != "" {
		return in.Model
	}
	head := in.Body
	if len(head) > modelScanBytes {
		head = head[:modelScanBytes]
	}
	if m := modelPattern.FindSubmatch(head); m != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}
