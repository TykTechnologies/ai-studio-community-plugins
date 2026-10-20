package main

import (
	"context"
	"sync"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
)

// llmInfo is what the post-auth hook needs to know about an LLM that its
// context does not carry: the API endpoint, which decides an operation's
// canonical path and so the request format (requestformat.For).
type llmInfo struct {
	Endpoint string
}

const (
	llmInfoTTL      = 30 * time.Second
	llmInfoErrorTTL = 5 * time.Second
	llmInfoTimeout  = 500 * time.Millisecond
	llmInfoMaxSize  = 4096
)

// llmInfoCache caches llmInfo per LLM ID. A failed lookup (no llms.read
// scope yet, no broker) is cached briefly as an empty endpoint: the format
// then falls back to the vendor's own, which at worst skips a body.
type llmInfoCache struct {
	mu      sync.Mutex
	now     func() time.Time
	fetch   func(ctx plugin_sdk.Context, llmID uint32) (llmInfo, error)
	entries map[uint32]llmInfoEntry
}

type llmInfoEntry struct {
	info    llmInfo
	expires time.Time
}

func newLLMInfoCache(fetch func(plugin_sdk.Context, uint32) (llmInfo, error)) *llmInfoCache {
	return &llmInfoCache{now: time.Now, fetch: fetch, entries: map[uint32]llmInfoEntry{}}
}

func (c *llmInfoCache) get(ctx plugin_sdk.Context, llmID uint32) llmInfo {
	if llmID == 0 {
		return llmInfo{}
	}
	c.mu.Lock()
	e, ok := c.entries[llmID]
	c.mu.Unlock()
	now := c.now()
	if ok && now.Before(e.expires) {
		return e.info
	}
	info, err := c.fetch(ctx, llmID)
	ttl := llmInfoTTL
	if err != nil {
		info, ttl = llmInfo{}, llmInfoErrorTTL
	}
	c.mu.Lock()
	if len(c.entries) >= llmInfoMaxSize {
		c.entries = map[uint32]llmInfoEntry{}
	}
	c.entries[llmID] = llmInfoEntry{info: info, expires: now.Add(ttl)}
	c.mu.Unlock()
	return info
}

// fetchLLMInfo reads an LLM through the gateway's service API (llms.read).
func fetchLLMInfo(ctx plugin_sdk.Context, llmID uint32) (llmInfo, error) {
	if ctx.Services == nil || ctx.Services.Gateway() == nil {
		return llmInfo{}, errNoBroker
	}
	base := ctx.Context
	if base == nil {
		base = context.Background()
	}
	c, cancel := context.WithTimeout(base, llmInfoTimeout)
	defer cancel()
	resp, err := ctx.Services.Gateway().GetLLM(c, llmID)
	if err != nil {
		return llmInfo{}, err
	}
	r, ok := resp.(*mgmtpb.GetLLMResponse)
	if !ok {
		return llmInfo{}, errUnexpectedResponse
	}
	return llmInfo{Endpoint: r.GetLlm().GetEndpoint()}, nil
}
