package main

import (
	"strconv"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// Response headers. Values are counts, an outcome constant and a short
// policy hash: never the policy name or anything from the request.
const (
	HeaderOutcome     = "X-Tyk-Compression"
	HeaderTokensSaved = "X-Tyk-Compression-Tokens-Saved"
	HeaderPolicy      = "X-Tyk-Compression-Policy"
)

// recordOutcome maps what post_auth did onto the outcome a record and the
// X-Tyk-Compression header carry.
func recordOutcome(rec pendingRecord) string {
	switch rec.Outcome {
	case string(compress.OutcomeCompressed):
		if rec.Forwarded {
			return records.OutcomeApplied
		}
		if rec.Mode == config.ModeAudit {
			return records.OutcomeAudit
		}
		return records.OutcomeSkipped
	case string(compress.OutcomeRejected):
		return records.OutcomeRejected
	case OutcomeBypassed:
		return records.OutcomeBypassed
	case OutcomeFailedOpen:
		return records.OutcomeFailedOpen
	case OutcomeFailedClosed:
		return records.OutcomeFailedClosed
	}
	return records.OutcomeSkipped
}

// OnBeforeWriteHeaders implements plugin_sdk.ResponseHandler. It labels the
// response with what post_auth did. The host replaces the whole header set
// with what this returns, so it returns every header it was given plus its
// own. It runs on buffered and streaming responses; the record stays for
// the body or stream-complete hook.
func (p *PromptCompressorPlugin) OnBeforeWriteHeaders(ctx plugin_sdk.Context, req *pb.HeadersRequest) (resp *pb.HeadersResponse, err error) {
	unchanged := &pb.HeadersResponse{Modified: false}
	defer func() {
		if r := recover(); r != nil {
			p.logger().Error("Labelling the response failed", "request_id", ctx.RequestID, "panic_type", panicType(r))
			resp, err = unchanged, nil
		}
	}()
	rec, ok := p.pending.peek(ctx.RequestID)
	if !ok {
		return unchanged, nil
	}
	headers := make(map[string]string, len(req.GetHeaders())+3)
	for k, v := range req.GetHeaders() {
		headers[k] = v
	}
	headers[HeaderOutcome] = recordOutcome(rec)
	if saved := rec.Report.TokensSaved(); saved > 0 {
		headers[HeaderTokensSaved] = strconv.Itoa(saved)
	}
	if len(rec.PolicyHash) >= 12 {
		headers[HeaderPolicy] = rec.PolicyHash[:12]
	}
	return &pb.HeadersResponse{Modified: true, Headers: headers}, nil
}

// OnBeforeWrite implements plugin_sdk.ResponseHandler: it reads the usage
// of a buffered response and completes the request's record. It never
// changes the response.
func (p *PromptCompressorPlugin) OnBeforeWrite(ctx plugin_sdk.Context, req *pb.ResponseWriteRequest) (resp *pb.ResponseWriteResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			p.logger().Error("Recording the response failed", "request_id", ctx.RequestID, "panic_type", panicType(r))
		}
		resp, err = &pb.ResponseWriteResponse{Modified: false}, nil
	}()
	if req.GetIsStreamChunk() {
		return nil, nil
	}
	if rec, ok := p.pending.take(ctx.RequestID); ok {
		u, found := parseUsage(req.GetBody())
		p.complete(rec, u, found, false, parseCalledTools(req.GetBody()))
	}
	return nil, nil
}

// OnStreamComplete implements plugin_sdk.StreamCompleteHandler: it reads the
// usage of a finished stream and completes the request's record. An OpenAI
// stream carries usage only with stream_options.include_usage; without it
// the record says usage is missing.
func (p *PromptCompressorPlugin) OnStreamComplete(ctx plugin_sdk.Context, req *pb.StreamCompleteRequest) (resp *pb.StreamCompleteResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			p.logger().Error("Recording the stream failed", "request_id", ctx.RequestID, "panic_type", panicType(r))
		}
		resp, err = &pb.StreamCompleteResponse{Handled: false}, nil
	}()
	if rec, ok := p.pending.take(ctx.RequestID); ok {
		u, found := parseStreamUsage(req.GetAccumulatedResponse())
		p.complete(rec, u, found, true, parseStreamCalledTools(req.GetAccumulatedResponse()))
	}
	return nil, nil
}

// complete hands a finished request to the recorder. called are the tools
// the response called.
func (p *PromptCompressorPlugin) complete(rec pendingRecord, u usage, found, streaming bool, called []string) {
	rep := rec.Report
	r := records.Request{
		At: rec.Created, AppID: rec.AppID, LLMID: rec.LLMID, Vendor: rec.Vendor, Model: rep.Model,
		PolicyName: rec.PolicyName, PolicyHash: rec.PolicyHash, Mode: string(rec.Mode), Level: rec.Level,
		Outcome: recordOutcome(rec), Reason: rep.Reason,
		UnitsSeen: rep.UnitsSeen, UnitsSelected: rep.UnitsSelected, UnitsCompressed: rep.UnitsCompressed,
		BytesBefore: rep.BytesBefore, BytesAfter: rep.BytesAfter,
		TokensBefore: rep.TokensBefore, TokensAfter: rep.TokensAfter,
		CompressMicros: rec.CompressMicros,
		UsageMissing:   !found, Streaming: streaming,
	}
	if found {
		r.InputTokens, r.CacheReadTokens, r.CacheWriteTokens = u.InputTokens, u.CacheReadTokens, u.CacheWriteTokens
	}
	if t := rec.Tools; len(t.Tools) > 0 {
		r.ToolDefs, r.ToolDefTokens, r.ToolListHash = len(t.Tools), t.Tokens, t.Hash
		r.OfferedTools = make([]records.OfferedTool, len(t.Tools))
		for i, o := range t.Tools {
			r.OfferedTools[i] = records.OfferedTool{Name: o.Name, Tokens: o.Tokens}
		}
	}
	r.CalledTools = offeredOnly(called, rec.Tools)
	if len(rep.Strategies) > 0 {
		r.Strategies = make(map[string]records.StrategyStats, len(rep.Strategies))
		for name, s := range rep.Strategies {
			r.Strategies[name] = records.StrategyStats{Leaves: int64(s.Leaves), BytesSaved: int64(s.BytesSaved)}
		}
	}
	p.recorder().Add(r)
}
