// Package main implements the LLM Firewall plugin for phrase-based prompt filtering.
// This plugin detects disallowed phrases in incoming prompts and blocks requests
// that contain policy violations.
package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

//go:embed manifest.json
var manifestBytes []byte

//go:embed config.schema.json
var configSchemaBytes []byte

const (
	PluginName    = "llm-firewall"
	PluginVersion = "1.1.1"
)

// FirewallConfig represents the plugin configuration
type FirewallConfig struct {
	HookPhase     string `json:"hook_phase"`
	BlockMessage  string `json:"block_message"`
	CaseSensitive bool   `json:"case_sensitive"`
	// FailClosed blocks a request whose content cannot be inspected (body
	// not parseable, or no text found) when a rule applies to its model.
	// Off by default: such requests pass through.
	FailClosed bool           `json:"fail_closed"`
	Rules      []FirewallRule `json:"rules"`
}

// FirewallRule represents a model-grouped set of blocked phrases
type FirewallRule struct {
	ModelPattern string        `json:"model_pattern"`
	Phrases      []BlockPhrase `json:"phrases"`
	Enabled      bool          `json:"enabled"`
}

// BlockPhrase represents a single phrase or pattern to block
type BlockPhrase struct {
	Pattern     string `json:"pattern"`
	IsRegex     bool   `json:"is_regex"`
	Description string `json:"description,omitempty"`
}

// CompiledRule holds pre-compiled patterns for runtime performance
type CompiledRule struct {
	ModelPattern   *regexp.Regexp
	OriginalGlob   string
	PhrasePatterns []*CompiledPhrase
	Enabled        bool
}

// CompiledPhrase holds a pre-compiled phrase pattern
type CompiledPhrase struct {
	Pattern     *regexp.Regexp
	Original    string
	Description string
}

// LLMFirewallPlugin implements phrase-based prompt filtering
type LLMFirewallPlugin struct {
	plugin_sdk.BasePlugin
	config        FirewallConfig
	compiledRules []*CompiledRule
}

// NewLLMFirewallPlugin creates a new firewall plugin instance
func NewLLMFirewallPlugin() *LLMFirewallPlugin {
	return &LLMFirewallPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(PluginName, PluginVersion, "Phrase-based prompt filtering for LLM requests"),
	}
}

// Initialize parses configuration and compiles all patterns
func (p *LLMFirewallPlugin) Initialize(ctx plugin_sdk.Context, config map[string]string) error {
	// Set defaults
	p.config = FirewallConfig{
		HookPhase:     "pre_auth",
		BlockMessage:  "Request blocked by content policy",
		CaseSensitive: false,
		Rules:         []FirewallRule{},
	}

	// Try to parse configuration from various sources
	// 1. Try "config" key (nested JSON string)
	if configJSON, ok := config["config"]; ok && configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &p.config); err != nil {
			log.Printf("%s: Failed to parse 'config' key: %v", PluginName, err)
		}
	}

	// 2. Try "plugin_config" key (microgateway may use this)
	if configJSON, ok := config["plugin_config"]; ok && configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &p.config); err != nil {
			log.Printf("%s: Failed to parse 'plugin_config' key: %v", PluginName, err)
		}
	}

	// 3. Try parsing the "rules" key directly if it exists
	if rulesJSON, ok := config["rules"]; ok && rulesJSON != "" {
		var rules []FirewallRule
		if err := json.Unmarshal([]byte(rulesJSON), &rules); err != nil {
			log.Printf("%s: Failed to parse 'rules' key: %v", PluginName, err)
		} else {
			p.config.Rules = rules
		}
	}

	// 4. Check for individual config keys (for simpler configurations)
	if hookPhase, ok := config["hook_phase"]; ok && hookPhase != "" {
		p.config.HookPhase = hookPhase
	}
	if blockMessage, ok := config["block_message"]; ok && blockMessage != "" {
		p.config.BlockMessage = blockMessage
	}
	if caseSensitive, ok := config["case_sensitive"]; ok {
		p.config.CaseSensitive = caseSensitive == "true"
	}
	if failClosed, ok := config["fail_closed"]; ok {
		p.config.FailClosed = failClosed == "true"
	}

	// Compile all rules and patterns
	if err := p.compileRules(); err != nil {
		return fmt.Errorf("failed to compile rules: %w", err)
	}

	log.Printf("%s: Initialized with %d rules", PluginName, len(p.compiledRules))
	return nil
}

// Shutdown cleans up plugin resources
func (p *LLMFirewallPlugin) Shutdown(ctx plugin_sdk.Context) error {
	return nil
}

// GetManifest returns the plugin manifest
func (p *LLMFirewallPlugin) GetManifest() ([]byte, error) {
	return manifestBytes, nil
}

// GetConfigSchema returns the JSON schema for configuration
func (p *LLMFirewallPlugin) GetConfigSchema() ([]byte, error) {
	return configSchemaBytes, nil
}

// HandlePreAuth processes requests before authentication
func (p *LLMFirewallPlugin) HandlePreAuth(ctx plugin_sdk.Context, req *pb.PluginRequest) (*pb.PluginResponse, error) {
	return p.checkRequest(ctx, req)
}

// HandlePostAuth processes requests after authentication
func (p *LLMFirewallPlugin) HandlePostAuth(ctx plugin_sdk.Context, req *pb.EnrichedRequest) (*pb.PluginResponse, error) {
	return p.checkRequest(ctx, req.Request)
}

// checkRequest performs the actual firewall check
func (p *LLMFirewallPlugin) checkRequest(ctx plugin_sdk.Context, req *pb.PluginRequest) (*pb.PluginResponse, error) {
	// Only check POST requests (LLM completions)
	if req.Method != "POST" {
		return &pb.PluginResponse{Modified: false}, nil
	}

	// Extract vendor from context
	vendor := ""
	if req.Context != nil {
		vendor = req.Context.Vendor
	}

	// Extract model name from request body (more accurate than LlmSlug which is the gateway slug)
	// For Google AI/Vertex, also check the URL path since model is often there
	modelName := p.extractModelFromBody(req.Body)
	if modelName == "" {
		modelName = p.extractModelFromPath(vendor, req.Path)
	}
	// Fallback to LlmSlug if model not found in body or path
	if modelName == "" && req.Context != nil {
		modelName = req.Context.LlmSlug
	}

	textContents, err := p.extractTextContent(vendor, req.Body)
	if err != nil || len(textContents) == 0 {
		// The content cannot be inspected. By default it passes (fail
		// open); with fail_closed it is blocked if any rule covers the model.
		if p.config.FailClosed && p.anyRuleApplies(modelName) {
			reason := "no text content found"
			if err != nil {
				reason = "body not parseable"
			}
			ctx.Services.Logger().Warn("Request blocked by firewall: content cannot be inspected (fail_closed)",
				"model", modelName,
				"reason", reason,
				"request_id", ctx.RequestID,
			)
			return p.blockResponse(), nil
		}
		return &pb.PluginResponse{Modified: false}, nil
	}

	// Check each applicable rule
	for _, rule := range p.compiledRules {
		if !rule.Enabled {
			continue
		}

		// Check if this rule applies to the current model
		if !p.modelMatches(rule, modelName) {
			continue
		}

		// Check each text content against each phrase pattern
		for _, content := range textContents {
			for _, phrase := range rule.PhrasePatterns {
				if phrase.Pattern.MatchString(content) {
					// Block the request - log via structured logger (privacy-preserving)
					ctx.Services.Logger().Warn("Request blocked by firewall",
						"model", modelName,
						"matched_pattern", phrase.Original,
						"description", phrase.Description,
						"request_id", ctx.RequestID,
					)
					return p.blockResponse(), nil
				}
			}
		}
	}

	// No violations found
	return &pb.PluginResponse{Modified: false}, nil
}

// anyRuleApplies reports whether an enabled rule covers modelName.
func (p *LLMFirewallPlugin) anyRuleApplies(modelName string) bool {
	for _, rule := range p.compiledRules {
		if rule.Enabled && p.modelMatches(rule, modelName) {
			return true
		}
	}
	return false
}

// blockErrorBody is the OpenAI-style error returned for a blocked request.
type blockErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// blockResponse creates a blocking response. The body is built with
// json.Marshal, so any block_message (quotes, backslashes, newlines) gives
// valid JSON.
func (p *LLMFirewallPlugin) blockResponse() *pb.PluginResponse {
	var body blockErrorBody
	body.Error.Message = p.config.BlockMessage
	body.Error.Type = "content_policy_violation"
	errorBody, _ := json.Marshal(body) // cannot fail for strings
	return &pb.PluginResponse{
		Block:      true,
		StatusCode: 403,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       errorBody,
	}
}

// modelMatches checks if a model name matches a rule's pattern
func (p *LLMFirewallPlugin) modelMatches(rule *CompiledRule, modelName string) bool {
	if rule.ModelPattern == nil {
		return false
	}
	// Empty model name matches wildcard patterns only
	if modelName == "" {
		return rule.OriginalGlob == "*"
	}
	return rule.ModelPattern.MatchString(modelName)
}

// compileRules pre-compiles all patterns for runtime performance
func (p *LLMFirewallPlugin) compileRules() error {
	p.compiledRules = make([]*CompiledRule, 0, len(p.config.Rules))

	for i, rule := range p.config.Rules {
		compiled := &CompiledRule{
			OriginalGlob:   rule.ModelPattern,
			Enabled:        rule.Enabled,
			PhrasePatterns: make([]*CompiledPhrase, 0, len(rule.Phrases)),
		}

		// Compile model pattern (glob to regex)
		modelRegex, err := p.globToRegex(rule.ModelPattern)
		if err != nil {
			log.Printf("%s: Invalid model pattern in rule %d: %v (skipping)", PluginName, i, err)
			continue
		}
		compiled.ModelPattern = modelRegex

		// Compile phrase patterns
		for j, phrase := range rule.Phrases {
			var pattern *regexp.Regexp
			var err error

			if phrase.IsRegex {
				// Use pattern as-is for regex
				if p.config.CaseSensitive {
					pattern, err = regexp.Compile(phrase.Pattern)
				} else {
					pattern, err = regexp.Compile("(?i)" + phrase.Pattern)
				}
			} else {
				// Escape literal string and wrap in regex
				escaped := regexp.QuoteMeta(phrase.Pattern)
				if p.config.CaseSensitive {
					pattern, err = regexp.Compile(escaped)
				} else {
					pattern, err = regexp.Compile("(?i)" + escaped)
				}
			}

			if err != nil {
				log.Printf("%s: Invalid phrase pattern in rule %d, phrase %d: %v (skipping)", PluginName, i, j, err)
				continue
			}

			compiled.PhrasePatterns = append(compiled.PhrasePatterns, &CompiledPhrase{
				Pattern:     pattern,
				Original:    phrase.Pattern,
				Description: phrase.Description,
			})
		}

		// Only add rule if it has at least one valid phrase pattern
		if len(compiled.PhrasePatterns) > 0 {
			p.compiledRules = append(p.compiledRules, compiled)
		}
	}

	return nil
}

// globToRegex converts a glob pattern to a regex
func (p *LLMFirewallPlugin) globToRegex(glob string) (*regexp.Regexp, error) {
	if glob == "" {
		glob = "*"
	}

	// Escape all regex special characters except * and ?
	var result strings.Builder
	result.WriteString("^")

	for _, ch := range glob {
		switch ch {
		case '*':
			result.WriteString(".*")
		case '?':
			result.WriteString(".")
		case '.', '+', '^', '$', '(', ')', '[', ']', '{', '}', '|', '\\':
			result.WriteString("\\")
			result.WriteRune(ch)
		default:
			result.WriteRune(ch)
		}
	}

	result.WriteString("$")
	return regexp.Compile(result.String())
}

// extractModelFromBody extracts the model name from the request body
func (p *LLMFirewallPlugin) extractModelFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	// Generic struct to extract model field (works for OpenAI, Anthropic, Ollama)
	var req struct {
		Model string `json:"model"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}

	return req.Model
}

// extractModelFromPath extracts the model name from the URL path (for Google AI/Vertex)
// Google AI format: /v1/models/{model}:generateContent or /v1beta/models/{model}:generateContent
// Vertex format: /v1/projects/{project}/locations/{location}/publishers/google/models/{model}:generateContent
func (p *LLMFirewallPlugin) extractModelFromPath(vendor string, path string) string {
	if path == "" {
		return ""
	}

	// Only process for Google AI and Vertex vendors
	vendorLower := strings.ToLower(vendor)
	if vendorLower != "google_ai" && vendorLower != "vertex" {
		return ""
	}

	// Look for /models/{model}: pattern
	modelsIdx := strings.Index(path, "/models/")
	if modelsIdx == -1 {
		return ""
	}

	// Extract everything after /models/
	modelPart := path[modelsIdx+8:] // len("/models/") = 8

	// Find the end of the model name (before : or / or end of string)
	endIdx := len(modelPart)
	if colonIdx := strings.Index(modelPart, ":"); colonIdx != -1 && colonIdx < endIdx {
		endIdx = colonIdx
	}
	if slashIdx := strings.Index(modelPart, "/"); slashIdx != -1 && slashIdx < endIdx {
		endIdx = slashIdx
	}

	if endIdx > 0 {
		return modelPart[:endIdx]
	}

	return ""
}

// contentContainers are the request fields that carry text the model reads,
// across vendors and API shapes: chat messages (OpenAI, Ollama, Anthropic),
// system prompts (Anthropic system, Google systemInstruction and the
// snake_case system_instruction its REST mapping also accepts), Google
// contents, the OpenAI Responses API (input, instructions), legacy
// completions (prompt), and tool definitions and settings, whose
// descriptions the model also sees.
var contentContainers = []string{
	"messages", "system", "systemInstruction", "system_instruction",
	"contents", "input", "instructions", "prompt",
	"tools", "functions", "tool_config", "toolConfig",
}

// mediaKeys hold inline media; their content is not text.
var mediaKeys = map[string]bool{
	"image_url": true, "inline_data": true, "inlineData": true,
	"file_data": true, "fileData": true, "input_audio": true,
}

// structuralKeys are message and block metadata (roles, block types, IDs,
// MIME types). They are skipped so they neither count as text for
// fail_closed nor slow matching; inside a payload they are ordinary data.
var structuralKeys = map[string]bool{
	"role": true, "type": true, "id": true, "tool_call_id": true,
	"tool_use_id": true, "call_id": true, "media_type": true,
	"mime_type": true, "mimeType": true, "detail": true, "cache_control": true,
}

// payloadKeys hold caller-chosen data the model reads (tool call input and
// arguments, function call args and responses, tool parameter schemas).
// Everything under them is inspected, with no keys skipped.
var payloadKeys = map[string]bool{
	"input": true, "arguments": true, "args": true, "response": true,
	"output": true, "parameters": true,
}

// maxDecodedDocument caps how much of a base64 text document is decoded.
const maxDecodedDocument = 1 << 20

// extractTextContent extracts the text a request sends to the model. It reads
// every string under the known content containers, whatever the nesting
// (content blocks of any type, tool results, tool call arguments, documents,
// tool descriptions), rather than an allowlist of shapes, so a phrase cannot
// hide in a field the upstream accepts but a shape-based reader skips. Each
// element of a container array (one message, say) yields one text, its
// strings joined. The vendor does not matter: unused containers are absent.
func (p *LLMFirewallPlugin) extractTextContent(_ string, body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}

	var texts []string
	for _, key := range contentContainers {
		container, ok := req[key]
		if !ok {
			continue
		}
		items, isArray := container.([]any)
		if !isArray {
			items = []any{container}
		}
		for _, item := range items {
			var parts []string
			collectStrings(item, false, &parts)
			if len(parts) > 0 {
				texts = append(texts, strings.Join(parts, " "))
			}
		}
	}
	return texts, nil
}

// collectStrings appends every string leaf under v. Outside a payload it
// skips structural metadata and inline media; a base64 media source has
// only its data skipped, and a text/* one is decoded and inspected.
func collectStrings(v any, inPayload bool, out *[]string) {
	switch val := v.(type) {
	case string:
		if val != "" {
			*out = append(*out, val)
		}
	case []any:
		for _, item := range val {
			collectStrings(item, inPayload, out)
		}
	case map[string]any:
		if !inPayload && isBase64Source(val) {
			if text, ok := decodeTextSource(val); ok {
				*out = append(*out, text)
			}
			return
		}
		for k, item := range val {
			if !inPayload && (mediaKeys[k] || structuralKeys[k]) {
				continue
			}
			collectStrings(item, inPayload || payloadKeys[k], out)
		}
	}
}

// isBase64Source reports whether m is an Anthropic-style media source:
// {"type":"base64","media_type":...,"data":...} and nothing else.
func isBase64Source(m map[string]any) bool {
	if t, _ := m["type"].(string); t != "base64" {
		return false
	}
	for k := range m {
		if k != "type" && k != "media_type" && k != "data" {
			return false // other fields: inspect normally
		}
	}
	_, hasData := m["data"].(string)
	return hasData
}

// decodeTextSource decodes the data of a base64 source whose media type is
// text/* (the model reads it as text), up to maxDecodedDocument bytes.
func decodeTextSource(m map[string]any) (string, bool) {
	mediaType, _ := m["media_type"].(string)
	if !strings.HasPrefix(mediaType, "text/") {
		return "", false
	}
	data, _ := m["data"].(string)
	if len(data) > base64.StdEncoding.EncodedLen(maxDecodedDocument) {
		data = data[:base64.StdEncoding.EncodedLen(maxDecodedDocument)]
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		// Undecodable data is inspected as is
		return data, true
	}
	return string(decoded), true
}

func main() {
	plugin := NewLLMFirewallPlugin()
	plugin_sdk.Serve(plugin)
}
