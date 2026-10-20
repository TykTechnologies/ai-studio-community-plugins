package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"
)

// The schema is served by GetConfigSchema from the plugin root.
const schemaPath = "../config.schema.json"

func loadSchema(t *testing.T) (*gojsonschema.Schema, map[string]interface{}) {
	t.Helper()
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	s, err := gojsonschema.NewSchema(gojsonschema.NewBytesLoader(raw))
	if err != nil {
		t.Fatalf("schema does not compile: %v", err)
	}
	return s, doc
}

func schemaValid(t *testing.T, s *gojsonschema.Schema, config string) (bool, string) {
	t.Helper()
	res, err := s.Validate(gojsonschema.NewStringLoader(config))
	if err != nil {
		t.Fatalf("validate %s: %v", config, err)
	}
	var msgs []string
	for _, e := range res.Errors() {
		msgs = append(msgs, e.String())
	}
	return res.Valid(), strings.Join(msgs, "; ")
}

func TestSchemaAcceptsTheRFCExample(t *testing.T) {
	s, _ := loadSchema(t)
	for _, config := range []string{rfcExample, `{}`, `{"mode": "off"}`} {
		if ok, msg := schemaValid(t, s, config); !ok {
			t.Errorf("schema rejects %s: %s", config, msg)
		}
	}
}

// Every variant is something Parse also rejects, so the form and the plugin
// agree.
func TestSchemaRejectsBrokenVariants(t *testing.T) {
	s, _ := loadSchema(t)
	for name, config := range map[string]string{
		"unknown mode":          `{"mode": "on"}`,
		"unknown setting":       `{"log_text_samples": false}`,
		"bypass not a bool":     `{"allow_client_bypass": "yes"}`,
		"zero body limit":       `{"max_body_bytes": 0}`,
		"level too high":        `{"policies": {"p": {"level": 4}}}`,
		"unknown policy field":  `{"policies": {"p": {"levle": 2}}}`,
		"unknown role":          `{"policies": {"p": {"roles": ["robot"]}}}`,
		"no roles":              `{"policies": {"p": {"roles": []}}}`,
		"bad policy name":       `{"policies": {"Bad Name": {}}}`,
		"bad binding key":       `{"bindings": {"team:1": "off"}}`,
		"bad app id":            `{"bindings": {"app:abc": "off"}}`,
		"binding value not str": `{"bindings": {"llm:a": 1}}`,
		"tool override field":   `{"policies": {"p": {"tool_overrides": {"Bash": {"drop": true}}}}}`,
		"engine kind":           `{"policies": {"p": {"engine": {"name": "h", "kind": "gpt"}}}}`,
		"engine without name":   `{"policies": {"p": {"engine": {"kind": "headroom"}}}}`,
		"engine creds nested":   `{"policies": {"p": {"engine": {"name": "h", "kind": "headroom", "url": "$SECRET/u"}}}}`,
		"long pattern":          `{"policies": {"p": {"protect_patterns": ["` + strings.Repeat("a", 513) + `"]}}}`,
	} {
		if ok, _ := schemaValid(t, s, config); ok {
			t.Errorf("%s: schema accepts %s", name, config)
		}
		if _, err := ParseJSON([]byte(config)); err == nil {
			t.Errorf("%s: Parse accepts %s, which the schema rejects", name, config)
		}
	}
}

// The schema's defaults are what the form fills in, so they must be the
// plugin's defaults.
func TestSchemaDefaultsMatchTheGoDefaults(t *testing.T) {
	_, doc := loadSchema(t)
	props := doc["properties"].(map[string]interface{})
	policyProps := doc["definitions"].(map[string]interface{})["policy"].(map[string]interface{})["properties"].(map[string]interface{})

	defaults := map[string]interface{}{}
	for k, v := range props {
		if d, ok := v.(map[string]interface{})["default"]; ok {
			if _, isObj := d.(map[string]interface{}); !isObj {
				defaults[k] = d
			}
		}
	}
	policyDefaults := map[string]interface{}{}
	for k, v := range policyProps {
		if d, ok := v.(map[string]interface{})["default"]; ok {
			policyDefaults[k] = d
		}
	}

	fromSchema, err := json.Marshal(defaults)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseJSON(fromSchema)
	if err != nil {
		t.Fatalf("schema defaults do not parse: %v", err)
	}
	d := Default()
	if c.Mode != d.Mode || c.DefaultPolicy != d.DefaultPolicy || c.AllowClientBypass != d.AllowClientBypass ||
		c.FailureMode != d.FailureMode || c.LossyWithRequestFilters != d.LossyWithRequestFilters || c.MaxBodyBytes != d.MaxBodyBytes || c.CacheDropAlert != d.CacheDropAlert {
		t.Errorf("schema top-level defaults %s differ from Default()", fromSchema)
	}
	if len(defaults) != 7 {
		t.Errorf("schema has defaults for %d top-level settings, want 7", len(defaults))
	}

	policyJSON, err := json.Marshal(policyDefaults)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := ParseJSON([]byte(`{"policies": {"p": ` + string(policyJSON) + `}}`))
	if err != nil {
		t.Fatalf("schema policy defaults do not parse: %v", err)
	}
	p, _ := pc.Policy("p")
	if p.Hash() != DefaultPolicy().computeHash() {
		t.Errorf("schema policy defaults %s differ from DefaultPolicy()", policyJSON)
	}
}
