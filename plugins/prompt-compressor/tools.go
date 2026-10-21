package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
)

// Tool-definition cost (eac.39): what the tools a request offers cost in
// prompt tokens, and which of them the model calls. Only tool names and
// sizes are kept; schemas, descriptions and arguments never leave the
// request.

// offeredTool is one tool definition in a request: its name and its
// definition's size in tokens (UTF-8 bytes / 4 of its JSON).
type offeredTool struct {
	Name   string
	Tokens int64
}

// toolsInfo is what a request offers.
type toolsInfo struct {
	Tools  []offeredTool
	Tokens int64
	// Hash identifies the tool list (definitions in order), so a change
	// shows up as a new list version, which also breaks the prompt cache.
	Hash string
}

// toolListHashLen is the hex length of a tool list hash.
const toolListHashLen = 16

// maxToolsPerRequest bounds the tools read from one request.
const maxToolsPerRequest = 512

// parseOfferedTools reads the tool definitions of a request body in its
// format. Bodies without tools, or that do not parse, offer none.
func parseOfferedTools(format string, body []byte) toolsInfo {
	var defs []json.RawMessage
	var names []string
	add := func(raw json.RawMessage, name string) {
		if len(defs) < maxToolsPerRequest {
			defs, names = append(defs, raw), append(names, name)
		}
	}
	switch format {
	case "anthropic":
		var b struct {
			Tools []json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(body, &b) != nil {
			return toolsInfo{}
		}
		for _, raw := range b.Tools {
			var t struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(raw, &t)
			add(raw, t.Name)
		}
	case "openai_responses":
		var b struct {
			Tools []json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(body, &b) != nil {
			return toolsInfo{}
		}
		for _, raw := range b.Tools {
			var t struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			_ = json.Unmarshal(raw, &t)
			name := t.Name
			if name == "" {
				name = t.Type // a built-in tool: web_search, file_search…
			}
			add(raw, name)
		}
	case "google_ai":
		var b struct {
			Tools []struct {
				FunctionDeclarations  []json.RawMessage `json:"functionDeclarations"`
				FunctionDeclarations2 []json.RawMessage `json:"function_declarations"`
			} `json:"tools"`
		}
		if json.Unmarshal(body, &b) != nil {
			return toolsInfo{}
		}
		for _, t := range b.Tools {
			for _, raw := range append(t.FunctionDeclarations, t.FunctionDeclarations2...) {
				var d struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(raw, &d)
				add(raw, d.Name)
			}
		}
	case "bedrock":
		var b struct {
			ToolConfig struct {
				Tools []json.RawMessage `json:"tools"`
			} `json:"toolConfig"`
		}
		if json.Unmarshal(body, &b) != nil {
			return toolsInfo{}
		}
		for _, raw := range b.ToolConfig.Tools {
			var t struct {
				ToolSpec struct {
					Name string `json:"name"`
				} `json:"toolSpec"`
			}
			_ = json.Unmarshal(raw, &t)
			if t.ToolSpec.Name == "" {
				continue // a cachePoint, not a tool
			}
			add(raw, t.ToolSpec.Name)
		}
	default: // OpenAI chat and the formats that copy it (Ollama)
		var b struct {
			Tools []json.RawMessage `json:"tools"`
		}
		if json.Unmarshal(body, &b) != nil {
			return toolsInfo{}
		}
		for _, raw := range b.Tools {
			var t struct {
				Name     string `json:"name"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			_ = json.Unmarshal(raw, &t)
			name := t.Function.Name
			if name == "" {
				name = t.Name
			}
			add(raw, name)
		}
	}
	if len(defs) == 0 {
		return toolsInfo{}
	}
	info := toolsInfo{Tools: make([]offeredTool, len(defs))}
	h := sha256.New()
	for i, raw := range defs {
		n := int64((len(raw) + 3) / 4)
		info.Tools[i] = offeredTool{Name: names[i], Tokens: n}
		info.Tokens += n
		var c bytes.Buffer
		if json.Compact(&c, raw) != nil {
			c.Reset()
			c.Write(raw)
		}
		h.Write([]byte{byte(c.Len() >> 24), byte(c.Len() >> 16), byte(c.Len() >> 8), byte(c.Len())})
		h.Write(c.Bytes())
	}
	info.Hash = hex.EncodeToString(h.Sum(nil))[:toolListHashLen]
	return info
}

// calledHolder is every place a response or a stream event names a tool
// the model called, in each vendor's spelling.
type calledHolder struct {
	// OpenAI chat: choices[].message.tool_calls / choices[].delta.tool_calls
	Choices []struct {
		Message *calledOpenAI `json:"message"`
		Delta   *calledOpenAI `json:"delta"`
	} `json:"choices"`
	// Anthropic message: content[] tool_use / server_tool_use
	Content []calledBlock `json:"content"`
	// Anthropic stream: content_block_start
	ContentBlock *calledBlock `json:"content_block"`
	// Responses: output[] items (an array); Bedrock Converse: output.message
	// (an object)
	Output json.RawMessage `json:"output"`
	// Responses stream: response.output_item.added / .done
	Item *calledBlock `json:"item"`
	// Responses stream: response.completed
	Response *struct {
		Output []calledBlock `json:"output"`
	} `json:"response"`
	// Google: candidates[].content.parts[].functionCall
	Candidates []struct {
		Content struct {
			Parts []struct {
				FunctionCall *struct {
					Name string `json:"name"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	// Bedrock converse-stream: contentBlockStart.start.toolUse
	Start *struct {
		ToolUse *calledBlock `json:"toolUse"`
	} `json:"start"`
}

type calledOpenAI struct {
	ToolCalls []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type calledBlock struct {
	Type    string       `json:"type"`
	Name    string       `json:"name"`
	ToolUse *calledBlock `json:"toolUse"`
}

// calledTypes are the block and item types that are a tool call.
var calledTypes = map[string]bool{
	"tool_use": true, "server_tool_use": true, "mcp_tool_use": true, // Anthropic
	"function_call": true, "custom_tool_call": true, // Responses
}

func (h *calledHolder) names(add func(string)) {
	for _, c := range h.Choices {
		for _, m := range []*calledOpenAI{c.Message, c.Delta} {
			if m != nil {
				for _, tc := range m.ToolCalls {
					add(tc.Function.Name)
				}
			}
		}
	}
	block := func(b *calledBlock) {
		if b == nil {
			return
		}
		if calledTypes[b.Type] {
			add(b.Name)
		}
		if b.ToolUse != nil {
			add(b.ToolUse.Name)
		}
	}
	for i := range h.Content {
		block(&h.Content[i])
	}
	block(h.ContentBlock)
	block(h.Item)
	if h.Response != nil {
		for i := range h.Response.Output {
			block(&h.Response.Output[i])
		}
	}
	if len(h.Output) > 0 {
		switch h.Output[0] {
		case '[':
			var items []calledBlock
			if json.Unmarshal(h.Output, &items) == nil {
				for i := range items {
					block(&items[i])
				}
			}
		case '{':
			var o struct {
				Message struct {
					Content []calledBlock `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(h.Output, &o) == nil {
				for i := range o.Message.Content {
					block(&o.Message.Content[i])
				}
			}
		}
	}
	for _, c := range h.Candidates {
		for _, p := range c.Content.Parts {
			if p.FunctionCall != nil {
				add(p.FunctionCall.Name)
			}
		}
	}
	if h.Start != nil {
		block(h.Start.ToolUse)
	}
}

// maxCalledTools bounds the distinct names read from one response.
const maxCalledTools = 64

// calledSet collects distinct tool names, sorted.
type calledSet map[string]bool

func (s calledSet) add(name string) {
	if name != "" && len(s) < maxCalledTools {
		s[name] = true
	}
}

func (s calledSet) sorted() []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, 0, len(s))
	for n := range s {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// offeredOnly keeps the called names the request offered; the others
// become records.OtherTool. A name comes from model output, so one it was
// never offered (a prompt-injected model can "call" a tool named after a
// secret it saw) is not kept.
func offeredOnly(called []string, offered toolsInfo) []string {
	if len(called) == 0 {
		return nil
	}
	names := make(map[string]bool, len(offered.Tools))
	for _, t := range offered.Tools {
		names[t.Name] = true
	}
	var out []string
	other := false
	for _, n := range called {
		switch {
		case names[n]:
			out = append(out, n)
		case !other:
			other = true
			out = append(out, records.OtherTool)
		}
	}
	return out
}

// parseCalledTools reads the tools a buffered response called.
func parseCalledTools(body []byte) []string {
	set := calledSet{}
	var h calledHolder
	if json.Unmarshal(body, &h) == nil {
		h.names(set.add)
	}
	return set.sorted()
}

// bedrockToolUse finds tool calls in a Bedrock event stream, whose frames
// are binary with JSON payloads. Model text inside a frame is a JSON
// string, its quotes escaped, so it cannot forge one.
var bedrockToolUse = regexp.MustCompile(`"toolUse"\s*:\s*\{[^{}]*"name"\s*:\s*"([^"\\]{1,200})"`)

// parseStreamCalledTools reads the tools an accumulated stream called:
// server-sent events (OpenAI, Anthropic, Google, Responses), a Google JSON
// array stream, or a Bedrock event stream.
func parseStreamCalledTools(stream []byte) []string {
	set := calledSet{}
	sse := false
	for _, line := range bytes.Split(stream, []byte("\n")) {
		data, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
		if !ok {
			continue
		}
		sse = true
		data = bytes.TrimSpace(data)
		if len(data) == 0 || data[0] != '{' {
			continue
		}
		var h calledHolder
		if json.Unmarshal(data, &h) == nil {
			h.names(set.add)
		}
	}
	if !sse {
		trimmed := bytes.TrimSpace(stream)
		var arr []calledHolder
		if len(trimmed) > 0 && trimmed[0] == '[' && json.Unmarshal(trimmed, &arr) == nil {
			for i := range arr {
				arr[i].names(set.add)
			}
		} else {
			for _, m := range bedrockToolUse.FindAllSubmatch(stream, -1) {
				set.add(string(m[1]))
			}
		}
	}
	return set.sorted()
}
