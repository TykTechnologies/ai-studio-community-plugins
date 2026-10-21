package compress

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// Dropping tool definitions (eac.40): a policy's drop_tools removes named
// tool definitions from the request, by replacing each tools array with
// one made of the kept definitions' own bytes. A tool the history already
// calls, or tool_choice names, is always kept, and a tools array is never
// emptied (vendors refuse an empty one, and some a tool call without its
// definition). Everything is decided from the body, so the same request
// always keeps the same tools.

// StrategyDropTools is the strategy name of dropped tool definitions.
const StrategyDropTools = "drop_tools"

// Why a tool was kept or dropped (ToolDecision.Reason).
const (
	ToolDropped    = "dropped"
	ToolNotListed  = "not_listed"
	ToolInHistory  = "in_history"
	ToolInChoice   = "tool_choice"
	ToolWouldEmpty = "would_empty"
)

// ToolDecision is what happened to one tool definition, for the preview.
type ToolDecision struct {
	Name    string `json:"name"`
	Dropped bool   `json:"dropped"`
	Reason  string `json:"reason"`
}

// toolsArray is one array of tool definitions in a body: its JSON Pointer
// and its items with their names.
type toolsArray struct {
	path  string
	raw   []byte // the array as sent
	items []json.RawMessage
	names []string
	// marker: the item is a cache marker, not a tool (a Bedrock cachePoint)
	marker []bool
	// breakpoint is an Anthropic tool's cache_control value, which moves
	// to a kept tool when its tool is dropped.
	breakpoint []json.RawMessage
}

// dropTools returns the edits that remove the matching tools, the bytes of
// each dropped definition and, for the trace, every tool's decision.
func dropTools(format string, body []byte, patterns []string) ([]segments.Edit, []json.RawMessage, []ToolDecision) {
	if len(patterns) == 0 {
		return nil, nil, nil
	}
	arrays := toolArrays(format, body)
	if len(arrays) == 0 {
		return nil, nil, nil
	}
	used, choice := usedTools(format, body)
	var edits []segments.Edit
	var dropped []json.RawMessage
	var decisions []ToolDecision
	for _, a := range arrays {
		keep := make([]bool, len(a.items))
		reason := make([]string, len(a.items))
		n := 0
		for i, name := range a.names {
			switch {
			case name == "" || !matchesAny(name, patterns):
				keep[i], reason[i] = true, ToolNotListed
			case used[name]:
				keep[i], reason[i] = true, ToolInHistory
			case choice[name]:
				keep[i], reason[i] = true, ToolInChoice
			default:
				reason[i] = ToolDropped
				n++
			}
		}
		if n > 0 && !a.stillValid(keep) {
			for i := range keep {
				keep[i], reason[i] = true, ToolWouldEmpty
			}
			n = 0
		}
		for i, name := range a.names {
			decisions = append(decisions, ToolDecision{Name: name, Dropped: !keep[i], Reason: reason[i]})
		}
		if n == 0 {
			continue
		}
		for i, item := range a.items {
			if !keep[i] {
				dropped = append(dropped, item)
			}
		}
		if v, ok := a.without(keep); ok {
			edits = append(edits, segments.Edit{Path: a.path, Value: v})
		}
	}
	return edits, dropped, decisions
}

// stillValid reports whether the array keep would leave is one vendors
// take: it holds a real tool (a cache marker alone is refused, Bedrock
// answers 400 to tools=[cachePoint], to tools=[] and to a missing
// toolConfig beside tool blocks in the history), and every kept cache
// marker has a kept tool before it to cache.
func (a toolsArray) stillValid(keep []bool) bool {
	tools := 0
	for i := range a.items {
		if !keep[i] {
			continue
		}
		if a.marker[i] {
			if tools == 0 {
				return false
			}
			continue
		}
		tools++
	}
	return tools > 0
}

// without is the array as sent with the items keep marks false cut out:
// the kept items, the text before the first and after the last, and the
// separator that followed each kept item are its own bytes. A dropped
// tool's Anthropic cache_control moves to the nearest kept tool before it
// (else after it), so the tools and system prompt stay cached up to the
// same point; a kept tool that has one keeps its own.
func (a toolsArray) without(keep []bool) ([]byte, bool) {
	items := append([]json.RawMessage(nil), a.items...)
	moved := make([]bool, len(items))
	for i := range a.items {
		if keep[i] || a.breakpoint[i] == nil {
			continue
		}
		j := -1
		for k := i - 1; k >= 0 && j < 0; k-- {
			if keep[k] && !a.marker[k] {
				j = k
			}
		}
		for k := i + 1; k < len(items) && j < 0; k++ {
			if keep[k] && !a.marker[k] {
				j = k
			}
		}
		if j < 0 || a.breakpoint[j] != nil || moved[j] {
			continue
		}
		if with, ok := withField(items[j], "cache_control", a.breakpoint[i]); ok {
			items[j], moved[j] = with, true
		}
	}
	starts := make([]int, len(a.items))
	pos := 0
	for i, item := range a.items {
		at := bytes.Index(a.raw[pos:], item)
		if at < 0 {
			return nil, false
		}
		starts[i] = pos + at
		pos = starts[i] + len(item)
	}
	end := func(i int) int { return starts[i] + len(a.items[i]) }
	last := len(a.items) - 1
	var b bytes.Buffer
	b.Write(a.raw[:starts[0]])
	prev := -1
	for i := range a.items {
		if !keep[i] {
			continue
		}
		if prev >= 0 {
			// the separator that followed the previous kept item
			b.Write(a.raw[end(prev):starts[prev+1]])
		}
		b.Write(items[i])
		prev = i
	}
	b.Write(a.raw[end(last):])
	return b.Bytes(), true
}

// withField is the JSON object obj with key: value added as its last
// member; the object's own bytes are kept.
func withField(obj json.RawMessage, key string, value json.RawMessage) (json.RawMessage, bool) {
	t := bytes.TrimRight(obj, " \t\r\n")
	if len(t) < 2 || t[len(t)-1] != '}' {
		return nil, false
	}
	body := bytes.TrimRight(t[:len(t)-1], " \t\r\n")
	var b bytes.Buffer
	b.Write(body)
	if body[len(body)-1] != '{' {
		b.WriteByte(',')
	}
	k, _ := json.Marshal(key)
	b.Write(k)
	b.WriteByte(':')
	b.Write(value)
	b.Write(t[len(body):])
	b.Write(obj[len(t):])
	return b.Bytes(), json.Valid(b.Bytes())
}

// matchesAny reports a name a drop_tools entry names: the whole name, or
// a prefix ending in '*'.
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		} else if name == p {
			return true
		}
	}
	return false
}

// toolArrays finds the body's arrays of tool definitions.
func toolArrays(format string, body []byte) []toolsArray {
	named := func(path string, raw []byte, name func(json.RawMessage) string) (toolsArray, bool) {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || len(items) == 0 {
			return toolsArray{}, false
		}
		a := toolsArray{path: path, raw: raw, items: items, names: make([]string, len(items)),
			marker: make([]bool, len(items)), breakpoint: make([]json.RawMessage, len(items))}
		for i, it := range items {
			a.names[i] = name(it)
			var m map[string]json.RawMessage
			if json.Unmarshal(it, &m) != nil {
				continue
			}
			if a.names[i] == "" {
				_, a.marker[i] = m["cachePoint"]
			}
			if format == "anthropic" {
				a.breakpoint[i] = m["cache_control"]
			}
		}
		return a, true
	}
	field := func(raw json.RawMessage, keys ...string) string {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return ""
		}
		for i, k := range keys {
			v, ok := m[k]
			if !ok {
				return ""
			}
			if i < len(keys)-1 {
				if json.Unmarshal(v, &m) != nil {
					return ""
				}
				continue
			}
			var s string
			if json.Unmarshal(v, &s) == nil {
				return s
			}
		}
		return ""
	}
	switch format {
	case "google_ai":
		var b struct {
			Tools []map[string]json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(body, &b) != nil {
			return nil
		}
		var out []toolsArray
		for i, t := range b.Tools {
			for _, key := range []string{"functionDeclarations", "function_declarations"} {
				if a, ok := named("/tools/"+strconv.Itoa(i)+"/"+key, t[key], func(r json.RawMessage) string { return field(r, "name") }); ok {
					out = append(out, a)
				}
			}
		}
		return out
	case "bedrock":
		var b struct {
			ToolConfig struct {
				Tools json.RawMessage `json:"tools"`
			} `json:"toolConfig"`
		}
		if json.Unmarshal(body, &b) != nil {
			return nil
		}
		// A cachePoint has no name, so it is never dropped
		if a, ok := named("/toolConfig/tools", b.ToolConfig.Tools, func(r json.RawMessage) string { return field(r, "toolSpec", "name") }); ok {
			return []toolsArray{a}
		}
		return nil
	default:
		var b struct {
			Tools json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(body, &b) != nil {
			return nil
		}
		if a, ok := named("/tools", b.Tools, func(r json.RawMessage) string {
			if n := field(r, "function", "name"); n != "" {
				return n // OpenAI chat, Ollama
			}
			return field(r, "name") // Anthropic, Responses; a built-in has none
		}); ok {
			return []toolsArray{a}
		}
		return nil
	}
}

// usedTools returns the tools the history calls and the ones tool_choice
// names, in the body's format.
func usedTools(format string, body []byte) (used, choice map[string]bool) {
	used, choice = map[string]bool{}, map[string]bool{}
	var b struct {
		Messages []struct {
			ToolCalls []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tool_calls"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Input    json.RawMessage `json:"input"`
		Contents []struct {
			Parts []struct {
				FunctionCall *struct {
					Name string `json:"name"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"contents"`
		ToolChoice json.RawMessage `json:"tool_choice"`
		ToolConfig struct {
			FunctionCallingConfig struct {
				AllowedFunctionNames []string `json:"allowedFunctionNames"`
			} `json:"functionCallingConfig"`
			ToolChoice struct {
				Tool struct {
					Name string `json:"name"`
				} `json:"tool"`
			} `json:"toolChoice"`
		} `json:"toolConfig"`
	}
	if json.Unmarshal(body, &b) != nil {
		return used, choice
	}
	type block struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		ToolUse *struct {
			Name string `json:"name"`
		} `json:"toolUse"`
	}
	for _, m := range b.Messages {
		for _, tc := range m.ToolCalls { // OpenAI chat
			used[tc.Function.Name] = true
		}
		var blocks []block // Anthropic tool_use, Bedrock toolUse
		if json.Unmarshal(m.Content, &blocks) == nil {
			for _, bl := range blocks {
				if bl.Type == "tool_use" || bl.Type == "server_tool_use" || bl.Type == "mcp_tool_use" {
					used[bl.Name] = true
				}
				if bl.ToolUse != nil {
					used[bl.ToolUse.Name] = true
				}
			}
		}
	}
	var items []block // Responses function_call
	if json.Unmarshal(b.Input, &items) == nil {
		for _, it := range items {
			if it.Type == "function_call" || it.Type == "custom_tool_call" {
				used[it.Name] = true
			}
		}
	}
	for _, c := range b.Contents { // Google functionCall
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				used[p.FunctionCall.Name] = true
			}
		}
	}
	// tool_choice: OpenAI {"function":{"name"}} and allowed_tools, Anthropic
	// and Responses {"name"}; Google allowedFunctionNames; Bedrock
	// toolChoice.tool.name
	var tc struct {
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
		AllowedTools struct {
			Tools []struct {
				Name     string `json:"name"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		} `json:"allowed_tools"`
	}
	if json.Unmarshal(b.ToolChoice, &tc) == nil {
		choice[tc.Name], choice[tc.Function.Name] = true, true
		for _, t := range tc.AllowedTools.Tools {
			choice[t.Name], choice[t.Function.Name] = true, true
		}
	}
	for _, n := range b.ToolConfig.FunctionCallingConfig.AllowedFunctionNames {
		choice[n] = true
	}
	choice[b.ToolConfig.ToolChoice.Tool.Name] = true
	delete(used, "")
	delete(choice, "")
	return used, choice
}
