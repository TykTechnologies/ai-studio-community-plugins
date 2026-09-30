package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// The cache stores one JSON response per request and replays it either as
// JSON or, converted, as the vendor's stream. These helpers decide which
// form a request wants, whether a response can be replayed in that form
// without losing anything, and which headers describe the replayed body.

// API formats the cache can convert between JSON and a stream.
const (
	formatOpenAI    = "openai"
	formatAnthropic = "anthropic"
	formatGoogle    = "google"
)

// apiFormat is the wire format of an LLM vendor's native API on the gateway's
// /llm/ endpoints, or "" when the cache cannot convert its responses (the
// cache then only replays a response in the form it was stored in).
func apiFormat(vendor string) string {
	v := strings.ToLower(vendor)
	switch {
	case strings.Contains(v, "anthropic"):
		return formatAnthropic
	case strings.Contains(v, "google"), strings.Contains(v, "gemini"), strings.Contains(v, "vertex"):
		return formatGoogle
	case v == "openai", v == "azure", v == "mock":
		return formatOpenAI
	}
	return ""
}

// apiPath is the vendor API path of a gateway request: the part after
// /llm/{call,rest,stream}/{slug}, with Gemini's streaming method folded into
// the plain one, since the cache serves both from one entry.
func apiPath(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if strings.HasPrefix(path, "/llm/") {
		parts := strings.SplitN(strings.TrimPrefix(path, "/llm/"), "/", 3)
		if len(parts) == 3 {
			path = "/" + parts[2]
		} else {
			path = "/"
		}
	}
	return strings.Replace(path, ":streamGenerateContent", ":generateContent", 1)
}

// modelFromPath returns the model named in a Gemini-style path
// (.../models/{model}:method), where the request body carries none.
func modelFromPath(path string) string {
	i := strings.Index(path, "/models/")
	if i < 0 {
		return ""
	}
	model := path[i+len("/models/"):]
	if j := strings.IndexAny(model, ":/?"); j >= 0 {
		model = model[:j]
	}
	return model
}

// requestStreams reports whether a request asks for a streamed response.
func requestStreams(vendor, path string, body []byte) bool {
	// Gemini chooses streaming by method, not by a body field.
	if strings.Contains(path, ":streamGenerateContent") {
		return true
	}
	if strings.Contains(path, ":generateContent") {
		return false
	}
	var request map[string]interface{}
	if json.Unmarshal(body, &request) == nil {
		if stream, ok := request["stream"].(bool); ok {
			return stream
		}
	}
	// Ollama's native API streams unless told not to.
	return strings.Contains(strings.ToLower(vendor), "ollama") && strings.Contains(path, "/api/")
}

// streamReplayable reports whether a stored stream can be rebuilt into JSON
// (and later streamed again) without losing anything: a single choice of
// plain text. Tool calls, thinking and other block types are not rebuilt.
func streamReplayable(format string, sse []byte) bool {
	sawData := false
	scanner := bufio.NewScanner(bytes.NewReader(sse))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == SSEDone || data == "" {
			continue
		}
		var chunk map[string]interface{}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			return false
		}
		sawData = true
		switch format {
		case formatAnthropic:
			switch chunk["type"] {
			case "error":
				return false
			case AnthropicContentBlockStart:
				block, _ := chunk["content_block"].(map[string]interface{})
				if block == nil || block["type"] != "text" || chunk["index"] != float64(0) {
					return false
				}
			case AnthropicContentBlockDelta:
				delta, _ := chunk["delta"].(map[string]interface{})
				if delta == nil || delta["type"] != "text_delta" || chunk["index"] != float64(0) {
					return false
				}
			}
		case formatOpenAI:
			choices, _ := chunk["choices"].([]interface{})
			for _, c := range choices {
				choice, _ := c.(map[string]interface{})
				if choice == nil || choice["index"] != float64(0) {
					return false
				}
				delta, _ := choice["delta"].(map[string]interface{})
				if delta != nil && (hasValue(delta["tool_calls"]) || hasValue(delta["function_call"])) {
					return false
				}
			}
		case formatGoogle:
			candidates, _ := chunk["candidates"].([]interface{})
			if len(candidates) > 1 {
				return false
			}
			for _, c := range candidates {
				if !textOnlyGoogleCandidate(c, false) {
					return false
				}
			}
		default:
			return false
		}
	}
	return sawData
}

// jsonReplayableAsStream reports whether a stored JSON response can be
// streamed by ConvertJSONToSSE without losing anything.
func jsonReplayableAsStream(format string, body []byte) bool {
	var response map[string]interface{}
	if json.Unmarshal(body, &response) != nil {
		return false
	}
	switch format {
	case formatAnthropic:
		content, _ := response["content"].([]interface{})
		if len(content) != 1 {
			return false
		}
		block, _ := content[0].(map[string]interface{})
		_, isText := block["text"].(string)
		return block != nil && block["type"] == "text" && isText
	case formatOpenAI:
		choices, _ := response["choices"].([]interface{})
		if len(choices) != 1 {
			return false
		}
		choice, _ := choices[0].(map[string]interface{})
		message, _ := choice["message"].(map[string]interface{})
		if message == nil || hasValue(message["tool_calls"]) || hasValue(message["function_call"]) {
			return false
		}
		_, isText := message["content"].(string)
		return isText
	case formatGoogle:
		candidates, _ := response["candidates"].([]interface{})
		return len(candidates) == 1 && textOnlyGoogleCandidate(candidates[0], true)
	}
	return false
}

// textOnlyGoogleCandidate reports whether a Gemini candidate holds only text
// parts (single is set when exactly one part is required).
func textOnlyGoogleCandidate(c interface{}, single bool) bool {
	candidate, _ := c.(map[string]interface{})
	if candidate == nil {
		return false
	}
	if idx, ok := candidate["index"].(float64); ok && idx != 0 {
		return false
	}
	content, _ := candidate["content"].(map[string]interface{})
	parts, _ := content["parts"].([]interface{})
	if single && len(parts) != 1 {
		return false
	}
	for _, p := range parts {
		part, _ := p.(map[string]interface{})
		if _, isText := part["text"].(string); !isText || len(part) != 1 {
			return false
		}
	}
	return true
}

func hasValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case []interface{}:
		return len(t) > 0
	case map[string]interface{}:
		return len(t) > 0
	}
	return true
}

// framingHeaders describe one particular response on the wire, so a stored
// copy of them is wrong for a replay: the body may be a different length or
// format, and the gateway sets its own request ID and date.
var framingHeaders = map[string]bool{
	"content-length":    true,
	"content-type":      true,
	"content-encoding":  true,
	"transfer-encoding": true,
	"connection":        true,
	"keep-alive":        true,
	"date":              true,
	"x-request-id":      true,
	"cache-control":     true,
}

// storableHeaders are the response headers worth keeping with an entry.
func storableHeaders(headers map[string]string) map[string]string {
	kept := make(map[string]string, len(headers))
	for k, v := range headers {
		lk := strings.ToLower(k)
		if framingHeaders[lk] || strings.HasPrefix(lk, "x-cache") {
			continue
		}
		kept[k] = v
	}
	return kept
}

// hitHeaders are the headers of a replayed body: the stored ones plus
// framing that matches what is actually sent.
func hitHeaders(stored map[string]string, body []byte, stream bool) map[string]string {
	headers := storableHeaders(stored)
	if stream {
		headers["Content-Type"] = "text/event-stream"
		headers["Cache-Control"] = "no-cache"
	} else {
		headers["Content-Type"] = "application/json"
	}
	headers["Content-Length"] = strconv.Itoa(len(body))
	return headers
}
