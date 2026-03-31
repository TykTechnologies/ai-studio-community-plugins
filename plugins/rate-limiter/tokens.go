package main

import (
	"encoding/json"
)

// TokenUsage represents extracted token usage from an LLM response.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheReadTokens  int
	CacheWriteTokens int
}

// ExtractTokenUsage parses actual token usage from a vendor-agnostic LLM response body.
// Supports OpenAI (prompt_tokens, completion_tokens) and Anthropic (input_tokens, output_tokens).
func ExtractTokenUsage(responseBody []byte) TokenUsage {
	if len(responseBody) == 0 {
		return TokenUsage{}
	}

	var response map[string]interface{}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return TokenUsage{}
	}

	usage, ok := response["usage"].(map[string]interface{})
	if !ok {
		return TokenUsage{}
	}

	tokens := TokenUsage{}

	// Anthropic: input_tokens / output_tokens
	// OpenAI: prompt_tokens / completion_tokens
	if v, ok := usage["input_tokens"].(float64); ok {
		tokens.PromptTokens = int(v)
	} else if v, ok := usage["prompt_tokens"].(float64); ok {
		tokens.PromptTokens = int(v)
	}

	if v, ok := usage["output_tokens"].(float64); ok {
		tokens.CompletionTokens = int(v)
	} else if v, ok := usage["completion_tokens"].(float64); ok {
		tokens.CompletionTokens = int(v)
	}

	if v, ok := usage["total_tokens"].(float64); ok {
		tokens.TotalTokens = int(v)
	} else {
		tokens.TotalTokens = tokens.PromptTokens + tokens.CompletionTokens
	}

	// Anthropic prompt caching
	if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
		tokens.CacheWriteTokens = int(v)
	}
	if v, ok := usage["cache_read_input_tokens"].(float64); ok {
		tokens.CacheReadTokens = int(v)
	}

	return tokens
}
