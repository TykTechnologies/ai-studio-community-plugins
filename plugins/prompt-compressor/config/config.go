// Package config holds the Prompt Compressor's configuration: the settings
// the host passes to Initialize, the named policies and their bindings, and
// the resolution of the policy that applies to one request.
//
// The plugin gets one config for every LLM it is attached to, so per-LLM and
// per-App choices are bindings inside that config, not per-LLM overrides.
//
// The package has no SDK dependency, so the compressor and its tests can use
// it on their own.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"regexp/syntax"
	"sort"
	"strconv"
	"strings"
)

// Mode says what the plugin does with a request.
type Mode string

const (
	// ModeOff does nothing.
	ModeOff Mode = "off"
	// ModeAudit compresses and records the savings, then forwards the
	// original body.
	ModeAudit Mode = "audit"
	// ModeEnforce forwards the compressed body.
	ModeEnforce Mode = "enforce"
)

// FailureMode says what happens when an engine call fails.
type FailureMode string

const (
	// FailOpen forwards the request (levels 0–3 applied) when the engine
	// fails.
	FailOpen FailureMode = "open"
	// FailClosed answers 502 when the engine fails. It applies to engine
	// failures only; every other failure passes the request through.
	FailClosed FailureMode = "closed"
)

// CacheMode says how compression treats the provider's prompt cache.
type CacheMode string

const (
	// CacheDeterministic compresses everything selected. Output is a pure
	// function of the unit text and the policy, so the compressed prefix is
	// the same every turn and the cache keeps hitting.
	CacheDeterministic CacheMode = "deterministic"
	// CacheSkipCached leaves everything up to the last cache breakpoint
	// alone.
	CacheSkipCached CacheMode = "skip_cached"
)

// Roles a policy can select units by.
const (
	RoleTool      = "tool"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// Engine kinds.
const (
	EngineHeadroom   = "headroom"
	EngineLLMLingua2 = "llmlingua2"
)

// PolicyOff is the reserved policy name that turns compression off. It can
// be bound like any policy (a per-App opt-out) and cannot be defined.
const PolicyOff = "off"

// DefaultPolicyName is the policy used when default_policy is not set. When
// no policy of that name is defined, it is the default policy settings.
const DefaultPolicyName = "default"

// Binding key prefixes.
const (
	BindLLM   = "llm:"
	BindLLMID = "llm_id:"
	BindApp   = "app:"
)

// Limits on admin input.
const (
	MaxLevel              = 3
	MaxPolicies           = 64
	MaxBindings           = 10000
	MaxProtectPatterns    = 64
	MaxProtectPatternLen  = 512
	MaxToolOverrides      = 256
	MaxEngineTimeoutMs    = 60000
	DefaultEngineTimeout  = 2000
	DefaultMaxBodyBytes   = 4 << 20
	MaxPolicyNameLen      = 64
	maxRegexpInstructions = 5000
)

// policyNamePattern is what a policy name may look like.
var policyNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

// engineKeyPattern matches the top-level engine credential keys,
// engine_<name>_url and engine_<name>_token.
var engineKeyPattern = regexp.MustCompile(`^engine_([a-z0-9]+(?:_[a-z0-9]+)*?)_(url|token)$`)

// Config is the plugin's whole configuration.
type Config struct {
	Mode                    Mode               `json:"mode"`
	DefaultPolicy           string             `json:"default_policy"`
	Policies                map[string]*Policy `json:"policies"`
	Bindings                map[string]string  `json:"bindings"`
	AllowClientBypass       bool               `json:"allow_client_bypass"`
	FailureMode             FailureMode        `json:"failure_mode"`
	LossyWithRequestFilters bool               `json:"lossy_with_request_filters"`
	// MaxBodyBytes skips bodies larger than this. Limits are by size, never
	// by time, so load never changes the output.
	MaxBodyBytes int `json:"max_body_bytes"`

	// Engines holds the engine credentials from the top-level
	// engine_<name>_url and engine_<name>_token keys, by name. They sit at
	// the top level so $SECRET/ references are resolved for them.
	Engines map[string]EngineCredentials `json:"-"`
}

// EngineCredentials is where an engine is and how to authenticate to it.
type EngineCredentials struct {
	URL   string
	Token string
}

// Policy is one named set of compression settings.
type Policy struct {
	// Level is the loss level, 0–3, cumulative.
	Level int `json:"level"`
	// Roles selects units by role. tool is the default.
	Roles []string `json:"roles"`
	// MinUnitTokens skips units smaller than this.
	MinUnitTokens int `json:"min_unit_tokens"`
	// ProtectRecentTurns keeps the newest turns at level 1 at most.
	ProtectRecentTurns int       `json:"protect_recent_turns"`
	CacheMode          CacheMode `json:"cache_mode"`
	// DedupeReferences replaces a tool output identical to an earlier one
	// in the same request with a reference to it. Off until it passes the
	// level-2 eval gate.
	DedupeReferences bool                     `json:"dedupe_references"`
	ToolOverrides    map[string]*ToolOverride `json:"tool_overrides,omitempty"`
	// ProtectPatterns are regexes whose matches lossy levels keep verbatim.
	ProtectPatterns []string `json:"protect_patterns,omitempty"`
	// MaxUnitTokens: level 3 cuts a longer tool output to its head and tail.
	MaxUnitTokens int `json:"max_unit_tokens"`
	// MaskAfterTurns: level 3 replaces tool results older than this many
	// turns with a placeholder.
	MaskAfterTurns int `json:"mask_after_turns"`
	// MaskStepTokens: level 3 masks in steps of at least this many tokens.
	MaskStepTokens int `json:"mask_step_tokens"`
	// Engine sends selected units to an external engine after levels 1–3.
	Engine *EngineRef `json:"engine,omitempty"`

	compiled []*regexp.Regexp
	hash     string
}

// ToolOverride changes the policy for one tool, by the tool name the parser
// reports.
type ToolOverride struct {
	Exclude bool `json:"exclude,omitempty"`
	// Level, when set, replaces the policy's level for this tool's output.
	Level *int `json:"level,omitempty"`
	// MaxUnitTokens, when set, is this tool's size budget.
	MaxUnitTokens int `json:"max_unit_tokens,omitempty"`
}

// EngineRef names an engine whose credentials are in the top-level config.
type EngineRef struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	TimeoutMs int    `json:"timeout_ms"`
}

// Default returns the configuration of a fresh install: audit mode and the
// default policy (level 1, tool outputs only).
func Default() *Config {
	return &Config{
		Mode:              ModeAudit,
		DefaultPolicy:     DefaultPolicyName,
		Policies:          map[string]*Policy{},
		Bindings:          map[string]string{},
		AllowClientBypass: true,
		FailureMode:       FailOpen,
		MaxBodyBytes:      DefaultMaxBodyBytes,
		Engines:           map[string]EngineCredentials{},
	}
}

// DefaultPolicy returns the default policy settings.
func DefaultPolicy() *Policy {
	return &Policy{
		Level:              1,
		Roles:              []string{RoleTool},
		MinUnitTokens:      300,
		ProtectRecentTurns: 1,
		CacheMode:          CacheDeterministic,
		MaxUnitTokens:      6000,
		MaskAfterTurns:     8,
		MaskStepTokens:     20000,
	}
}

// offPolicy is the policy the reserved name "off" resolves to. Policies
// returned by Config.Policy are shared: treat them as read-only.
var offPolicy = func() *Policy {
	p := DefaultPolicy()
	p.Level = 0
	return p
}()

// Policy returns the named policy. The default policy name falls back to
// the default settings when it is not defined, and "off" is always level 0.
func (c *Config) Policy(name string) (*Policy, bool) {
	if name == PolicyOff {
		return offPolicy, true
	}
	if p, ok := c.Policies[name]; ok {
		return p, true
	}
	if name == DefaultPolicyName {
		return defaultPolicy, true
	}
	return nil, false
}

var defaultPolicy = DefaultPolicy()

// Parse builds a Config from the string map the host passes to Initialize:
// top-level scalars as strings, arrays and objects JSON-encoded. Keys that
// start with "_" are the host's (plugin and broker IDs) and are ignored.
// Unknown keys are an error, so a typo does not silently do nothing.
func Parse(m map[string]string) (*Config, error) {
	c := Default()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var errs []error
	for _, k := range keys {
		v := m[k]
		// "_" keys are the host's; null is how the host passes an unset value
		if strings.HasPrefix(k, "_") || v == "null" {
			continue
		}
		var err error
		switch k {
		case "mode":
			c.Mode = Mode(v)
		case "default_policy":
			c.DefaultPolicy = v
		case "policies":
			c.Policies, err = parsePolicies([]byte(v))
		case "bindings":
			c.Bindings = map[string]string{}
			err = strictUnmarshal([]byte(v), &c.Bindings)
		case "allow_client_bypass":
			c.AllowClientBypass, err = strconv.ParseBool(v)
		case "failure_mode":
			c.FailureMode = FailureMode(v)
		case "lossy_with_request_filters":
			c.LossyWithRequestFilters, err = strconv.ParseBool(v)
		case "max_body_bytes":
			c.MaxBodyBytes, err = parseInt(v)
		default:
			if sub := engineKeyPattern.FindStringSubmatch(k); sub != nil {
				creds := c.Engines[sub[1]]
				if sub[2] == "url" {
					creds.URL = v
				} else {
					creds.Token = v
				}
				c.Engines[sub[1]] = creds
				continue
			}
			err = errors.New("unknown setting")
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", k, err))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if c.DefaultPolicy == "" {
		c.DefaultPolicy = DefaultPolicyName
	}
	if c.Policies == nil {
		c.Policies = map[string]*Policy{}
	}
	if c.Bindings == nil {
		c.Bindings = map[string]string{}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// ParseJSON builds a Config from the JSON object an admin saves, the form
// the Studio UI edits. It flattens the object the way the host does and
// calls Parse, so both paths accept exactly the same input.
func ParseJSON(raw []byte) (*Config, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("config is not a JSON object: %w", err)
	}
	m := make(map[string]string, len(obj))
	for k, v := range obj {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			m[k] = s
			continue
		}
		m[k] = string(bytes.TrimSpace(v))
	}
	return Parse(m)
}

// parseInt reads an integer the host formatted with %v from a JSON number,
// which can be in exponent form (1e+06).
func parseInt(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
		return 0, fmt.Errorf("%q is not an integer", s)
	}
	return int(f), nil
}

func strictUnmarshal(raw []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after JSON value")
	}
	return nil
}

// parsePolicies decodes the policies object. Each policy starts from the
// default settings, so a policy only names what it changes.
func parsePolicies(raw []byte) (map[string]*Policy, error) {
	var rawPolicies map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawPolicies); err != nil {
		return nil, err
	}
	out := make(map[string]*Policy, len(rawPolicies))
	for name, r := range rawPolicies {
		p := DefaultPolicy()
		if err := strictUnmarshal(r, p); err != nil {
			return nil, fmt.Errorf("policy %q: %w", name, err)
		}
		out[name] = p
	}
	return out, nil
}

// Validate checks the whole config, compiles protect patterns and computes
// the policy hashes. Parse calls it; call it again after changing a Config
// by hand.
func (c *Config) Validate() error {
	var errs []error
	add := func(format string, a ...interface{}) { errs = append(errs, fmt.Errorf(format, a...)) }

	switch c.Mode {
	case ModeOff, ModeAudit, ModeEnforce:
	default:
		add("mode: %q is not one of off, audit, enforce", c.Mode)
	}
	switch c.FailureMode {
	case FailOpen, FailClosed:
	default:
		add("failure_mode: %q is not one of open, closed", c.FailureMode)
	}
	if c.MaxBodyBytes <= 0 {
		add("max_body_bytes: must be positive")
	}
	if len(c.Policies) > MaxPolicies {
		add("policies: %d defined, at most %d allowed", len(c.Policies), MaxPolicies)
	}

	names := make([]string, 0, len(c.Policies))
	for name := range c.Policies {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == PolicyOff {
			add("policies: %q is reserved (it turns compression off) and cannot be defined", PolicyOff)
			continue
		}
		if len(name) > MaxPolicyNameLen || !policyNamePattern.MatchString(name) {
			add("policies: name %q must be lower-case letters, digits, '-' or '_', at most %d characters", name, MaxPolicyNameLen)
		}
		p := c.Policies[name]
		if p == nil {
			add("policy %q: is empty", name)
			continue
		}
		if err := p.validate(c); err != nil {
			add("policy %q: %w", name, err)
		}
	}

	if _, ok := c.Policy(c.DefaultPolicy); !ok {
		add("default_policy: no policy named %q", c.DefaultPolicy)
	}

	if len(c.Bindings) > MaxBindings {
		add("bindings: %d defined, at most %d allowed", len(c.Bindings), MaxBindings)
	}
	keys := make([]string, 0, len(c.Bindings))
	for k := range c.Bindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := validateBindingKey(key); err != nil {
			add("bindings: %w", err)
		}
		if _, ok := c.Policy(c.Bindings[key]); !ok {
			add("bindings: %q names unknown policy %q", key, c.Bindings[key])
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	c.sealHashes()
	return nil
}

func validateBindingKey(key string) error {
	switch {
	case strings.HasPrefix(key, BindLLM):
		if strings.TrimPrefix(key, BindLLM) == "" {
			return fmt.Errorf("%q has no LLM slug", key)
		}
	case strings.HasPrefix(key, BindLLMID):
		if !isID(strings.TrimPrefix(key, BindLLMID)) {
			return fmt.Errorf("%q: the LLM ID must be a positive integer", key)
		}
	case strings.HasPrefix(key, BindApp):
		if !isID(strings.TrimPrefix(key, BindApp)) {
			return fmt.Errorf("%q: the App ID must be a positive integer", key)
		}
	default:
		return fmt.Errorf("%q must start with llm:, llm_id: or app:", key)
	}
	return nil
}

func isID(s string) bool {
	n, err := strconv.ParseUint(s, 10, 32)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == s
}

func (p *Policy) validate(c *Config) error {
	var errs []error
	add := func(format string, a ...interface{}) { errs = append(errs, fmt.Errorf(format, a...)) }

	if p.Level < 0 || p.Level > MaxLevel {
		add("level: %d is not 0–%d", p.Level, MaxLevel)
	}
	if len(p.Roles) == 0 {
		add("roles: select at least one role (use level 0 to turn compression off)")
	}
	seen := map[string]bool{}
	for _, r := range p.Roles {
		switch r {
		case RoleTool, RoleUser, RoleAssistant, RoleSystem:
		default:
			add("roles: %q is not one of tool, user, assistant, system", r)
		}
		if seen[r] {
			add("roles: %q is listed twice", r)
		}
		seen[r] = true
	}
	for field, v := range map[string]int{
		"min_unit_tokens":      p.MinUnitTokens,
		"protect_recent_turns": p.ProtectRecentTurns,
		"max_unit_tokens":      p.MaxUnitTokens,
		"mask_after_turns":     p.MaskAfterTurns,
		"mask_step_tokens":     p.MaskStepTokens,
	} {
		if v < 0 {
			add("%s: must not be negative", field)
		}
	}
	switch p.CacheMode {
	case CacheDeterministic, CacheSkipCached:
	default:
		add("cache_mode: %q is not one of deterministic, skip_cached", p.CacheMode)
	}

	if len(p.ToolOverrides) > MaxToolOverrides {
		add("tool_overrides: %d defined, at most %d allowed", len(p.ToolOverrides), MaxToolOverrides)
	}
	for tool, o := range p.ToolOverrides {
		if tool == "" {
			add("tool_overrides: a tool name is empty")
		}
		if o == nil {
			add("tool_overrides[%q]: is empty", tool)
			continue
		}
		if o.Level != nil && (*o.Level < 0 || *o.Level > MaxLevel) {
			add("tool_overrides[%q].level: %d is not 0–%d", tool, *o.Level, MaxLevel)
		}
		if o.MaxUnitTokens < 0 {
			add("tool_overrides[%q].max_unit_tokens: must not be negative", tool)
		}
	}

	if len(p.ProtectPatterns) > MaxProtectPatterns {
		add("protect_patterns: %d defined, at most %d allowed", len(p.ProtectPatterns), MaxProtectPatterns)
	}
	p.compiled = p.compiled[:0]
	for i, pat := range p.ProtectPatterns {
		re, err := compilePattern(pat)
		if err != nil {
			add("protect_patterns[%d]: %w", i, err)
			continue
		}
		p.compiled = append(p.compiled, re)
	}

	if e := p.Engine; e != nil {
		switch e.Kind {
		case EngineHeadroom, EngineLLMLingua2:
		default:
			add("engine.kind: %q is not one of headroom, llmlingua2", e.Kind)
		}
		if !policyNamePattern.MatchString(e.Name) || strings.Contains(e.Name, "-") {
			add("engine.name: %q must be lower-case letters, digits or '_'", e.Name)
		} else if c.Engines[e.Name].URL == "" {
			add("engine.name: %q has no engine_%s_url setting", e.Name, e.Name)
		}
		if e.TimeoutMs == 0 {
			e.TimeoutMs = DefaultEngineTimeout
		}
		if e.TimeoutMs < 0 || e.TimeoutMs > MaxEngineTimeoutMs {
			add("engine.timeout_ms: %d is not 1–%d", e.TimeoutMs, MaxEngineTimeoutMs)
		}
	}
	return errors.Join(errs...)
}

// compilePattern compiles one protect pattern within the size limits. Go's
// regexp runs in linear time, so the limits bound memory and per-byte cost,
// not backtracking: a short pattern such as (abcdefghij){1000} still compiles
// to a large program.
func compilePattern(pat string) (*regexp.Regexp, error) {
	if pat == "" {
		return nil, errors.New("is empty")
	}
	if len(pat) > MaxProtectPatternLen {
		return nil, fmt.Errorf("is %d characters, at most %d allowed", len(pat), MaxProtectPatternLen)
	}
	parsed, err := syntax.Parse(pat, syntax.Perl)
	if err != nil {
		return nil, err
	}
	prog, err := syntax.Compile(parsed.Simplify())
	if err != nil {
		return nil, err
	}
	if len(prog.Inst) > maxRegexpInstructions {
		return nil, fmt.Errorf("is too complex (%d instructions, at most %d allowed)", len(prog.Inst), maxRegexpInstructions)
	}
	return regexp.Compile(pat)
}

// ProtectRegexps returns the compiled protect patterns. Validate fills them.
func (p *Policy) ProtectRegexps() []*regexp.Regexp {
	return p.compiled
}

// ToolOverride returns the override for a tool, or nil.
func (p *Policy) ToolOverride(tool string) *ToolOverride {
	if p.ToolOverrides == nil {
		return nil
	}
	return p.ToolOverrides[tool]
}

// HasRole reports whether the policy selects units of this role.
func (p *Policy) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}
