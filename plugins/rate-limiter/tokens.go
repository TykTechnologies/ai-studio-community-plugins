package main

import (
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
)

// TokenUsage represents extracted token usage from an LLM response.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CacheReadTokens  int
	CacheWriteTokens int
}

// ExtractTokenUsage parses actual token usage from an LLM response body in its
// vendor's shape: OpenAI (prompt_tokens, completion_tokens), Anthropic
// (input_tokens, output_tokens), Google (usageMetadata) and Bedrock Converse
// (camelCase inputTokens, outputTokens), as a REST body or a whole stream.
func ExtractTokenUsage(vendor string, responseBody []byte) TokenUsage {
	u := plugin_sdk.ResponseUsage(vendor, responseBody)
	return TokenUsage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}
