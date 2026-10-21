package config

import (
	"encoding/json"
	"strconv"
)

// Source says which rule chose a request's policy.
type Source string

const (
	SourceAppMetadata Source = "app_metadata"
	SourceAppBinding  Source = "app_binding"
	SourceLLMIDBind   Source = "llm_id_binding"
	SourceLLMBinding  Source = "llm_binding"
	SourceDefault     Source = "default"
)

// Target identifies the request a policy is resolved for, from the plugin
// context at post_auth.
type Target struct {
	AppID   uint32
	LLMID   uint32
	LLMSlug string
}

// Resolution is the policy that applies to one request.
type Resolution struct {
	// Name is the policy name; "off" for an opt-out.
	Name string
	// Policy is shared: treat it as read-only.
	Policy *Policy
	// Hash is Policy.Hash(), carried on every record.
	Hash   string
	Source Source
}

// Off reports whether the resolved policy compresses nothing.
func (r Resolution) Off() bool {
	return r.Policy == nil || r.Policy.Level == 0
}

// Resolve picks the policy for a request. The first match wins:
//
//  1. appPolicy, the App metadata override (prompt_compressor.policy);
//  2. the app:<id> binding;
//  3. the llm_id:<id> binding;
//  4. the llm:<slug> binding;
//  5. default_policy.
//
// An App metadata value that names no policy is ignored, so a typo in one
// App falls back to the bindings instead of failing its requests.
func (c *Config) Resolve(t Target, appPolicy string) Resolution {
	if appPolicy != "" {
		if p, ok := c.Policy(appPolicy); ok {
			return Resolution{Name: appPolicy, Policy: p, Hash: p.Hash(), Source: SourceAppMetadata}
		}
	}
	try := func(key string, src Source) (Resolution, bool) {
		name, ok := c.Bindings[key]
		if !ok {
			return Resolution{}, false
		}
		p, ok := c.Policy(name)
		if !ok {
			return Resolution{}, false
		}
		return Resolution{Name: name, Policy: p, Hash: p.Hash(), Source: src}, true
	}
	if t.AppID != 0 {
		if r, ok := try(BindApp+strconv.FormatUint(uint64(t.AppID), 10), SourceAppBinding); ok {
			return r
		}
	}
	if t.LLMID != 0 {
		if r, ok := try(BindLLMID+strconv.FormatUint(uint64(t.LLMID), 10), SourceLLMIDBind); ok {
			return r
		}
	}
	if t.LLMSlug != "" {
		if r, ok := try(BindLLM+t.LLMSlug, SourceLLMBinding); ok {
			return r
		}
	}
	p, ok := c.Policy(c.DefaultPolicy)
	if !ok {
		// Validate rejects this; a hand-built Config falls back to the defaults
		p = defaultPolicy
	}
	return Resolution{Name: c.DefaultPolicy, Policy: p, Hash: p.Hash(), Source: SourceDefault}
}

// AppMetadataKey is the App metadata key that overrides the policy for one
// App. Both {"prompt_compressor.policy": "max"} and
// {"prompt_compressor": {"policy": "max"}} are read.
const AppMetadataKey = "prompt_compressor.policy"

// AppPolicyFromMetadata reads the policy override from an App's metadata
// JSON. It returns "" when there is none or the metadata does not parse.
func AppPolicyFromMetadata(metadata string) string {
	if metadata == "" {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return ""
	}
	var name string
	if raw, ok := m[AppMetadataKey]; ok && json.Unmarshal(raw, &name) == nil {
		return name
	}
	if raw, ok := m["prompt_compressor"]; ok {
		var nested struct {
			Policy string `json:"policy"`
		}
		if json.Unmarshal(raw, &nested) == nil {
			return nested.Policy
		}
	}
	return ""
}
