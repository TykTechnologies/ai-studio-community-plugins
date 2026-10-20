package compress

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Level-1 strategies. Each is lossless for what a model reads: the text a
// terminal or a JSON parser would show is unchanged. Each is a pure function
// of its input, so the same tool output compresses to the same bytes every
// turn and the provider's prompt cache keeps hitting.

// Strategy names, as reported.
const (
	StrategyJSONCompact = "json_compact"
	StrategyANSI        = "ansi_strip"
	StrategyCRFrames    = "cr_frames"
	StrategyWhitespace  = "whitespace"
	StrategyDedupe      = "dedupe_reference"
)

// step is one strategy that changed a leaf, and the bytes it saved.
type step struct {
	Strategy string
	Saved    int
}

// maxJSONStringDepth bounds how many times a JSON document inside a string
// inside a JSON document is unwrapped (tool outputs are often JSON-encoded
// twice: Codex wraps shell output as {"output": "..."}).
const maxJSONStringDepth = 3

// lossless applies the level-1 text strategies to one leaf and reports which
// changed it. A JSON document is compacted, and its string values get the
// text strategies; any other text has ANSI codes, carriage-return frames and
// redundant whitespace removed. Saved bytes are counted on the decoded text.
func lossless(text string) (string, []step) {
	return losslessDepth(text, 0)
}

func losslessDepth(text string, depth int) (string, []step) {
	if out, ok := compactJSON(text); ok {
		var steps []step
		if out != text {
			steps = append(steps, step{StrategyJSONCompact, len(text) - len(out)})
		}
		if depth < maxJSONStringDepth {
			var inner []step
			out, inner = rewriteJSONStrings(out, depth+1)
			steps = append(steps, inner...)
		}
		return out, steps
	}
	var steps []step
	for _, s := range []struct {
		name string
		fn   func(string) string
	}{
		{StrategyANSI, stripANSI},
		{StrategyCRFrames, collapseCRFrames},
		{StrategyWhitespace, collapseWhitespace},
	} {
		if out := s.fn(text); out != text {
			steps = append(steps, step{s.name, len(text) - len(out)})
			text = out
		}
	}
	return text, steps
}

// compactJSON removes insignificant whitespace from text when all of it is
// one JSON object or array. It works on the bytes (json.Compact), so key
// order, duplicate keys, number spelling and string escapes are kept.
func compactJSON(text string) (string, bool) {
	t := strings.TrimSpace(text)
	if t == "" || (t[0] != '{' && t[0] != '[') || !json.Valid([]byte(t)) {
		return "", false
	}
	var buf bytes.Buffer
	buf.Grow(len(t))
	if err := json.Compact(&buf, []byte(t)); err != nil {
		return "", false
	}
	return buf.String(), true
}

// rewriteJSONStrings applies losslessDepth to each string value (not key) of
// a compact JSON document. A value is replaced only when its new encoding is
// shorter; others keep their bytes, escapes and all.
func rewriteJSONStrings(doc string, depth int) (string, []step) {
	var b strings.Builder
	var steps []step
	last := 0
	for i := 0; i < len(doc); {
		if doc[i] != '"' {
			i++
			continue
		}
		j := i + 1
		for j < len(doc) && doc[j] != '"' {
			if doc[j] == '\\' {
				j++
			}
			j++
		}
		end := j + 1
		if end > len(doc) {
			break
		}
		if end < len(doc) && doc[end] == ':' {
			i = end // a key
			continue
		}
		var s string
		if json.Unmarshal([]byte(doc[i:end]), &s) == nil {
			if out, st := losslessDepth(s, depth); len(st) > 0 && out != s && validText(out) {
				if enc := encodeJSONString(out); len(enc) < end-i {
					if b.Len() == 0 {
						b.Grow(len(doc))
					}
					b.WriteString(doc[last:i])
					b.WriteString(enc)
					last = end
					steps = append(steps, st...)
				}
			}
		}
		i = end
	}
	if last == 0 {
		return doc, nil
	}
	b.WriteString(doc[last:])
	return b.String(), steps
}

// encodeJSONString encodes s as a JSON string without HTML escaping, as
// segments writes leaves.
func encodeJSONString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

// stripANSI removes terminal escape sequences: CSI (ESC [ ... final byte),
// OSC (ESC ] ... BEL or ESC \), and two-byte escapes. A lone ESC at the end
// is kept.
func stripANSI(s string) string {
	if strings.IndexByte(s, 0x1b) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != 0x1b || i+1 >= len(s) {
			b.WriteByte(c)
			i++
			continue
		}
		switch s[i+1] {
		case '[':
			j := i + 2
			// Parameter and intermediate bytes, then one final byte.
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
				j++
			}
			if j < len(s) && s[j] >= 0x40 && s[j] <= 0x7e {
				i = j + 1
				continue
			}
			// Not a complete sequence: keep it as text.
			b.WriteByte(c)
			i++
		case ']':
			j := i + 2
			end := -1
			for ; j < len(s); j++ {
				if s[j] == 0x07 {
					end = j + 1
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					end = j + 2
					break
				}
			}
			if end < 0 {
				b.WriteByte(c)
				i++
				continue
			}
			i = end
		default:
			// ESC, any intermediate bytes, then a final byte: ESC c, ESC M,
			// ESC ( B and the like.
			j := i + 1
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
				j++
			}
			if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7e {
				i = j + 1
				continue
			}
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// collapseCRFrames renders carriage-return progress frames the way a
// terminal shows them: each frame after a bare \r overwrites the line from
// its start, so "50%\r100%" reads "100%" and "abcdef\rXY" reads "XYcdef". A
// \r before \n is a line ending and is dropped.
func collapseCRFrames(s string) string {
	if strings.IndexByte(s, '\r') < 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.IndexByte(line, '\r') >= 0 {
			line = overlayFrames(strings.Split(line, "\r"))
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// overlayFrames writes each frame over the previous ones from column 0.
func overlayFrames(frames []string) string {
	var screen []rune
	for _, f := range frames {
		for i, r := range []rune(f) {
			if i < len(screen) {
				screen[i] = r
			} else {
				screen = append(screen, r)
			}
		}
	}
	return string(screen)
}

// collapseWhitespace drops trailing spaces and tabs on each line and
// collapses runs of blank lines to one. Leading indentation is kept.
func collapseWhitespace(s string) string {
	if !strings.ContainsAny(s, " \t\n") {
		return s
	}
	lines := strings.Split(s, "\n")
	out := lines[:0]
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// dedupeReference is the text that replaces a tool output identical to an
// earlier one in the same request.
func dedupeReference(tool, callID string) string {
	switch {
	case tool != "" && callID != "":
		return "[identical to the earlier output of " + tool + " (call " + callID + ")]"
	case callID != "":
		return "[identical to the earlier output of call " + callID + "]"
	case tool != "":
		return "[identical to an earlier output of " + tool + "]"
	}
	return "[identical to an earlier tool output]"
}

// validText reports whether s can be a leaf: valid UTF-8, not empty (an
// empty text block is refused by some vendors).
func validText(s string) bool {
	return s != "" && utf8.ValidString(s)
}
