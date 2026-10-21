package main

import (
	"bytes"
	"encoding/json"
	"regexp"
)

// usage is the prompt-side token usage a vendor reports for one response.
// InputTokens is the whole prompt, cached tokens included, whatever the
// vendor's own convention (Anthropic and Bedrock report cache reads and
// writes apart from input_tokens; OpenAI and Google include them).
type usage struct {
	InputTokens      int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	OutputTokens     int64
}

// rawUsage is every usage field the vendors use, in their spellings.
type rawUsage struct {
	// Anthropic, OpenAI Responses
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	InputTokensDetails       *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	// OpenAI chat
	PromptTokens        *int64 `json:"prompt_tokens"`
	CompletionTokens    *int64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	// Google (usageMetadata)
	PromptTokenCount        *int64 `json:"promptTokenCount"`
	CandidatesTokenCount    *int64 `json:"candidatesTokenCount"`
	CachedContentTokenCount *int64 `json:"cachedContentTokenCount"`
	// Bedrock Converse
	BedrockInputTokens      *int64 `json:"inputTokens"`
	BedrockOutputTokens     *int64 `json:"outputTokens"`
	CacheReadInputTokensBR  *int64 `json:"cacheReadInputTokens"`
	CacheWriteInputTokensBR *int64 `json:"cacheWriteInputTokens"`
}

func val(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// normalise reads a raw usage object. ok is false when it holds no prompt
// count at all.
func (r rawUsage) normalise() (usage, bool) {
	switch {
	case r.PromptTokens != nil: // OpenAI chat
		u := usage{InputTokens: *r.PromptTokens, OutputTokens: val(r.CompletionTokens)}
		if r.PromptTokensDetails != nil {
			u.CacheReadTokens = val(r.PromptTokensDetails.CachedTokens)
		}
		return u, true
	case r.PromptTokenCount != nil: // Google
		return usage{InputTokens: *r.PromptTokenCount, CacheReadTokens: val(r.CachedContentTokenCount), OutputTokens: val(r.CandidatesTokenCount)}, true
	case r.BedrockInputTokens != nil: // Bedrock Converse: input excludes the cache
		read, write := val(r.CacheReadInputTokensBR), val(r.CacheWriteInputTokensBR)
		return usage{InputTokens: *r.BedrockInputTokens + read + write, CacheReadTokens: read, CacheWriteTokens: write, OutputTokens: val(r.BedrockOutputTokens)}, true
	case r.InputTokensDetails != nil: // OpenAI Responses: input includes the cache
		return usage{InputTokens: val(r.InputTokens), CacheReadTokens: val(r.InputTokensDetails.CachedTokens), OutputTokens: val(r.OutputTokens)}, true
	case r.InputTokens != nil: // Anthropic: input excludes the cache
		read, write := val(r.CacheReadInputTokens), val(r.CacheCreationInputTokens)
		return usage{InputTokens: *r.InputTokens + read + write, CacheReadTokens: read, CacheWriteTokens: write, OutputTokens: val(r.OutputTokens)}, true
	}
	return usage{}, false
}

// usageHolder is where a response or event carries its usage: at the top
// (OpenAI chat, Anthropic message, Bedrock Converse), in usageMetadata
// (Google), in message (Anthropic message_start), in response (Responses
// events) or in metadata (Bedrock converse-stream).
type usageHolder struct {
	Usage         *rawUsage `json:"usage"`
	UsageMetadata *rawUsage `json:"usageMetadata"`
	Message       *struct {
		Usage *rawUsage `json:"usage"`
	} `json:"message"`
	Response *struct {
		Usage *rawUsage `json:"usage"`
	} `json:"response"`
	Metadata *struct {
		Usage *rawUsage `json:"usage"`
	} `json:"metadata"`
}

func (h usageHolder) raw() []*rawUsage {
	var out []*rawUsage
	for _, r := range []*rawUsage{h.Usage, h.UsageMetadata} {
		if r != nil {
			out = append(out, r)
		}
	}
	if h.Message != nil && h.Message.Usage != nil {
		out = append(out, h.Message.Usage)
	}
	if h.Response != nil && h.Response.Usage != nil {
		out = append(out, h.Response.Usage)
	}
	if h.Metadata != nil && h.Metadata.Usage != nil {
		out = append(out, h.Metadata.Usage)
	}
	return out
}

// parseUsage reads usage from a buffered JSON response. ok is false when it
// carries none.
func parseUsage(body []byte) (usage, bool) {
	var h usageHolder
	if json.Unmarshal(body, &h) != nil {
		return usage{}, false
	}
	return merge(h.raw())
}

// merge combines the usage objects of a response or a stream: each count
// is the largest any event reported (Anthropic sends input in message_start
// and output in message_delta; Google repeats running totals).
func merge(raws []*rawUsage) (usage, bool) {
	var u usage
	found := false
	for _, r := range raws {
		n, ok := r.normalise()
		if !ok {
			// An output-only update (Anthropic message_delta).
			u.OutputTokens = max(u.OutputTokens, val(r.OutputTokens))
			continue
		}
		found = true
		u.InputTokens = max(u.InputTokens, n.InputTokens)
		u.CacheReadTokens = max(u.CacheReadTokens, n.CacheReadTokens)
		u.CacheWriteTokens = max(u.CacheWriteTokens, n.CacheWriteTokens)
		u.OutputTokens = max(u.OutputTokens, n.OutputTokens)
	}
	return u, found
}

// bedrockUsage finds the usage object in a Bedrock event stream, whose
// frames are binary with JSON payloads.
var bedrockUsage = regexp.MustCompile(`"usage"\s*:\s*\{[^{}]*\}`)

// parseStreamUsage reads usage from an accumulated stream: server-sent
// events (OpenAI, Anthropic, Google, Responses) or a Bedrock event stream.
// ok is false when no event carried usage (an OpenAI stream without
// stream_options.include_usage).
func parseStreamUsage(stream []byte) (usage, bool) {
	var raws []*rawUsage
	for _, line := range bytes.Split(stream, []byte("\n")) {
		line = bytes.TrimSpace(line)
		data, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		data = bytes.TrimSpace(data)
		if len(data) == 0 || data[0] != '{' {
			continue
		}
		var h usageHolder
		if json.Unmarshal(data, &h) == nil {
			raws = append(raws, h.raw()...)
		}
	}
	if len(raws) == 0 {
		for _, m := range bedrockUsage.FindAll(stream, -1) {
			var h usageHolder
			if json.Unmarshal(append(append([]byte("{"), m...), '}'), &h) == nil {
				raws = append(raws, h.raw()...)
			}
		}
	}
	return merge(raws)
}
