package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/engine"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	"github.com/TykTechnologies/midsommar/v2/pkg/requestformat"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// BypassHeader set to "off" asks to skip compression for one request, when
// the config allows clients to (allow_client_bypass). No header can choose
// a policy or raise a level.
const BypassHeader = "X-Tyk-Compression"

// lookupWait bounds how long a request waits for a host lookup (an LLM's
// endpoint, an App's metadata) the plugin does not have yet.
const lookupWait = 25 * time.Millisecond

// ReasonAppPolicyUnknown: the request's App metadata, which may opt the App
// out, is not known yet (or the host cannot be asked), so the request is
// forwarded unchanged.
const ReasonAppPolicyUnknown = "app_policy_unknown"

// ReasonAppScopeMissing: as ReasonAppPolicyUnknown, because the host
// refused the App lookup (the plugin's apps.read scope is not approved).
const ReasonAppScopeMissing = "app_scope_missing"

// ReasonClientBypass: the client sent X-Tyk-Compression: off.
const ReasonClientBypass = "client_bypass"

// appScopeMissingStatus is getStatus's message while App lookups are
// refused.
const appScopeMissingStatus = "apps.read not approved: App traffic is forwarded uncompressed"

var (
	errNoBroker           = errors.New("no service broker")
	errUnexpectedResponse = errors.New("unexpected service response")
)

// hookCounters count what the post-auth hook did, for getStatus.
type hookCounters struct {
	seen, compressed, unchanged, skipped, rejected, bypassed, failedOpen, failedClosed atomic.Uint64
}

func (c *hookCounters) count(outcome string) {
	switch outcome {
	case string(compress.OutcomeCompressed):
		c.compressed.Add(1)
	case string(compress.OutcomeUnchanged):
		c.unchanged.Add(1)
	case string(compress.OutcomeSkipped):
		c.skipped.Add(1)
	case string(compress.OutcomeRejected):
		c.rejected.Add(1)
	case OutcomeBypassed:
		c.bypassed.Add(1)
	case OutcomeFailedOpen:
		c.failedOpen.Add(1)
	case OutcomeFailedClosed:
		c.failedClosed.Add(1)
	}
}

func (c *hookCounters) snapshot() map[string]uint64 {
	return map[string]uint64{
		"seen": c.seen.Load(), "compressed": c.compressed.Load(), "unchanged": c.unchanged.Load(),
		"skipped": c.skipped.Load(), "rejected": c.rejected.Load(), "bypassed": c.bypassed.Load(),
		"failed_open": c.failedOpen.Load(), "failed_closed": c.failedClosed.Load(),
	}
}

// HandlePostAuth implements plugin_sdk.PostAuthHandler: it compresses the
// request body under the resolved policy. In audit mode it records what it
// would have saved and forwards the original; in enforce mode it forwards
// the compressed body.
//
// It never fails a request. It returns no error (the host turns any error
// into a 500), recovers panics, and passes the request through unchanged on
// anything it cannot do. Headers stay nil: the host Sets each returned
// header and never removes one, so echoing them back would collapse
// multi-value headers. Headers and bodies are never logged: the request
// carries the caller's credentials.
func (p *PromptCompressorPlugin) HandlePostAuth(ctx plugin_sdk.Context, req *pb.EnrichedRequest) (resp *pb.PluginResponse, err error) {
	passThrough := &pb.PluginResponse{Modified: false}
	defer func() {
		if r := recover(); r != nil {
			p.logger().Error("Compression failed; the request continues unchanged", "request_id", ctx.RequestID, "panic_type", panicType(r))
			p.counters.count(OutcomeFailedOpen)
			p.pending.put(pendingRecord{RequestID: ctx.RequestID, LLMID: ctx.LLMID, AppID: ctx.AppID, Vendor: ctx.Vendor, Outcome: OutcomeFailedOpen})
			resp, err = passThrough, nil
		}
	}()

	cfg := p.state().config
	r := req.GetRequest()
	if cfg.Mode == config.ModeOff || r == nil || len(r.GetBody()) == 0 {
		return passThrough, nil
	}
	p.counters.seen.Add(1)
	// Every parse of the body holds its share of the in-flight budget
	// (max_in_flight_bytes) until the hook returns. A body larger than
	// max_body_bytes, or one the budget has no room for, is never parsed.
	size := len(r.GetBody())
	held := size <= cfg.MaxBodyBytes && p.budget.acquire(size, cfg.MaxInFlightBytes)
	if held {
		defer p.budget.release(size)
	}
	// The tool definitions are measured whatever happens to the body (the
	// vendor's format is enough to read them). The body is parsed once, and
	// the compressor reads the same parse.
	var tools toolsInfo
	var doc *segments.Doc
	if held {
		if d, err := parseBody(r.GetBody()); err == nil {
			doc = d
			tools = offeredTools(requestformat.For(ctx.Vendor, r.GetMethod(), r.GetPath(), ""), d)
		}
	}

	if cfg.AllowClientBypass && strings.EqualFold(strings.TrimSpace(headerValue(r.GetHeaders(), BypassHeader)), "off") {
		p.counters.count(OutcomeBypassed)
		p.pending.put(pendingRecord{RequestID: ctx.RequestID, LLMID: ctx.LLMID, AppID: ctx.AppID, Vendor: ctx.Vendor, Mode: cfg.Mode, Outcome: OutcomeBypassed,
			Report: compress.Report{Reason: ReasonClientBypass}, Tools: tools})
		return passThrough, nil
	}

	res, unknown := p.resolvePolicy(ctx)
	if unknown != "" {
		rec := pendingRecord{
			RequestID: ctx.RequestID, LLMID: ctx.LLMID, AppID: ctx.AppID, Vendor: ctx.Vendor, Mode: cfg.Mode,
			Outcome: string(compress.OutcomeSkipped),
			Report:  compress.Report{Outcome: compress.OutcomeSkipped, Reason: unknown},
			Tools:   tools,
		}
		p.counters.count(rec.Outcome)
		p.pending.put(rec)
		return passThrough, nil
	}
	rec := pendingRecord{
		RequestID: ctx.RequestID, LLMID: ctx.LLMID, AppID: ctx.AppID, Vendor: ctx.Vendor,
		Mode: cfg.Mode, PolicyName: res.Name, PolicyHash: res.Hash, Tools: tools,
	}
	if res.Policy != nil {
		rec.Level = res.Policy.Level
	}
	if res.Off() {
		rec.Outcome = string(compress.OutcomeSkipped)
		rec.Report = compress.Report{Outcome: compress.OutcomeSkipped, Reason: compress.ReasonOff, PolicyHash: res.Hash}
		p.counters.count(rec.Outcome)
		p.pending.put(rec)
		return passThrough, nil
	}
	// Over budget: forwarded unchanged rather than queued, so load never
	// holds a request back (Compress itself skips an oversized body).
	if !held && size <= cfg.MaxBodyBytes {
		rec.Outcome = string(compress.OutcomeSkipped)
		rec.Report = compress.Report{Outcome: compress.OutcomeSkipped, Reason: ReasonOverBudget, PolicyHash: res.Hash, Level: rec.Level}
		p.counters.count(rec.Outcome)
		p.pending.put(rec)
		return passThrough, nil
	}

	llm, llmKnown := p.llms.getWait(ctx.LLMID, lookupWait)
	format := requestformat.For(ctx.Vendor, r.GetMethod(), r.GetPath(), llm.Endpoint)
	// Request filters screen the body the plugin forwards, so lossy levels
	// stay off where they may run, and where the plugin cannot tell.
	capLossy := !cfg.LossyWithRequestFilters && (!llmKnown || llm.HasRequestFilters)
	// failure_mode closed refuses a request only when it would have been
	// sent compressed; audit mode never refuses one.
	failClosed := cfg.FailureMode == config.FailClosed && cfg.Mode == config.ModeEnforce
	start := time.Now()
	out, rep := p.compressFn(compress.Input{
		Format:       format,
		Body:         r.GetBody(),
		Doc:          doc,
		Policy:       res.Policy,
		MaxBodyBytes: cfg.MaxBodyBytes,
		CapLossy:     capLossy,
		Engine:       p.engineFor(ctx, cfg, res, failClosed, llm.StoresBodies),
	})
	rec.CompressMicros = time.Since(start).Microseconds()
	rec.Outcome, rec.Report = string(rep.Outcome), rep
	if rep.Outcome == compress.OutcomeEngineFailed {
		rec.Outcome = OutcomeFailedClosed
		p.counters.count(rec.Outcome)
		// No upstream response follows, so the record completes now.
		p.complete(rec, usage{}, false, false, nil)
		return &pb.PluginResponse{Block: true, StatusCode: http.StatusBadGateway,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"error":{"message":"request compression is unavailable","type":"compression_engine_unavailable"}}`)}, nil
	}
	p.counters.count(rec.Outcome)

	if cfg.Mode == config.ModeEnforce && rep.Outcome == compress.OutcomeCompressed && len(out) > 0 {
		rec.Forwarded = true
		p.pending.put(rec)
		return &pb.PluginResponse{Modified: true, Body: out}, nil
	}
	p.pending.put(rec)
	return passThrough, nil
}

// engineFor returns the compressor's engine hook for a policy that names
// an engine; nil otherwise. Clients, and so their memos, live across
// requests, one per engine setting. storeText is the LLM's
// stores_request_bodies (false while unknown).
func (p *PromptCompressorPlugin) engineFor(ctx plugin_sdk.Context, cfg *config.Config, res config.Resolution, failClosed, storeText bool) compress.EngineFunc {
	if res.Policy == nil || res.Policy.Engine == nil {
		return nil
	}
	ref := *res.Policy.Engine
	creds := cfg.Engines[ref.Name]
	key := engine.Key(ref, creds)
	c, ok := p.engines.Load(key)
	if !ok {
		c, _ = p.engines.LoadOrStore(key, engine.New(ref, creds))
	}
	mode := config.FailOpen
	if failClosed {
		mode = config.FailClosed
	}
	var kv engine.KV
	if ctx.Services != nil {
		if s := ctx.Services.KV(); s != nil {
			kv = s
		}
	}
	base := ctx.Context
	if base == nil {
		base = context.Background()
	}
	return c.(*engine.Client).Run(base, kv, p.logger().Warn, res.Hash, mode, storeText)
}

// headerValue looks a header up whatever its case.
func headerValue(headers map[string]string, name string) string {
	if v, ok := headers[name]; ok {
		return v
	}
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

// panicType names a recovered value's type without its contents, which
// may hold request text.
func panicType(r interface{}) string {
	switch r.(type) {
	case error:
		return "error"
	case string:
		return "string"
	}
	return "other"
}
