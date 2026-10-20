package compress

import (
	"strings"

	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// UnitTrace is what Compress decided for one unit, when Input.Trace is set
// (the Studio preview). It holds the unit's text, so it never goes into a
// record or a log.
type UnitTrace struct {
	// Index is the unit's position among the body's units.
	Index int `json:"index"`
	// Role is the effective role: tool for a tool result.
	Role     string `json:"role"`
	Kind     string `json:"kind"`
	ToolName string `json:"tool_name,omitempty"`
	Turn     int    `json:"turn"`
	// Tokens is the size selection compares with min_unit_tokens.
	Tokens int `json:"tokens"`

	Selected bool `json:"selected"`
	// Reason says why an unselected unit was left alone (Trace*).
	Reason string `json:"reason,omitempty"`
	// Level is the effective level; Capped says what lowered it.
	Level  int      `json:"level"`
	Capped []string `json:"capped,omitempty"`
	// Budget is the tool's size budget (max_unit_tokens), when it has one.
	Budget int `json:"budget,omitempty"`

	Changed bool `json:"changed"`
	// Dedupe: the unit became a reference to an earlier identical output.
	Dedupe bool `json:"dedupe,omitempty"`
	// Strategies are the strategies that changed a leaf, in the order they
	// first ran; a reducer appears as its reduce_* name.
	Strategies []string `json:"strategies,omitempty"`
	// Leaves are the changed leaves.
	Leaves []LeafTrace `json:"leaves,omitempty"`

	// Bytes and tokens over all the unit's leaves, before and after.
	BytesBefore  int `json:"bytes_before"`
	BytesAfter   int `json:"bytes_after"`
	TokensBefore int `json:"tokens_before"`
	TokensAfter  int `json:"tokens_after"`
}

// LeafTrace is one changed leaf.
type LeafTrace struct {
	Path   string `json:"path"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Why a unit was not selected.
const (
	TraceServerTool     = "server_tool"
	TraceUndecodable    = "undecodable"
	TraceNoText         = "no_text"
	TraceRole           = "role"
	TraceExcludedTool   = "excluded_tool"
	TraceLevel0         = "level_0"
	TraceBelowMinTokens = "below_min_tokens"
	TraceCached         = "cached"
)

// What lowered a unit's level.
const (
	TraceCapNativeContext = "native_context_management"
	TraceCapToolOverride  = "tool_override"
	TraceCapProtectedTurn = "protected_turn"
)

// addLeaf adds one of the unit's leaves, and its result, to the trace.
func (t *UnitTrace) addLeaf(c *Compressor, est *Estimator, leaf segments.Segment, v memoValue) {
	after := leaf.Text
	if !v.Same {
		after = v.Text
		t.Changed = true
		t.Leaves = append(t.Leaves, LeafTrace{Path: leaf.Path, Before: leaf.Text, After: v.Text})
		for _, st := range v.Steps {
			if !containsString(t.Strategies, st.Strategy) {
				t.Strategies = append(t.Strategies, st.Strategy)
			}
		}
	}
	t.BytesBefore += len(leaf.Text)
	t.BytesAfter += len(after)
	t.TokensBefore += c.countTokens(est, leaf.Text)
	t.TokensAfter += c.countTokens(est, after)
}

func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Detectors names the level-2 reducers that recognise text, in the order
// they are tried, so the first is the one level 2 would run: "json" for a
// JSON document (its arrays are reduced, never its lines), else every line
// reducer whose detector matches. Detectors are yes/no; there is no score.
func Detectors(text string) []string {
	if _, ok := compactJSON(text); ok {
		return []string{jsonReducerName}
	}
	text = collapseWhitespace(collapseCRFrames(stripANSI(text)))
	lines := strings.Split(text, "\n")
	if len(lines) < minReducerLines {
		return nil
	}
	var out []string
	for _, r := range registry {
		if r.detect(lines) {
			out = append(out, r.name)
		}
	}
	return out
}
