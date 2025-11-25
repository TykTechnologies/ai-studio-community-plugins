// Package main implements the LLM Firewall plugin for phrase-based prompt filtering.
// This plugin detects disallowed phrases in incoming prompts and blocks requests
// that contain policy violations.
package main

import (
	_ "embed"
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
	PluginVersion = "1.0.0"
)

// FirewallConfig represents the plugin configuration
type FirewallConfig struct {
	HookPhase     string         `json:"hook_phase"`
	BlockMessage  string         `json:"block_message"`
	CaseSensitive bool           `json:"case_sensitive"`
	Rules         []FirewallRule `json:"rules"`
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
	if err != nil {
		// Fail open - allow request if we can't parse it
		return &pb.PluginResponse{Modified: false}, nil
	}

	if len(textContents) == 0 {
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

// blockResponse creates a blocking response
func (p *LLMFirewallPlugin) blockResponse() *pb.PluginResponse {
	errorBody := fmt.Sprintf(`{"error":{"message":"%s","type":"content_policy_violation"}}`, p.config.BlockMessage)
	return &pb.PluginResponse{
		Block:      true,
		StatusCode: 403,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(errorBody),
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

// extractTextContent extracts all text content from a request body based on vendor
func (p *LLMFirewallPlugin) extractTextContent(vendor string, body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}

	switch strings.ToLower(vendor) {
	case "openai", "ollama":
		return p.extractOpenAIContent(body)
	case "anthropic":
		return p.extractAnthropicContent(body)
	case "google_ai", "vertex":
		return p.extractGoogleAIContent(body)
	default:
		// Fallback: try OpenAI format (most common)
		return p.extractOpenAIContent(body)
	}
}

// extractOpenAIContent extracts text from OpenAI-format requests
func (p *LLMFirewallPlugin) extractOpenAIContent(body []byte) ([]string, error) {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid OpenAI request: %w", err)
	}

	var texts []string
	for _, msg := range req.Messages {
		text := p.extractContentString(msg.Content)
		if text != "" {
			texts = append(texts, text)
		}
	}

	return texts, nil
}

// extractAnthropicContent extracts text from Anthropic-format requests
func (p *LLMFirewallPlugin) extractAnthropicContent(body []byte) ([]string, error) {
	var req struct {
		System   any `json:"system"`
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid Anthropic request: %w", err)
	}

	var texts []string

	// Extract system prompt if present
	if req.System != nil {
		text := p.extractContentString(req.System)
		if text != "" {
			texts = append(texts, text)
		}
	}

	// Extract messages
	for _, msg := range req.Messages {
		text := p.extractContentString(msg.Content)
		if text != "" {
			texts = append(texts, text)
		}
	}

	return texts, nil
}

// extractGoogleAIContent extracts text from Google AI/Vertex-format requests
func (p *LLMFirewallPlugin) extractGoogleAIContent(body []byte) ([]string, error) {
	var req struct {
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid Google AI request: %w", err)
	}

	var texts []string

	// Extract system instruction
	for _, part := range req.SystemInstruction.Parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}

	// Extract contents
	for _, content := range req.Contents {
		for _, part := range content.Parts {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
	}

	return texts, nil
}

// extractContentString extracts string content from various content formats
func (p *LLMFirewallPlugin) extractContentString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		// Handle content arrays (e.g., Anthropic multi-part content)
		var texts []string
		for _, item := range v {
			if itemMap, ok := item.(map[string]any); ok {
				// Check for text type content blocks
				if blockType, ok := itemMap["type"].(string); ok && blockType == "text" {
					if text, ok := itemMap["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, " ")
	default:
		return ""
	}
}

func main() {
	plugin := NewLLMFirewallPlugin()
	plugin_sdk.Serve(plugin)
}
