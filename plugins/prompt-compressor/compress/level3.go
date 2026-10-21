package compress

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/guardrails"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// Level 3: cuts that keep a tool output's shape but not its content. A
// text over its size budget (max_unit_tokens) keeps its head and tail; a
// JSON document keeps the first and last items of its arrays, then, if it
// is still over, only its field names and value types; tool results older
// than mask_after_turns become a placeholder. Every cut is a pure function
// of the body, so a request's earlier turns compress to the same bytes on
// every later turn.

// Level-3 strategy names, as reported.
const (
	StrategyHeadTail  = "head_tail"
	StrategyJSONShape = "json_shape"
	StrategyMask      = "mask_old_turn"
)

// level3Version is fingerprinted with the reducers (registryFingerprint).
const level3Version = 2

// headTailMarkerRoom is the bytes kept free of the budget for markers.
const headTailMarkerRoom = 160

// headTailName names the cut in its marker.
const headTailName = "head and tail"

// headTail cuts text over budget tokens to its first and last lines: a
// third of the budget for the head, the rest for the tail, where errors and
// totals usually are. Lines a protect pattern matches stay. A text with no
// line short enough is cut by bytes, at rune boundaries.
func headTail(text string, budget int, keep func(string) bool) (string, bool) {
	if budget <= 0 || selectionTokens(text) <= budget {
		return text, false
	}
	room := max(budget*4-headTailMarkerRoom, budget*2)
	headRoom := room / 3
	lines := strings.Split(text, "\n")
	h, used := 0, 0
	for h < len(lines) && used+len(lines[h])+1 <= headRoom {
		used += len(lines[h]) + 1
		h++
	}
	t := len(lines)
	for t > h && used+len(lines[t-1])+1 <= room {
		used += len(lines[t-1]) + 1
		t--
	}
	if t > h && (h > 0 || t < len(lines)) {
		drop := make([]bool, len(lines))
		for i := h; i < t; i++ {
			drop[i] = true
		}
		r := reducer{name: headTailName, plan: func([]string) cutPlan { return cutPlan{drop: drop, failures: -1} }}
		if out, ok := r.apply(text, lines, keep); ok {
			return out, true
		}
		return text, false
	}
	// No whole line fits: cut bytes.
	head := runePrefix(text, headRoom)
	tail := runeSuffix(text, room-len(head))
	cut := len(text) - len(head) - len(tail)
	if cut <= 0 {
		return text, false
	}
	mark := fmt.Sprintf("[tyk-compressed: %s output, %s → %s tokens; %s bytes cut]", headTailName, thousands(selectionTokens(text)), thousands(selectionTokens(head+tail)), thousands(cut))
	out := head + "\n" + mark + "\n" + tail
	if len(out) >= len(text) {
		return text, false
	}
	return out, true
}

// runePrefix is at most n bytes of s, ending on a rune boundary.
func runePrefix(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// runeSuffix is at most n bytes from the end of s, starting on a rune
// boundary.
func runeSuffix(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}

// jsonShapeKey is the member a shaped object starts with.
const jsonShapeKey = "tyk-compressed"

// jsonShape replaces doc, a compact JSON document, with its field names
// and value types: strings become "string", numbers "number", booleans
// "boolean"; an array keeps its first item's shape and the count of the
// rest. It stays JSON: an object's first member, or an array's first item,
// is the marker.
func jsonShape(doc string) (string, bool) {
	if doc == "" || (doc[0] != '{' && doc[0] != '[') {
		return doc, false
	}
	var b strings.Builder
	shapeValue(&b, doc, 0)
	shape := b.String()
	mark := encodeJSONString(fmt.Sprintf("[tyk-compressed: json shape, %s → %s tokens]", thousands(selectionTokens(doc)), thousands(selectionTokens(shape))))
	var out string
	switch {
	case shape == "{}" || shape == "[]":
		return doc, false
	case shape[0] == '{':
		out = `{"` + jsonShapeKey + `":` + mark + "," + shape[1:]
	default:
		out = "[" + mark + "," + shape[1:]
	}
	if len(out) >= len(doc) {
		return doc, false
	}
	return out, true
}

func shapeValue(b *strings.Builder, v string, depth int) {
	if v == "" {
		b.WriteString("null")
		return
	}
	if depth > maxJSONReduceDepth && (v[0] == '{' || v[0] == '[') {
		b.WriteString(`"json"`)
		return
	}
	switch v[0] {
	case '{':
		b.WriteByte('{')
		for i, m := range jsonMembers(v) {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(encodeJSONString(m.key))
			b.WriteByte(':')
			shapeValue(b, m.val, depth+1)
		}
		b.WriteByte('}')
	case '[':
		items, _ := jsonItems(v, 0)
		b.WriteByte('[')
		if len(items) > 0 {
			shapeValue(b, v[items[0].start:items[0].end], depth+1)
			if len(items) > 1 {
				b.WriteByte(',')
				b.WriteString(encodeJSONString(itemsMarker(len(items) - 1)))
			}
		}
		b.WriteByte(']')
	case '"':
		b.WriteString(`"string"`)
	case 't', 'f':
		b.WriteString(`"boolean"`)
	case 'n':
		b.WriteString("null")
	default:
		b.WriteString(`"number"`)
	}
}

// maskBoundary returns the turn before which tool results are masked: 0
// for none. Tool results become eligible mask_after_turns turns after
// their own, but the boundary moves only when the results it would newly
// mask hold at least mask_step_tokens, so each move (a one-off cache
// rewrite for the vendor) buys a large saving. It depends only on the
// body's earlier turns, which a conversation never changes, so it never
// moves back.
func maskBoundary(units []segments.Unit, lastTurn int, p *config.Policy) int {
	if p.MaskAfterTurns <= 0 {
		return 0
	}
	eligible := lastTurn - p.MaskAfterTurns + 1 // mask turns before this
	if eligible <= 0 {
		return 0
	}
	// maskable: tokens of the tool results masking would replace (single
	// leaf, not vendor-produced); all: every unit's tokens, per turn
	maskable := make([]int, eligible)
	all := make([]int, lastTurn+1)
	for _, u := range units {
		if u.Turn < 0 || u.Turn > lastTurn {
			continue
		}
		n := selectionTokens(u.Text)
		all[u.Turn] += n
		if u.Kind == guardrails.KindToolResult && !u.ServerTool && len(u.Leaves) == 1 && u.Turn < eligible {
			maskable[u.Turn] += n
		}
	}
	step := max(p.MaskStepTokens, 1)
	boundary, sinceBoundary := 0, 0
	for t := 0; t < eligible; t++ {
		sinceBoundary += maskable[t]
		if sinceBoundary < step {
			continue
		}
		// Moving the boundary past turn t first happens when turn
		// t+mask_after_turns is the newest: the vendor then re-caches every
		// turn after t up to it. The move must mask more than that, times
		// maskRecacheRatio, or the rewrite costs more than the masking saves.
		// Only turns up to t+mask_after_turns count, so the decision never
		// changes as the conversation grows.
		recache := 0
		for k := t + 1; k <= t+p.MaskAfterTurns && k <= lastTurn; k++ {
			recache += all[k]
		}
		if float64(sinceBoundary) >= maskRecacheRatio*float64(recache) {
			boundary, sinceBoundary = t+1, 0
		}
	}
	return boundary
}

// maskRecacheRatio is how many times the tokens a mask step re-caches it
// must mask. Replaying the corpus sessions, 2 cost least of 0, 1 and 2
// (eac.69); even so masking costs more than it saves on Anthropic and
// GPT-5 sessions, so mask_after_turns defaults to 0.
const maskRecacheRatio = 2.0

// maskPlaceholder is what a masked tool result becomes:
//
//	[tyk-compressed: shell output, 12,345 tokens, masked after 8 turns]
func maskPlaceholder(tool string, tokens, afterTurns int) string {
	if tool == "" {
		tool = "tool"
	}
	tool = runePrefix(tool, 64)
	return fmt.Sprintf("[tyk-compressed: %s output, %s tokens, masked after %d turns]", tool, thousands(tokens), afterTurns)
}
