package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/pkg/requestformat"
)

// The preview tool (Studio UI) runs the compressor over a body an admin
// pastes or uploads, with a saved policy or one edited inline, and shows
// the output and every decision. It calls the same compressor with the
// same inputs as the edge's post_auth hook, so the output is the bytes an
// edge would forward. Inputs are never stored or logged, no error repeats
// them, and no engine is called from Studio.

// Preview limits. The payload cap applies before anything is parsed.
const (
	maxPreviewBytes   = 2 << 20
	maxPreviewPayload = maxPreviewBytes + 64<<10
	maxToolNameLen    = 128
	previewToolCallID = "call_preview"
)

// Synthetic corpus samples (testdata/corpus), for trying the tool without
// real data.
//
//go:embed samples/*.json
var previewSamples embed.FS

type previewSample struct {
	Name   string `json:"name"`
	Vendor string `json:"vendor"`
	Path   string `json:"path"`
	About  string `json:"about"`
}

var sampleIndex = []previewSample{
	{Name: "openai_pytest_developer", Vendor: "openai", Path: "/v1/chat/completions", About: "OpenAI chat: a failing pytest run"},
	{Name: "anthropic_cargo_string", Vendor: "anthropic", Path: "/v1/messages", About: "Anthropic: a tree listing and a failing cargo test"},
	{Name: "anthropic_cache_control", Vendor: "anthropic", Path: "/v1/messages", About: "Anthropic with cache breakpoints: a git diff and a Gradle build"},
	{Name: "responses_codex_shell", Vendor: "openai", Path: "/v1/responses", About: "OpenAI Responses (Codex): go test and cargo output"},
	{Name: "bedrock_converse_terraform_pytest", Vendor: "bedrock", Path: "/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse", About: "Bedrock Converse: terraform plan and pytest"},
	{Name: "google_parallel_k8s_tf", Vendor: "google_ai", Path: "/v1beta/models/gemini-2.5-pro:generateContent", About: "Gemini: kubectl and terraform plan"},
}

type previewRequest struct {
	// Mode "tool_output" wraps Text as one tool result in a minimal
	// request in Format; "body" compresses Body as sent to Vendor at Path;
	// "sample" uses an embedded sample.
	Mode     string          `json:"mode"`
	Text     string          `json:"text"`
	ToolName string          `json:"tool_name"`
	Format   string          `json:"format"`
	Body     json.RawMessage `json:"body"`
	Vendor   string          `json:"vendor"`
	Path     string          `json:"path"`
	Sample   string          `json:"sample"`
	// PolicyName picks a saved policy; Policy is one edited inline,
	// checked exactly as a saved one is.
	PolicyName string          `json:"policy_name"`
	Policy     json.RawMessage `json:"policy"`
	// RequestFilters previews an LLM with request filters: level 3 is
	// held at 2 unless lossy_with_request_filters is set, as on the edge.
	RequestFilters bool `json:"request_filters"`
}

type protectHit struct {
	Pattern string `json:"pattern"`
	Before  int    `json:"before"`
	After   int    `json:"after"`
}

type previewUnit struct {
	compress.UnitTrace
	// Detected are the level-2 reducers whose detector matched a changed
	// leaf, in registry order (the first one runs).
	Detected []string `json:"detected,omitempty"`
	// Protected counts, per protect pattern, the matching lines before and
	// after in the unit's changed leaves.
	Protected []protectHit `json:"protected,omitempty"`
}

type previewResponse struct {
	Format     string          `json:"format"`
	PolicyName string          `json:"policy_name"`
	PolicyHash string          `json:"policy_hash"`
	Level      int             `json:"level"`
	Report     compress.Report `json:"report"`
	Units      []previewUnit   `json:"units"`
	Input      string          `json:"input"`
	Output     string          `json:"output"`
	Changed    bool            `json:"changed"`
	PostCheck  string          `json:"post_check"`
	// EngineNotRun: the policy names an external engine, which the preview
	// never calls; an edge would run it after levels 1–3.
	EngineNotRun bool `json:"engine_not_run,omitempty"`
}

var errPreviewTooLarge = errors.New("the input is larger than 2 MiB; preview a smaller body")

// rpcPreview runs one preview.
func (p *PromptCompressorPlugin) rpcPreview(payload []byte) ([]byte, error) {
	if len(payload) > maxPreviewPayload {
		return nil, errPreviewTooLarge
	}
	var req previewRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, errors.New("preview: the request is not valid JSON")
	}
	format, body, err := previewInput(req)
	if err != nil {
		return nil, err
	}
	if len(body) > maxPreviewBytes {
		return nil, errPreviewTooLarge
	}
	name, policy, err := p.previewPolicy(req)
	if err != nil {
		return nil, err
	}

	cfg := p.state().config
	// No engine is called from Studio
	out, rep := p.compressFn(compress.Input{
		Format:       format,
		Body:         body,
		Policy:       policy,
		MaxBodyBytes: cfg.MaxBodyBytes,
		CapLossy:     req.RequestFilters && !cfg.LossyWithRequestFilters,
		Trace:        true,
	})
	resp := previewResponse{
		Format: format, PolicyName: name, PolicyHash: policy.Hash(), Level: policy.Level,
		Input: string(body), Output: string(out), Changed: string(out) != string(body),
		Units:        make([]previewUnit, 0, len(rep.Units)),
		EngineNotRun: policy.Engine != nil,
	}
	switch rep.Outcome {
	case compress.OutcomeRejected:
		resp.PostCheck = "failed: the rewrite was refused, so the original body would be forwarded"
	case compress.OutcomeCompressed:
		resp.PostCheck = "passed"
	default:
		resp.PostCheck = "not run: nothing was rewritten"
	}
	res := policy.ProtectRegexps()
	for _, u := range rep.Units {
		pu := previewUnit{UnitTrace: u}
		seen := map[string]bool{}
		hits := make([]protectHit, len(res))
		for _, leaf := range u.Leaves {
			for _, d := range compress.Detectors(leaf.Before) {
				if !seen[d] {
					seen[d] = true
					pu.Detected = append(pu.Detected, d)
				}
			}
			for i, re := range res {
				hits[i].Before += countLinesMatching(re, leaf.Before)
				hits[i].After += countLinesMatching(re, leaf.After)
			}
		}
		for i, h := range hits {
			if h.Before > 0 {
				h.Pattern = policy.ProtectPatterns[i]
				pu.Protected = append(pu.Protected, h)
			}
		}
		resp.Units = append(resp.Units, pu)
	}
	rep.Units = nil // sent once, in Units
	resp.Report = rep
	return json.Marshal(resp)
}

func countLinesMatching(re *regexp.Regexp, text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if re.MatchString(line) {
			n++
		}
	}
	return n
}

// previewInput builds the body and its format.
func previewInput(req previewRequest) (string, []byte, error) {
	switch req.Mode {
	case "tool_output":
		if len(req.Text) > maxPreviewBytes {
			return "", nil, errPreviewTooLarge
		}
		if req.Text == "" {
			return "", nil, errors.New("preview: paste a tool output")
		}
		tool := strings.TrimSpace(req.ToolName)
		if tool == "" {
			tool = "run_command"
		}
		if len(tool) > maxToolNameLen {
			return "", nil, fmt.Errorf("preview: the tool name is longer than %d characters", maxToolNameLen)
		}
		body, err := wrapToolOutput(req.Format, tool, req.Text)
		if err != nil {
			return "", nil, err
		}
		return req.Format, body, nil
	case "body", "sample":
		vendor, apiPath, body := req.Vendor, req.Path, []byte(req.Body)
		if req.Mode == "sample" {
			s, raw, err := loadSample(req.Sample)
			if err != nil {
				return "", nil, err
			}
			vendor, apiPath, body = s.Vendor, s.Path, raw
		}
		if len(body) == 0 {
			return "", nil, errors.New("preview: paste or upload a request body")
		}
		if !json.Valid(body) {
			return "", nil, errors.New("preview: the request body is not valid JSON")
		}
		if !strings.HasPrefix(apiPath, "/") || len(apiPath) > 512 {
			return "", nil, errors.New("preview: the path must start with / (for example /v1/messages)")
		}
		// The edge sees the vendor path under /llm/rest/<slug>
		return requestformat.For(vendor, "POST", "/llm/rest/preview"+apiPath, ""), body, nil
	default:
		return "", nil, errors.New(`preview: mode must be "tool_output", "body" or "sample"`)
	}
}

// wrapToolOutput puts text into a minimal request in format as the result
// of one tool call. A later turn follows it, so protect_recent_turns does
// not cap the result at level 1 and the preview shows the policy's level.
func wrapToolOutput(format, tool, text string) ([]byte, error) {
	type obj = map[string]interface{}
	type arr = []interface{}
	var v obj
	switch format {
	case "openai":
		v = obj{"model": "gpt-4o", "messages": arr{
			obj{"role": "user", "content": "Run the tool."},
			obj{"role": "assistant", "content": nil, "tool_calls": arr{
				obj{"id": previewToolCallID, "type": "function", "function": obj{"name": tool, "arguments": "{}"}},
			}},
			obj{"role": "tool", "tool_call_id": previewToolCallID, "content": text},
			obj{"role": "assistant", "content": "Done."},
			obj{"role": "user", "content": "Next."},
		}}
	case "anthropic":
		v = obj{"model": "claude-sonnet-4-5", "max_tokens": 1024, "messages": arr{
			obj{"role": "user", "content": "Run the tool."},
			obj{"role": "assistant", "content": arr{
				obj{"type": "tool_use", "id": previewToolCallID, "name": tool, "input": obj{}},
			}},
			obj{"role": "user", "content": arr{
				obj{"type": "tool_result", "tool_use_id": previewToolCallID, "content": text},
			}},
			obj{"role": "assistant", "content": "Done."},
			obj{"role": "user", "content": "Next."},
		}}
	case requestformat.OpenAIResponses:
		v = obj{"model": "gpt-5-codex", "input": arr{
			obj{"type": "message", "role": "user", "content": "Run the tool."},
			obj{"type": "function_call", "call_id": previewToolCallID, "name": tool, "arguments": "{}"},
			obj{"type": "function_call_output", "call_id": previewToolCallID, "output": text},
			obj{"type": "message", "role": "assistant", "content": arr{obj{"type": "output_text", "text": "Done."}}},
			obj{"type": "message", "role": "user", "content": "Next."},
		}}
	default:
		return nil, fmt.Errorf("preview: a pasted tool output can be wrapped as openai, anthropic or %s; paste a whole request body for other formats", requestformat.OpenAIResponses)
	}
	return json.Marshal(v)
}

func loadSample(name string) (previewSample, []byte, error) {
	for _, s := range sampleIndex {
		if s.Name == name {
			raw, err := previewSamples.ReadFile(path.Join("samples", s.Name+".json"))
			if err != nil {
				return s, nil, errors.New("preview: the sample is missing from the plugin")
			}
			return s, raw, nil
		}
	}
	return previewSample{}, nil, errors.New("preview: unknown sample")
}

// previewPolicy picks the policy: a saved one by name, or one edited
// inline, validated exactly as a saved policy is.
func (p *PromptCompressorPlugin) previewPolicy(req previewRequest) (string, *config.Policy, error) {
	if len(req.Policy) > 0 && string(req.Policy) != "null" {
		raw, err := json.Marshal(map[string]map[string]json.RawMessage{"policies": {"preview": req.Policy}})
		if err != nil {
			return "", nil, errors.New("preview: the policy is not valid JSON")
		}
		cfg, err := config.ParseJSON(raw)
		if err != nil {
			return "", nil, fmt.Errorf("preview: the policy is invalid: %w", err)
		}
		pol, _ := cfg.Policy("preview")
		return "(edited here)", pol, nil
	}
	cfg := p.state().config
	name := req.PolicyName
	if name == "" {
		name = cfg.DefaultPolicy
	}
	pol, ok := cfg.Policy(name)
	if !ok {
		return "", nil, errors.New("preview: no saved policy has that name")
	}
	return name, pol, nil
}

// rpcPreviewOptions answers the preview page: the samples and the saved
// policies (name, level, hash).
func (p *PromptCompressorPlugin) rpcPreviewOptions() ([]byte, error) {
	cfg := p.state().config
	type policyOption struct {
		Name   string         `json:"name"`
		Level  int            `json:"level"`
		Hash   string         `json:"short_hash"`
		Policy *config.Policy `json:"policy"`
	}
	policies := []policyOption{}
	names := []string{config.DefaultPolicyName}
	for n := range cfg.Policies {
		if n != config.DefaultPolicyName {
			names = append(names, n)
		}
	}
	for _, n := range names {
		if pol, ok := cfg.Policy(n); ok {
			policies = append(policies, policyOption{Name: n, Level: pol.Level, Hash: pol.ShortHash(), Policy: pol})
		}
	}
	return json.Marshal(map[string]interface{}{
		"samples":        sampleIndex,
		"policies":       policies,
		"default_policy": cfg.DefaultPolicy,
		"formats":        []string{"openai", "anthropic", requestformat.OpenAIResponses},
		"max_bytes":      maxPreviewBytes,
	})
}
