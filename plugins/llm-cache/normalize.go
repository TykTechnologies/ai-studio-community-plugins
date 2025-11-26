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

// CacheKeyComponents holds the components used to generate a cache key
type CacheKeyComponents struct {
	Namespace    string
	Model        string
	Messages     []NormalizedMessage
	SystemPrompt string
	Tools        []NormalizedTool
	Temperature  float64
}

// NormalizedMessage represents a normalized chat message
type NormalizedMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NormalizedTool represents a normalized tool definition
type NormalizedTool struct {
	Type     string      `json:"type"`
	Function interface{} `json:"function,omitempty"`
}

// whitespaceRegex matches multiple whitespace characters
var whitespaceRegex = regexp.MustCompile(`\s+`)

// GenerateCacheKey generates a deterministic cache key from request components
func GenerateCacheKey(namespace string, requestBody []byte, normalizePrompts bool) (string, string, error) {
	var request map[string]interface{}
	if err := json.Unmarshal(requestBody, &request); err != nil {
		return "", "", fmt.Errorf("failed to parse request body: %w", err)
	}

	components := CacheKeyComponents{
		Namespace: namespace,
	}

	// Extract model
	if model, ok := request["model"].(string); ok {
		components.Model = model
	}

	// Extract temperature (default to 1.0 if not specified)
	components.Temperature = 1.0
	if temp, ok := request["temperature"].(float64); ok {
		components.Temperature = temp
	}

	// Extract and normalize system prompt (Anthropic style)
	if system, ok := request["system"].(string); ok {
		if normalizePrompts {
			components.SystemPrompt = normalizeText(system)
		} else {
			components.SystemPrompt = system
		}
	}

	// Extract and normalize messages
	if messages, ok := request["messages"].([]interface{}); ok {
		for _, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				normalized := normalizeMessage(msgMap, normalizePrompts)
				components.Messages = append(components.Messages, normalized)
			}
		}
	}

	// Extract and normalize tools
	if tools, ok := request["tools"].([]interface{}); ok {
		components.Tools = normalizeTools(tools)
	}

	// Generate hash from components
	hash, err := hashComponents(components)
	if err != nil {
		return "", "", fmt.Errorf("failed to hash components: %w", err)
	}

	// Build cache key
	cacheKey := fmt.Sprintf("cache:resp:%s:%s", namespace, hash)

	return cacheKey, components.Model, nil
}

// normalizeMessage normalizes a single message
func normalizeMessage(msg map[string]interface{}, normalizePrompts bool) NormalizedMessage {
	normalized := NormalizedMessage{}

	if role, ok := msg["role"].(string); ok {
		normalized.Role = role
	}

	// Handle different content formats
	if content, ok := msg["content"].(string); ok {
		if normalizePrompts {
			normalized.Content = normalizeText(content)
		} else {
			normalized.Content = content
		}
	} else if contentArray, ok := msg["content"].([]interface{}); ok {
		// Handle array content (e.g., Anthropic vision messages)
		var parts []string
		for _, item := range contentArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if text, ok := itemMap["text"].(string); ok {
					if normalizePrompts {
						parts = append(parts, normalizeText(text))
					} else {
						parts = append(parts, text)
					}
				}
				// For images, include the source hash or URL
				if source, ok := itemMap["source"].(map[string]interface{}); ok {
					if data, ok := source["data"].(string); ok {
						// Hash the image data for consistency
						hash := sha256.Sum256([]byte(data))
						parts = append(parts, fmt.Sprintf("[image:%s]", hex.EncodeToString(hash[:8])))
					}
				}
				if imageURL, ok := itemMap["image_url"].(map[string]interface{}); ok {
					if url, ok := imageURL["url"].(string); ok {
						parts = append(parts, fmt.Sprintf("[image:%s]", url))
					}
				}
			}
		}
		normalized.Content = strings.Join(parts, " ")
	}

	return normalized
}

// normalizeTools normalizes and sorts tool definitions
func normalizeTools(tools []interface{}) []NormalizedTool {
	normalized := make([]NormalizedTool, 0, len(tools))

	for _, tool := range tools {
		if toolMap, ok := tool.(map[string]interface{}); ok {
			nt := NormalizedTool{}

			if toolType, ok := toolMap["type"].(string); ok {
				nt.Type = toolType
			}

			if function, ok := toolMap["function"].(map[string]interface{}); ok {
				// Canonicalize the function definition
				nt.Function = canonicalizeJSON(function)
			}

			normalized = append(normalized, nt)
		}
	}

	// Sort tools by type and function name for consistency
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Type != normalized[j].Type {
			return normalized[i].Type < normalized[j].Type
		}
		iName := getToolName(normalized[i])
		jName := getToolName(normalized[j])
		return iName < jName
	})

	return normalized
}

// getToolName extracts the tool name for sorting
func getToolName(tool NormalizedTool) string {
	if funcMap, ok := tool.Function.(map[string]interface{}); ok {
		if name, ok := funcMap["name"].(string); ok {
			return name
		}
	}
	return ""
}

// normalizeText normalizes text by trimming and collapsing whitespace
func normalizeText(text string) string {
	// Trim leading and trailing whitespace
	text = strings.TrimSpace(text)

	// Collapse multiple whitespace characters into single space
	text = whitespaceRegex.ReplaceAllString(text, " ")

	return text
}

// canonicalizeJSON recursively sorts JSON object keys for consistent hashing
func canonicalizeJSON(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		// Sort keys and recursively canonicalize values
		result := make(map[string]interface{})
		for key, val := range v {
			result[key] = canonicalizeJSON(val)
		}
		return result
	case []interface{}:
		// Recursively canonicalize array elements
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = canonicalizeJSON(val)
		}
		return result
	default:
		return v
	}
}

// hashComponents generates a SHA-256 hash of the cache key components
func hashComponents(components CacheKeyComponents) (string, error) {
	// Create a canonical representation
	canonical := struct {
		Namespace    string              `json:"ns"`
		Model        string              `json:"m"`
		Messages     []NormalizedMessage `json:"msg"`
		SystemPrompt string              `json:"sys"`
		Tools        []NormalizedTool    `json:"tools"`
		Temperature  float64             `json:"temp"`
	}{
		Namespace:    components.Namespace,
		Model:        components.Model,
		Messages:     components.Messages,
		SystemPrompt: components.SystemPrompt,
		Tools:        components.Tools,
		Temperature:  components.Temperature,
	}

	// Marshal with sorted keys using a custom encoder
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(canonical); err != nil {
		return "", err
	}

	// Generate SHA-256 hash
	hash := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(hash[:]), nil
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
