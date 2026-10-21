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
	// OutcomeEngineFailed: the external engine failed under failure_mode
	// closed; the original body is returned and the caller refuses the
	// request.
	OutcomeEngineFailed Outcome = "engine_failed"
)

// Reasons a body is skipped or rejected.
const (
	ReasonOff               = "policy_off"
	ReasonTooLarge          = "body_too_large"
	ReasonUnsupportedFormat = "unsupported_format"
	ReasonUnreadable        = "unreadable_body"
	ReasonPostCheck         = "post_check_failed"
	ReasonEngineFailed      = "engine_failed"
)

// DefaultMemoBytes bounds the memo of compressed leaves.
const DefaultMemoBytes = 64 << 20

// Input is one body to compress.
type Input struct {
	// Format is the request filter format (pkg/requestformat.For).
	Format string
	Body   []byte
	// Doc is Body already parsed (segments.Parse of these same bytes, in
	// any format), or nil. The compressor reads it As Format instead of
	// parsing the body again.
	Doc *segments.Doc
	// Policy is the resolved policy; nil or level 0 skips the body.
	Policy *config.Policy
	// MaxBodyBytes skips larger bodies; 0 means config.DefaultMaxBodyBytes.
	MaxBodyBytes int
	// Model names the tokenizer for the token estimate; empty reads the
	// body's "model".
	Model string
	// Engine, when set, sends selected units to the policy's external
	// engine after levels 1–3 (engine.Client.Run). Units in protected turns,
	// system prompts, tool definitions, masked and deduplicated units are
	// never sent, nor is anything when CapLossy or native context
	// management applies.
	Engine EngineFunc
	// CapLossy caps the level at 2: the LLM has request filters, which
	// would screen the cut body instead of the client's, and the config
	// does not allow lossy levels with them (lossy_with_request_filters).
	CapLossy bool
	// Trace fills Report.Units with every unit's decisions (trace.go), for
	// the Studio preview. It never changes the body or the rest of the
	// Report; the request path leaves it off.
	Trace bool
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
	// RequestFiltersCapped: Input.CapLossy lowered the level to 2.
	RequestFiltersCapped bool `json:"request_filters_capped,omitempty"`
	// EngineUnits counts leaves sent to the engine (or found in its memo);
	// EngineFailed those it failed on, forwarded at levels 1–3.
	EngineUnits  int `json:"engine_units,omitempty"`
	EngineFailed int `json:"engine_failed,omitempty"`

	// Units is every unit's decisions, in body order, when Input.Trace.
	Units []UnitTrace `json:"units,omitempty"`
	// ToolDecisions is what drop_tools did with each tool definition, when
	// Input.Trace.
	ToolDecisions []ToolDecision `json:"tool_decisions,omitempty"`
	// DroppedTools names the tool definitions drop_tools removed, when the
	// rewrite went through.
	DroppedTools []string `json:"dropped_tools,omitempty"`
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

// countTokens is est.Count, through the token-count memo when it runs the
// tokenizer.
func (c *Compressor) countTokens(est *Estimator, text string) int {
	if est.enc == nil {
		return est.Count(text)
	}
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
	// The body is parsed once: selection, the rewrite and the post-check
	// read the same tree (eac.80).
	doc, err := parseInput(in)
	if err != nil {
		return skip(ReasonUnreadable)
	}
	units, err := doc.Units(segments.Options{})
	if err != nil {
		return skip(ReasonUnreadable)
	}
	rep.UnitsSeen = len(units)

	sel := c.selectUnits(in, doc, p, units, &rep)
	rep.Model = modelOf(in)
	est := EstimatorFor(in.Format, rep.Model)
	rep.Estimator = est.Name()
	var sample []string
	for _, s := range sel {
		for _, l := range s.unit.Leaves {
			sample = append(sample, l.Text)
		}
	}
	est = est.calibrated(func(t string) int { return c.countTokens(est, t) }, sample)

	edits, fixed := c.edit(p, rep.PolicyHash, sel, est, &rep)
	if in.Engine != nil && p.Engine != nil {
		var err error
		if edits, err = c.runEngine(in, p, sel, edits, fixed, est, &rep); err != nil {
			rep.Outcome, rep.Reason = OutcomeEngineFailed, ReasonEngineFailed
			rep.UnitsCompressed, rep.Strategies, rep.TokensBefore, rep.TokensAfter = 0, nil, 0, 0
			return in.Body, rep
		}
	}
	if len(p.DropTools) > 0 {
		edits = c.applyDropTools(in, p, edits, est, &rep)
	}
	if len(edits) == 0 {
		rep.Outcome = OutcomeUnchanged
		return in.Body, rep
	}
	out, err := doc.Apply(edits)
	if err == nil {
		err = postCheck(doc, out, edits)
	}
	if err != nil {
		rep.Outcome, rep.Reason = OutcomeRejected, ReasonPostCheck
		rep.UnitsCompressed, rep.Strategies, rep.TokensBefore, rep.TokensAfter = 0, nil, 0, 0
		rep.DroppedTools = nil
		return in.Body, rep
	}
	rep.Outcome = OutcomeCompressed
	rep.BytesAfter = len(out.Body())
	return out.Body(), rep
}

// parseInput is in.Doc read as in.Format when it holds in.Body, else
// in.Body parsed.
func parseInput(in Input) (*segments.Doc, error) {
	if d := in.Doc; d != nil && len(d.Body()) == len(in.Body) && len(in.Body) > 0 && &d.Body()[0] == &in.Body[0] {
		return d.As(in.Format)
	}
	return segments.Parse(in.Format, in.Body)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// editableServerTools are the vendor tool results a policy may compress
// (compress_server_tool_results): those a live check (eac.56, 2026-10-21)
// found the vendor accepts edited when replayed in history and the model
// reads as edited, with the last line of the output swapped for a marker
// the model then quoted. Anthropic: claude-haiku-4-5 and claude-sonnet-5;
// OpenAI Responses: gpt-4.1-mini and gpt-5.5 with encrypted reasoning
// replayed. Kinds not checked (MCP connector results, the older code
// execution and text editor results, Google's code execution) stay as sent.
var editableServerTools = map[string]bool{
	"web_search_tool_result":          true,
	"web_fetch_tool_result":           true,
	"bash_code_execution_tool_result": true,
	"file_search_call":                true,
	"code_interpreter_call":           true,
}

// selected is a unit to compress at a level.
type selected struct {
	unit   segments.Unit
	level  int
	budget int  // the size budget, tokens; 0 for none
	mask   bool // level 3: an old tool result, replaced by a placeholder
	engine bool // may go to the external engine
	trace  int  // the unit's entry in Report.Units, when tracing
}

// selectUnits picks the units the policy compresses, with the level each
// gets.
func (c *Compressor) selectUnits(in Input, doc *segments.Doc, p *config.Policy, units []segments.Unit, rep *Report) []selected {
	maxLevel := p.Level
	if in.CapLossy && maxLevel > 2 {
		rep.RequestFiltersCapped = true
		maxLevel = 2
	}
	if doc.NativeContextManagement() {
		rep.NativeContextManagement = true
		maxLevel = min(maxLevel, 1)
	}
	var cached *segments.Breakpoint
	if p.CacheMode == config.CacheSkipCached {
		if bps, err := doc.CacheBreakpoints(); err == nil && len(bps) > 0 {
			cached = &bps[len(bps)-1]
		}
	}
	lastTurn := 0
	for _, u := range units {
		lastTurn = max(lastTurn, u.Turn)
	}
	maskBefore := 0
	if maxLevel >= 3 {
		maskBefore = maskBoundary(units, lastTurn, p)
	}

	var out []selected
	for i, u := range units {
		role := u.Role
		if u.Kind == guardrails.KindToolResult {
			role = config.RoleTool
		}
		var tr *UnitTrace
		if in.Trace {
			rep.Units = append(rep.Units, UnitTrace{Index: i, Role: role, Kind: u.Kind, ToolName: u.ToolName, Turn: u.Turn, Tokens: selectionTokens(u.Text)})
			tr = &rep.Units[len(rep.Units)-1]
		}
		skip := func(reason string) {
			if tr != nil {
				tr.Reason = reason
			}
		}
		// A result the vendor produced is replayed to it as it was: the
		// vendor may check it against encrypted or signed parts beside it.
		switch {
		case u.ServerTool && !(p.CompressServerToolResults && editableServerTools[u.ServerToolType]):
			rep.ServerToolSkipped++
			skip(TraceServerTool)
			continue
		case u.Undecodable:
			skip(TraceUndecodable)
			continue
		case len(u.Leaves) == 0:
			skip(TraceNoText)
			continue
		}
		if !p.HasRole(role) {
			skip(TraceRole)
			continue
		}
		level, budget := maxLevel, 0
		var capped []string
		if rep.RequestFiltersCapped {
			capped = append(capped, TraceCapRequestFilters)
		}
		if rep.NativeContextManagement && p.Level > 1 {
			capped = append(capped, TraceCapNativeContext)
		}
		if o := p.ToolOverride(u.ToolName); u.Kind == guardrails.KindToolResult && o != nil {
			if o.Exclude {
				skip(TraceExcludedTool)
				continue
			}
			if o.Level != nil && *o.Level < level {
				level = *o.Level
				capped = append(capped, TraceCapToolOverride)
			}
			budget = o.MaxUnitTokens
		}
		protected := p.ProtectRecentTurns > 0 && u.Turn > lastTurn-p.ProtectRecentTurns
		if protected && level > 1 {
			level = 1
			capped = append(capped, TraceCapProtectedTurn)
		}
		if level >= 3 && budget == 0 {
			budget = p.MaxUnitTokens
		}
		mask := level >= 3 && u.Kind == guardrails.KindToolResult && u.Turn < maskBefore
		if tr != nil {
			tr.Level, tr.Capped, tr.Budget, tr.Masked = level, capped, budget, mask
		}
		if level <= 0 {
			skip(TraceLevel0)
			continue
		}
		if selectionTokens(u.Text) < p.MinUnitTokens {
			skip(TraceBelowMinTokens)
			continue
		}
		if cached != nil && u.Cached(*cached) {
			rep.CacheSkipped++
			skip(TraceCached)
			continue
		}
		if tr != nil {
			tr.Selected = true
		}
		toEngine := !mask && !protected && !rep.RequestFiltersCapped && !rep.NativeContextManagement &&
			role != "system" && role != "developer" && u.Kind != guardrails.KindToolDefinition
		out = append(out, selected{unit: u, level: level, budget: budget, mask: mask, engine: toEngine, trace: len(rep.Units) - 1})
	}
	rep.UnitsSelected = len(out)
	return out
}

// edit returns the leaf edits for the selected units, and the paths of the
// leaves masked or replaced by a reference (which the engine never gets).
func (c *Compressor) edit(p *config.Policy, policyHash string, sel []selected, est *Estimator, rep *Report) ([]segments.Edit, map[string]bool) {
	var edits []segments.Edit
	fixed := map[string]bool{}
	seen := map[string]segments.Unit{} // tool output text -> first unit with it
	keep := protectFunc(p)
	for _, s := range sel {
		u := s.unit
		var tr *UnitTrace
		if rep.Units != nil {
			tr = &rep.Units[s.trace]
		}
		if s.mask {
			if e, ok := c.mask(p, u, est, rep, tr); ok {
				edits = append(edits, e...)
				fixed[u.Leaves[0].Path] = true
				continue
			}
		}
		if p.DedupeReferences && u.Kind == guardrails.KindToolResult && len(u.Leaves) == 1 {
			if first, ok := seen[u.Text]; ok {
				ref := dedupeReference(first.ToolName, first.ToolCallID)
				old := u.Leaves[0].Text
				if len(ref) < len(old) {
					edits = append(edits, segments.Edit{Path: u.Leaves[0].Path, Old: old, New: ref})
					fixed[u.Leaves[0].Path] = true
					rep.count(StrategyDedupe, len(old)-len(ref))
					rep.TokensBefore += c.countTokens(est, old)
					rep.TokensAfter += c.countTokens(est, ref)
					rep.UnitsCompressed++
					if tr != nil {
						tr.Changed, tr.Dedupe, tr.Strategies = true, true, []string{StrategyDedupe}
						tr.Leaves = []LeafTrace{{Path: u.Leaves[0].Path, Before: old, After: ref}}
						tr.BytesBefore, tr.BytesAfter = len(old), len(ref)
						tr.TokensBefore, tr.TokensAfter = c.countTokens(est, old), c.countTokens(est, ref)
					}
					continue
				}
			} else {
				seen[u.Text] = u
			}
		}
		changed := false
		for _, leaf := range u.Leaves {
			v := c.compressLeaf(policyHash, pass{level: s.level, keep: keep, budget: s.budget}, leaf.Text)
			if tr != nil {
				tr.addLeaf(c, est, leaf, v)
			}
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
	return edits, fixed
}

// runEngine sends the leaves of the units that may go to the engine, as
// levels 1–3 left them, and takes each shorter answer. Protect-pattern
// matches go with each job, and an answer that drops one is not taken
// (engine.Client checks). A non-nil error is a failure under failure_mode
// closed.
func (c *Compressor) runEngine(in Input, p *config.Policy, sel []selected, edits []segments.Edit, fixed map[string]bool, est *Estimator, rep *Report) ([]segments.Edit, error) {
	byPath := make(map[string]int, len(edits))
	for i, e := range edits {
		byPath[e.Path] = i
	}
	type target struct {
		leaf segments.Segment
		edit int // index in edits, or -1
		unit int // index in sel
	}
	var jobs []EngineJob
	var targets []target
	res := p.ProtectRegexps()
	for si, s := range sel {
		if !s.engine {
			continue
		}
		for _, leaf := range s.unit.Leaves {
			if fixed[leaf.Path] {
				continue
			}
			t := target{leaf: leaf, edit: -1, unit: si}
			text := leaf.Text
			if i, ok := byPath[leaf.Path]; ok {
				t.edit, text = i, edits[i].New
			}
			if text == "" {
				continue
			}
			var protect []string
			for _, re := range res {
				protect = append(protect, re.FindAllString(text, -1)...)
			}
			jobs = append(jobs, EngineJob{Role: s.unit.Role, Text: text, Protect: protect})
			targets = append(targets, t)
		}
	}
	if len(jobs) == 0 {
		return edits, nil
	}
	results, err := in.Engine(rep.Model, jobs)
	if err != nil {
		return nil, err
	}
	if len(results) != len(jobs) {
		return nil, errEngineResults
	}
	strategy := "engine_" + p.Engine.Kind
	changedUnit := map[int]bool{}
	for _, t := range targets {
		if t.edit >= 0 {
			changedUnit[t.unit] = true
		}
	}
	rep.EngineUnits += len(jobs)
	for i, r := range results {
		j, t := jobs[i], targets[i]
		if r.Failed {
			rep.EngineFailed++
		}
		if !r.Changed || len(r.Text) >= len(j.Text) || !validText(r.Text) {
			continue
		}
		rep.count(strategy, len(j.Text)-len(r.Text))
		if t.edit >= 0 {
			rep.TokensAfter += c.countTokens(est, r.Text) - c.countTokens(est, j.Text)
			edits[t.edit].New = r.Text
			continue
		}
		edits = append(edits, segments.Edit{Path: t.leaf.Path, Old: t.leaf.Text, New: r.Text})
		rep.TokensBefore += c.countTokens(est, t.leaf.Text)
		rep.TokensAfter += c.countTokens(est, r.Text)
		if !changedUnit[t.unit] {
			changedUnit[t.unit] = true
			rep.UnitsCompressed++
		}
	}
	return edits, nil
}

var errEngineResults = errors.New("compress: engine returned the wrong number of results")

// applyDropTools adds the edits that remove the policy's drop_tools. A
// leaf edit inside a tools array being replaced is left out: the array's
// kept definitions go as they were sent.
func (c *Compressor) applyDropTools(in Input, p *config.Policy, edits []segments.Edit, est *Estimator, rep *Report) []segments.Edit {
	drops, removed, decisions := dropTools(in.Format, in.Body, p.DropTools)
	if in.Trace {
		rep.ToolDecisions = decisions
	}
	if len(drops) == 0 {
		return edits
	}
	kept := edits[:0:0]
	for _, e := range edits {
		inside := false
		for _, d := range drops {
			if strings.HasPrefix(e.Path, d.Path+"/") {
				inside = true
			}
		}
		if !inside {
			kept = append(kept, e)
		}
	}
	for _, raw := range removed {
		rep.count(StrategyDropTools, len(raw))
		rep.TokensBefore += c.countTokens(est, string(raw))
	}
	for _, d := range decisions {
		if d.Dropped {
			rep.DroppedTools = append(rep.DroppedTools, d.Name)
		}
	}
	return append(kept, drops...)
}

// mask replaces an old tool result with a placeholder. Only a result held
// in one leaf is masked: a structured result (a JSON tool result read leaf
// by leaf) cannot lose its fields through leaf edits, so it keeps its
// level-3 cuts instead. It does nothing when the placeholder would not be
// shorter.
func (c *Compressor) mask(p *config.Policy, u segments.Unit, est *Estimator, rep *Report, tr *UnitTrace) ([]segments.Edit, bool) {
	if len(u.Leaves) != 1 {
		return nil, false
	}
	l := u.Leaves[0]
	ph := maskPlaceholder(u.ToolName, selectionTokens(u.Text), p.MaskAfterTurns)
	if len(ph) >= len(l.Text) {
		return nil, false
	}
	v := memoValue{Text: ph, Steps: []step{{StrategyMask, len(l.Text) - len(ph)}}}
	rep.TokensBefore += c.countTokens(est, l.Text)
	rep.TokensAfter += c.countTokens(est, ph)
	if tr != nil {
		tr.addLeaf(c, est, l, v)
	}
	rep.count(StrategyMask, len(l.Text)-len(ph))
	rep.UnitsCompressed++
	return []segments.Edit{{Path: l.Path, Old: l.Text, New: ph}}, true
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
// leaf exactly as written. after is the rewrite as Doc.Apply parsed it.
func postCheck(before, after *segments.Doc, edits []segments.Edit) error {
	if len(after.Body()) > len(before.Body()) {
		return errPostCheck
	}
	units, err := after.Units(segments.Options{})
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
		if e.Value != nil {
			continue // a tools array, rebuilt from its own items
		}
		if got, ok := leaves[e.Path]; !ok || got != e.New {
			return errPostCheck
		}
	}
	// The input is read only when the output fails: a body that did not
	// pair before may not pair after.
	if after.CheckToolPairing() != nil && before.CheckToolPairing() == nil {
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
