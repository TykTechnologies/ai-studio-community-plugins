package main

import (
	"context"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	studiopb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
)

// llmInfo is what the post-auth hook needs to know about an LLM that its
// context does not carry: the API endpoint, which decides an operation's
// canonical path and so the request format (requestformat.For), and
// whether request filters screen its bodies (lossy levels are capped then).
type llmInfo struct {
	Endpoint          string
	HasRequestFilters bool
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

// fetchLLMInfo reads an LLM through the host's service API (llms.read):
// the gateway's on an edge, Studio's on Studio's own gateway.
func fetchLLMInfo(ctx plugin_sdk.Context, llmID uint32) (llmInfo, error) {
	if ctx.Services == nil {
		return llmInfo{}, errNoBroker
	}
	base := ctx.Context
	if base == nil {
		base = context.Background()
	}
	c, cancel := context.WithTimeout(base, llmInfoTimeout)
	defer cancel()
	if ctx.Runtime == plugin_sdk.RuntimeStudio {
		st := ctx.Services.Studio()
		if st == nil {
			return llmInfo{}, errNoBroker
		}
		resp, err := st.GetLLM(c, llmID)
		if err != nil {
			return llmInfo{}, err
		}
		r, ok := resp.(*studiopb.GetLLMResponse)
		if !ok {
			return llmInfo{}, errUnexpectedResponse
		}
		return llmInfo{Endpoint: r.GetLlm().GetApiEndpoint(), HasRequestFilters: r.GetLlm().GetHasRequestFilters()}, nil
	}
	gw := ctx.Services.Gateway()
	if gw == nil {
		return llmInfo{}, errNoBroker
	}
	resp, err := gw.GetLLM(c, llmID)
	if err != nil {
		return llmInfo{}, err
	}
	r, ok := resp.(*mgmtpb.GetLLMResponse)
	if !ok {
		return llmInfo{}, errUnexpectedResponse
	}
	return llmInfo{Endpoint: r.GetLlm().GetEndpoint(), HasRequestFilters: r.GetLlm().GetHasRequestFilters()}, nil
}
