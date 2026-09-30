package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CacheScope is where a request goes. The same body sent to another LLM, or
// to another API of the same LLM, is a different request with a differently
// shaped answer, so the scope is part of the key.
type CacheScope struct {
	Namespace string
	LLMID     uint32
	Vendor    string
	// Path is the vendor API path (see apiPath).
	Path string
}

// whitespaceRegex matches multiple whitespace characters
var whitespaceRegex = regexp.MustCompile(`\s+`)

// transportFields ask for a form of the answer, not a different answer: a
// hit is replayed in whichever form the request asks for (see replay.go).
var transportFields = []string{"stream", "stream_options"}

// promptTextFields hold prompt text that NormalizePrompts compares with
// whitespace collapsed: message content, content-part text and system
// prompts, in every vendor's request format.
var promptTextFields = map[string]bool{"content": true, "text": true, "system": true, "prompt": true}

// GenerateCacheKey generates a deterministic cache key for a request. Every
// field of the request is part of the key (anything may change the answer:
// max_tokens, response_format, tool results, Gemini's contents), except the
// transport fields. With normalizePrompts, prompt text is compared with its
// whitespace collapsed and tools are compared regardless of their order.
func GenerateCacheKey(scope CacheScope, requestBody []byte, normalizePrompts bool) (string, string, error) {
	var request map[string]interface{}
	if err := json.Unmarshal(requestBody, &request); err != nil {
		return "", "", fmt.Errorf("failed to parse request body: %w", err)
	}
	if request == nil {
		return "", "", fmt.Errorf("request body is not a JSON object")
	}

	model, _ := request["model"].(string)
	if model == "" {
		model = modelFromPath(scope.Path)
	}

	for _, field := range transportFields {
		delete(request, field)
	}
	var body interface{} = request
	if normalizePrompts {
		if tools, ok := request["tools"].([]interface{}); ok {
			request["tools"] = sortTools(tools)
		}
		body = normalizePromptText(request, "")
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	// encoding/json writes map keys in sorted order, so equal requests
	// encode identically.
	if err := encoder.Encode(struct {
		Namespace string      `json:"ns"`
		LLMID     uint32      `json:"llm"`
		Vendor    string      `json:"vendor"`
		Path      string      `json:"path"`
		Body      interface{} `json:"body"`
	}{scope.Namespace, scope.LLMID, strings.ToLower(scope.Vendor), scope.Path, body}); err != nil {
		return "", "", fmt.Errorf("failed to encode cache key: %w", err)
	}
	hash := sha256.Sum256(buf.Bytes())

	return fmt.Sprintf("cache:resp:%s:%s", scope.Namespace, hex.EncodeToString(hash[:])), model, nil
}

// normalizePromptText returns a copy of v with the whitespace of prompt text
// collapsed. key is the name v was found under.
func normalizePromptText(v interface{}, key string) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = normalizePromptText(val, k)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = normalizePromptText(val, key)
		}
		return out
	case string:
		if promptTextFields[key] {
			return normalizeText(t)
		}
	}
	return v
}

// sortTools orders tool definitions by type and name, whatever the vendor's
// shape ({"type","function":{"name"}} or {"name","input_schema"}), keeping
// each definition whole. Definitions without a name keep their order.
func sortTools(tools []interface{}) []interface{} {
	sorted := make([]interface{}, len(tools))
	copy(sorted, tools)
	sort.SliceStable(sorted, func(i, j int) bool {
		return toolSortKey(sorted[i]) < toolSortKey(sorted[j])
	})
	return sorted
}

func toolSortKey(tool interface{}) string {
	m, _ := tool.(map[string]interface{})
	if m == nil {
		return ""
	}
	name, _ := m["name"].(string)
	if fn, ok := m["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok {
			name = n
		}
	}
	toolType, _ := m["type"].(string)
	return toolType + "\x00" + name
}

// normalizeText normalizes text by trimming and collapsing whitespace
func normalizeText(text string) string {
	// Trim leading and trailing whitespace
	text = strings.TrimSpace(text)

	// Collapse multiple whitespace characters into single space
	text = whitespaceRegex.ReplaceAllString(text, " ")

	return text
}

// ExtractTokensFromResponse extracts token usage from an LLM response
func ExtractTokensFromResponse(responseBody []byte) int {
	var response map[string]interface{}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return 0
	}

	usage, ok := response["usage"].(map[string]interface{})
	if !ok {
		return 0
	}

	// Try different field names used by different providers
	// Anthropic: input_tokens, output_tokens
	// OpenAI: prompt_tokens, completion_tokens, total_tokens
	totalTokens := 0

	if total, ok := usage["total_tokens"].(float64); ok {
		return int(total)
	}

	if input, ok := usage["input_tokens"].(float64); ok {
		totalTokens += int(input)
	} else if prompt, ok := usage["prompt_tokens"].(float64); ok {
		totalTokens += int(prompt)
	}

	if output, ok := usage["output_tokens"].(float64); ok {
		totalTokens += int(output)
	} else if completion, ok := usage["completion_tokens"].(float64); ok {
		totalTokens += int(completion)
	}

	return totalTokens
}
