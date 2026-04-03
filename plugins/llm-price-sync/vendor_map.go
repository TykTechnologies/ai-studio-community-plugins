package main

import "strings"

// vendorMap maps llm-prices.com vendor names to platform vendor slugs.
// Source vendor names (from llm-prices.com): amazon, anthropic, deepseek, google,
// minimax, mistral, moonshot-ai, openai, qwen, xai
var vendorMap = map[string]string{
	"anthropic":     "anthropic",
	"openai":        "openai",
	"google":        "google_ai",
	"google ai":     "google_ai",
	"amazon":        "bedrock",
	"aws bedrock":   "bedrock",
	"bedrock":       "bedrock",
	"vertex ai":     "vertex",
	"vertex":        "vertex",
	"hugging face":  "huggingface",
	"huggingface":   "huggingface",
	"mistral":       "mistral",
	"mistral ai":    "mistral",
	"deepseek":      "deepseek",
	"xai":           "xai",
	"qwen":          "qwen",
	"minimax":       "minimax",
	"moonshot-ai":   "moonshot-ai",
}

func mapVendor(source string) (string, bool) {
	slug, ok := vendorMap[strings.ToLower(strings.TrimSpace(source))]
	return slug, ok
}
