package main

import (
	"context"
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
	llmInfoTimeout  = 2 * time.Second
	llmInfoMaxSize  = 4096
)

// newLLMLookup caches llmInfo per LLM ID, fetched in the background
// (asyncLookup). Until an LLM's endpoint is known (the first request, no
// llms.read scope, no broker), the format is the vendor's own, which at
// worst skips a body.
func (p *PromptCompressorPlugin) newLLMLookup() *asyncLookup[uint32, llmInfo] {
	return newAsyncLookup(llmInfoTTL, llmInfoErrorTTL, llmInfoMaxSize, func(llmID uint32) (llmInfo, error) {
		return fetchLLMInfo(p.backgroundContext(), llmID)
	})
}

// backgroundContext is a plugin context for host calls made outside a
// request.
func (p *PromptCompressorPlugin) backgroundContext() plugin_sdk.Context {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return plugin_sdk.Context{Runtime: p.runtime, Services: p.services, Context: context.Background()}
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
