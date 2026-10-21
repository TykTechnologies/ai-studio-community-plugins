package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// rfcExample is the RFC's example config as amended by the decisions:
// engine credentials are top-level keys (eac.3) and log_text_samples is gone
// (samples are Phase 4).
const rfcExample = `{
  "mode": "enforce",
  "default_policy": "safe",
  "policies": {
    "safe":   {"level": 1},
    "agents": {"level": 2, "roles": ["tool"], "min_unit_tokens": 400,
               "protect_recent_turns": 1, "cache_mode": "deterministic",
               "tool_overrides": {"Bash": {"max_unit_tokens": 4000},
                                  "read_contract": {"exclude": true}},
               "protect_patterns": ["[A-Z]{2,6}-[0-9]+"]},
    "max":    {"level": 3, "max_unit_tokens": 6000, "mask_after_turns": 8,
               "mask_step_tokens": 20000,
               "engine": {"name": "headroom", "kind": "headroom", "timeout_ms": 2000}}
  },
  "bindings": {
    "llm:claude-prod": "agents",
    "app:42": "max"
  },
  "engine_headroom_url": "$SECRET/headroom_url",
  "engine_headroom_token": "$SECRET/headroom_token",
  "allow_client_bypass": true,
  "failure_mode": "open",
  "lossy_with_request_filters": false
}`

// hostFlatten turns a stored config into the map Initialize receives, as
// services/plugin_init_config.go and microgateway/plugins/manager.go do
// (without $SECRET/ resolution).
func hostFlatten(t *testing.T, raw string) map[string]string {
	t.Helper()
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("test config does not parse: %v", err)
	}
	m := make(map[string]string, len(config)+2)
	for k, v := range config {
		switch val := v.(type) {
		case string:
			m[k] = val
		case int, int64, uint, uint64, float64, bool:
			m[k] = fmt.Sprintf("%v", val)
		default:
			b, err := json.Marshal(val)
			if err != nil {
				t.Fatalf("flatten %s: %v", k, err)
			}
			m[k] = string(b)
		}
	}
	// The gateway adds _plugin_id and _service_broker_id, Studio plugin_id,
	// service_broker_id and has_service_api
	m["_plugin_id"] = "7"
	m["_service_broker_id"] = "3"
	m["plugin_id"] = "7"
	m["service_broker_id"] = "3"
	m["has_service_api"] = "true"
	return m
}

func TestUnknownTopLevelKeys(t *testing.T) {
	// From the host an unknown key is skipped, so a key a newer host adds
	// cannot turn compression off
	c := mustParse(t, `{"mode": "enforce", "log_text_samples": false, "zzz": 1}`)
	if c.Mode != ModeEnforce || !reflect.DeepEqual(c.Ignored, []string{"log_text_samples", "zzz"}) {
		t.Errorf("mode %s, ignored %v", c.Mode, c.Ignored)
	}
	// What an admin saves is checked strictly
	_, err := ParseJSON([]byte(`{"mode": "enforce", "log_text_samples": false}`))
	if err == nil || !strings.Contains(err.Error(), "log_text_samples: unknown setting") {
		t.Errorf("ParseJSON error = %v, want the unknown setting named", err)
	}
	// Host keys are never reported
	if c := mustParse(t, `{}`); len(c.Ignored) != 0 {
		t.Errorf("host keys reported as ignored: %v", c.Ignored)
	}
}

func mustParse(t *testing.T, raw string) *Config {
	t.Helper()
	c, err := Parse(hostFlatten(t, raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

func TestDefaults(t *testing.T) {
	for name, input := range map[string]map[string]string{
		"nil":          nil,
		"empty":        {},
		"host keys":    {"_plugin_id": "1", "_service_broker_id": "2"},
		"null setting": {"mode": "null", "policies": "null"},
	} {
		c, err := Parse(input)
		if err != nil {
			t.Fatalf("%s: Parse: %v", name, err)
		}
		if c.Mode != ModeAudit || c.FailureMode != FailOpen || !c.AllowClientBypass || c.LossyWithRequestFilters ||
			c.DefaultPolicy != DefaultPolicyName || c.MaxBodyBytes != DefaultMaxBodyBytes {
			t.Errorf("%s: top-level defaults = %+v", name, c)
		}
		r := c.Resolve(Target{AppID: 1, LLMID: 2, LLMSlug: "x"}, "")
		want := &Policy{Level: 1, Roles: []string{RoleTool}, MinUnitTokens: 300, ProtectRecentTurns: 1,
			CacheMode: CacheDeterministic, MaxUnitTokens: 6000, MaskAfterTurns: 0, MaskStepTokens: 20000}
		got := *r.Policy
		got.hash, got.compiled = "", nil
		if r.Name != DefaultPolicyName || r.Source != SourceDefault || !reflect.DeepEqual(&got, want) {
			t.Errorf("%s: default resolution = %s/%s %+v", name, r.Name, r.Source, got)
		}
	}
}

func TestPolicyStartsFromDefaults(t *testing.T) {
	c := mustParse(t, `{"policies": {"tight": {"min_unit_tokens": 50}}, "default_policy": "tight"}`)
	p, _ := c.Policy("tight")
	if p.MinUnitTokens != 50 || p.Level != 1 || !p.HasRole(RoleTool) || p.ProtectRecentTurns != 1 {
		t.Errorf("policy = %+v, want the defaults with min_unit_tokens 50", p)
	}
}

func TestRFCExampleParses(t *testing.T) {
	c := mustParse(t, rfcExample)
	if c.Mode != ModeEnforce || len(c.Policies) != 3 {
		t.Fatalf("config = %+v", c)
	}
	agents, _ := c.Policy("agents")
	if o := agents.ToolOverride("read_contract"); o == nil || !o.Exclude {
		t.Error("read_contract is not excluded")
	}
	if o := agents.ToolOverride("Bash"); o == nil || o.MaxUnitTokens != 4000 {
		t.Error("Bash budget is not 4000")
	}
	if res := agents.ProtectRegexps(); len(res) != 1 || !res[0].MatchString("see JIRA-123") {
		t.Errorf("protect patterns = %v", res)
	}
	if got := c.Engines["headroom"]; got.URL != "$SECRET/headroom_url" || got.Token != "$SECRET/headroom_token" {
		t.Errorf("engine credentials = %+v", got)
	}
	// The host formats a big number as 1e+06
	c = mustParse(t, `{"max_body_bytes": 1000000}`)
	if c.MaxBodyBytes != 1000000 {
		t.Errorf("max_body_bytes = %d", c.MaxBodyBytes)
	}
}

func TestParseJSONMatchesParse(t *testing.T) {
	a, err := ParseJSON([]byte(rfcExample))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	b := mustParse(t, rfcExample)
	for name := range b.Policies {
		pa, _ := a.Policy(name)
		pb, _ := b.Policy(name)
		if pa.Hash() != pb.Hash() {
			t.Errorf("policy %s hashes differ between ParseJSON and Parse", name)
		}
	}
	if !reflect.DeepEqual(a.Bindings, b.Bindings) || a.Mode != b.Mode || !reflect.DeepEqual(a.Engines, b.Engines) {
		t.Error("ParseJSON and Parse disagree")
	}
	if _, err := ParseJSON([]byte(`[1]`)); err == nil {
		t.Error("ParseJSON accepted a non-object")
	}
}

func TestInvalidConfigs(t *testing.T) {
	cases := map[string]struct {
		config string
		want   string
	}{
		"unknown mode":            {`{"mode": "on"}`, "mode"},
		"unknown failure mode":    {`{"failure_mode": "maybe"}`, "failure_mode"},
		"bad bool":                {`{"allow_client_bypass": "yes"}`, "allow_client_bypass"},
		"fractional int":          {`{"max_body_bytes": 1.5}`, "max_body_bytes"},
		"zero body limit":         {`{"max_body_bytes": 0}`, "max_body_bytes"},
		"cache drop above 1":      {`{"cache_drop_alert": 1.5}`, "cache_drop_alert"},
		"cache drop not a number": {`{"cache_drop_alert": "x"}`, "cache_drop_alert"},
		"unknown default policy":  {`{"default_policy": "nope"}`, "default_policy: names no defined policy"},
		"unknown bound policy":    {`{"bindings": {"llm:a": "nope"}}`, `names unknown policy "nope"`},
		"bad binding key":         {`{"bindings": {"team:1": "off"}}`, "must start with"},
		"bad app id":              {`{"bindings": {"app:abc": "off"}}`, "App ID"},
		"zero llm id":             {`{"bindings": {"llm_id:0": "off"}}`, "LLM ID"},
		"empty slug":              {`{"bindings": {"llm:": "off"}}`, "no LLM slug"},
		"binding value not str":   {`{"bindings": {"llm:a": 1}}`, "bindings"},
		"off is reserved":         {`{"policies": {"off": {"level": 2}}}`, "reserved"},
		"bad policy name":         {`{"policies": {"Bad Name": {}}}`, "name"},
		"unknown policy field":    {`{"policies": {"p": {"levle": 2}}}`, "levle"},
		"level too high":          {`{"policies": {"p": {"level": 4}}}`, "level"},
		"negative tokens":         {`{"policies": {"p": {"min_unit_tokens": -1}}}`, "min_unit_tokens"},
		"no roles":                {`{"policies": {"p": {"roles": []}}}`, "roles"},
		"unknown role":            {`{"policies": {"p": {"roles": ["robot"]}}}`, "robot"},
		"duplicate role":          {`{"policies": {"p": {"roles": ["tool", "tool"]}}}`, "twice"},
		"bad cache mode":          {`{"policies": {"p": {"cache_mode": "always"}}}`, "cache_mode"},
		"invalid regex":           {`{"policies": {"p": {"protect_patterns": ["[a-"]}}}`, "protect_patterns[0]"},
		"empty regex":             {`{"policies": {"p": {"protect_patterns": [""]}}}`, "empty"},
		"long regex":              {`{"policies": {"p": {"protect_patterns": ["` + strings.Repeat("a", 513) + `"]}}}`, "at most 512"},
		"complex regex":           {`{"policies": {"p": {"protect_patterns": ["(abcdefghij){1000}"]}}}`, "too complex"},
		"tool override level":     {`{"policies": {"p": {"tool_overrides": {"Bash": {"level": 9}}}}}`, "Bash"},
		"tool override field":     {`{"policies": {"p": {"tool_overrides": {"Bash": {"drop": true}}}}}`, "drop"},
		"engine without url":      {`{"policies": {"p": {"engine": {"name": "h", "kind": "headroom"}}}}`, "engine_h_url"},
		"engine kind":             {`{"engine_h_url": "http://x", "policies": {"p": {"engine": {"name": "h", "kind": "gpt"}}}}`, "engine.kind"},
		"engine timeout":          {`{"engine_h_url": "http://x", "policies": {"p": {"engine": {"name": "h", "kind": "headroom", "timeout_ms": 999999}}}}`, "timeout_ms"},
		"policies not an object":  {`{"policies": ["safe"]}`, "policies"},
		"several errors reported": {`{"mode": "on", "failure_mode": "maybe"}`, "failure_mode"},
	}
	for name, tc := range cases {
		_, err := Parse(hostFlatten(t, tc.config))
		if err == nil {
			t.Errorf("%s: Parse accepted %s", name, tc.config)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not mention %q", name, err, tc.want)
		}
	}
}

func TestResolutionPrecedence(t *testing.T) {
	c := mustParse(t, `{
	  "default_policy": "base",
	  "policies": {"base": {}, "by-slug": {"level": 2}, "by-id": {"level": 2, "min_unit_tokens": 1},
	               "by-app": {"level": 3}, "meta": {"level": 2, "min_unit_tokens": 2}},
	  "bindings": {"llm:claude": "by-slug", "llm_id:5": "by-id", "app:42": "by-app", "app:43": "off"}
	}`)
	cases := []struct {
		name       string
		target     Target
		appPolicy  string
		wantPolicy string
		wantSource Source
	}{
		{"nothing matches", Target{AppID: 1, LLMID: 1, LLMSlug: "gpt"}, "", "base", SourceDefault},
		{"llm slug", Target{AppID: 1, LLMID: 1, LLMSlug: "claude"}, "", "by-slug", SourceLLMBinding},
		{"llm id beats slug", Target{AppID: 1, LLMID: 5, LLMSlug: "claude"}, "", "by-id", SourceLLMIDBind},
		{"app beats llm", Target{AppID: 42, LLMID: 5, LLMSlug: "claude"}, "", "by-app", SourceAppBinding},
		{"app opt-out", Target{AppID: 43, LLMID: 5, LLMSlug: "claude"}, "", "off", SourceAppBinding},
		{"metadata beats app", Target{AppID: 42, LLMID: 5, LLMSlug: "claude"}, "meta", "meta", SourceAppMetadata},
		{"metadata opt-out", Target{AppID: 42}, "off", "off", SourceAppMetadata},
		{"metadata default name", Target{AppID: 42}, "default", "default", SourceAppMetadata},
		{"unknown metadata falls back", Target{AppID: 42, LLMSlug: "claude"}, "typo", "by-app", SourceAppBinding},
		{"no app", Target{LLMSlug: "claude"}, "", "by-slug", SourceLLMBinding},
	}
	for _, tc := range cases {
		r := c.Resolve(tc.target, tc.appPolicy)
		if r.Name != tc.wantPolicy || r.Source != tc.wantSource {
			t.Errorf("%s: resolved %s from %s, want %s from %s", tc.name, r.Name, r.Source, tc.wantPolicy, tc.wantSource)
		}
		if r.Policy == nil || r.Hash != r.Policy.Hash() {
			t.Errorf("%s: resolution carries no policy or a stale hash", tc.name)
		}
		if r.Off() != (tc.wantPolicy == PolicyOff) {
			t.Errorf("%s: Off() = %v", tc.name, r.Off())
		}
	}
}

func TestHashIsStableAcrossKeyOrder(t *testing.T) {
	a := mustParse(t, `{"policies": {"p": {"level": 2, "roles": ["tool", "user"], "min_unit_tokens": 10,
	  "tool_overrides": {"A": {"exclude": true}, "B": {"level": 1}}}}, "default_policy": "p"}`)
	b := mustParse(t, `{"default_policy": "p", "policies": {"p": {"tool_overrides": {"B": {"level": 1}, "A": {"exclude": true}},
	  "min_unit_tokens": 10, "roles": ["user", "tool"], "level": 2}}}`)
	pa, _ := a.Policy("p")
	pb, _ := b.Policy("p")
	if pa.Hash() != pb.Hash() {
		t.Error("policy hash depends on key or role order")
	}
	// Defaults written out or left implicit are the same policy
	c := mustParse(t, `{"policies": {"p": {"level": 1, "roles": ["tool"], "min_unit_tokens": 300}, "q": {}}}`)
	pc, _ := c.Policy("p")
	qc, _ := c.Policy("q")
	dflt, _ := c.Policy(DefaultPolicyName)
	if pc.Hash() != qc.Hash() || qc.Hash() != dflt.Hash() {
		t.Error("an explicit default changes the hash")
	}
	if len(pc.Hash()) != 64 || len(pc.ShortHash()) != 12 || !strings.HasPrefix(pc.Hash(), pc.ShortHash()) {
		t.Errorf("hash %q / short %q", pc.Hash(), pc.ShortHash())
	}
	// Studio sees $SECRET/ references where an edge sees values
	studio := mustParse(t, `{"engine_h_url": "$SECRET/u", "policies": {"p": {"engine": {"name": "h", "kind": "headroom"}}}}`)
	edge := mustParse(t, `{"engine_h_url": "http://10.0.0.1:8787", "engine_h_token": "t", "policies": {"p": {"engine": {"name": "h", "kind": "headroom"}}}}`)
	ps, _ := studio.Policy("p")
	pe, _ := edge.Policy("p")
	if ps.Hash() != pe.Hash() {
		t.Error("engine credentials change the policy hash")
	}
}

func TestHashChangesWithEverySetting(t *testing.T) {
	base := `{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"]}`
	variants := []string{
		`{"level": 3, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"]}`,
		`{"level": 2, "roles": ["tool", "user"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"]}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 11}}, "protect_patterns": ["X-[0-9]+"]}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"B": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"]}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10, "exclude": true}}, "protect_patterns": ["X-[0-9]+"]}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10, "level": 1}}, "protect_patterns": ["X-[0-9]+"]}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["Y-[0-9]+"]}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "min_unit_tokens": 1}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "protect_recent_turns": 2}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "cache_mode": "skip_cached"}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "dedupe_references": true}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "max_unit_tokens": 1}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "mask_after_turns": 1}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "mask_step_tokens": 1}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "engine": {"name": "h", "kind": "headroom"}}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "engine": {"name": "h", "kind": "headroom", "timeout_ms": 10}}`,
		`{"level": 2, "roles": ["tool"], "tool_overrides": {"A": {"max_unit_tokens": 10}}, "protect_patterns": ["X-[0-9]+"], "engine": {"name": "h", "kind": "llmlingua2"}}`,
	}
	hashOf := func(policy string) string {
		c := mustParse(t, `{"engine_h_url": "http://x", "policies": {"p": `+policy+`}}`)
		p, _ := c.Policy("p")
		return p.Hash()
	}
	seen := map[string]int{hashOf(base): -1}
	for i, v := range variants {
		h := hashOf(v)
		if prev, dup := seen[h]; dup {
			t.Errorf("variant %d has the same hash as %d: %s", i, prev, v)
		}
		seen[h] = i
	}
	if hashOf(base) != hashOf(base) {
		t.Error("hash is not deterministic")
	}
}

func TestEngineKeysNeedAName(t *testing.T) {
	c := mustParse(t, `{"engine_my_engine_url": "http://u", "engine_my_engine_token": "t"}`)
	if got := c.Engines["my_engine"]; got.URL != "http://u" || got.Token != "t" {
		t.Errorf("engines = %+v", c.Engines)
	}
	if _, err := ParseJSON([]byte(`{"engine__url": "u"}`)); err == nil {
		t.Error("engine key without a name was accepted")
	}
}

// An engine URL must be http or https; the error names the key, never the
// value, which may carry credentials. Unresolved references are left to the
// side that resolves them.
func TestEngineURLValidation(t *testing.T) {
	for raw, want := range map[string]string{
		"ftp://user:pw-secret@engine.internal/x": "engine_main_url: must be an http or https URL",
		"pw-secret":                              "engine_main_url: must be an http or https URL",
		"https://":                               "engine_main_url: must be an http or https URL",
		"//user:pw-secret@host":                  "engine_main_url: must be an http or https URL",
		"https://user:pw-secret@engine.internal:8080/b": "engine_main_url: must not carry credentials; put them in engine_main_token",
		"http://pw-secret@127.0.0.1:9000":               "engine_main_url: must not carry credentials",
	} {
		_, err := ParseJSON([]byte(`{"engine_main_url": ` + strconv.Quote(raw) + `}`))
		if err == nil {
			t.Errorf("%q was accepted", raw)
			continue
		}
		if !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "pw-secret") {
			t.Errorf("%q: error %q", raw, err)
		}
	}
	for _, raw := range []string{"https://engine.internal:8080/base", "http://127.0.0.1:9000", "$SECRET/engine_url", refusedCredential} {
		if _, err := ParseJSON([]byte(`{"engine_main_url": ` + strconv.Quote(raw) + `}`)); err != nil {
			t.Errorf("%q: %v", raw, err)
		}
	}
}
