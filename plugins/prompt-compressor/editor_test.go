package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/TykTechnologies/midsommar/v2/models"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	studiopb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// fakeStudio stands in for the Studio management API: the stored config,
// paged LLM and App lists capped at 100 rows like the real server.
type fakeStudio struct {
	mu        sync.Mutex
	stored    string
	updates   []string
	updateErr error
	llms      []*studiopb.LLMInfo
	apps      []*studiopb.AppInfo
	listCalls int
}

func (f *fakeStudio) GetPluginConfig(ctx context.Context, pluginID uint32) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if pluginID != 7 {
		return "", errors.New("wrong plugin")
	}
	return f.stored, nil
}

func (f *fakeStudio) UpdatePluginConfig(ctx context.Context, pluginID uint32, configJSON string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updates = append(f.updates, configJSON)
	f.stored = configJSON
	return nil
}

func page[T any](rows []T, page, limit int32) []T {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	start := int((page - 1) * limit)
	if start >= len(rows) {
		return nil
	}
	end := start + int(limit)
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}

func (f *fakeStudio) ListLLMs(ctx context.Context, p, limit int32) ([]*studiopb.LLMInfo, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	return page(f.llms, p, limit), int64(len(f.llms)), nil
}

func (f *fakeStudio) ListApps(ctx context.Context, p, limit int32) ([]*studiopb.AppInfo, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	return page(f.apps, p, limit), int64(len(f.apps)), nil
}

func editorPlugin(t *testing.T, studio *fakeStudio) *PromptCompressorPlugin {
	t.Helper()
	p := NewPromptCompressorPlugin()
	p.testStudio = studio
	if err := p.Initialize(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio}, map[string]string{"plugin_id": "7", "has_service_api": "true"}); err != nil {
		t.Fatal(err)
	}
	return p
}

func callRPC(t *testing.T, p *PromptCompressorPlugin, method string, payload interface{}, out interface{}) error {
	t.Helper()
	raw, _ := json.Marshal(payload)
	res, err := p.HandleRPC(method, raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(res, out); err != nil {
		t.Fatalf("%s returned invalid JSON: %v", method, err)
	}
	return nil
}

func TestGetEditorListsEveryLLMAndApp(t *testing.T) {
	studio := &fakeStudio{stored: `{"mode": "enforce", "engine_h_url": "$SECRET/u", "engine_h_token": "plaintext", "engine_k_token": "$SECRET/k"}`}
	for i := 1; i <= 250; i++ {
		studio.llms = append(studio.llms, &studiopb.LLMInfo{Id: uint32(i), Name: fmt.Sprintf("Claude Prod %03d", i), Vendor: "anthropic", Active: true})
	}
	for i := 1; i <= 120; i++ {
		studio.apps = append(studio.apps, &studiopb.AppInfo{Id: uint32(i), Name: fmt.Sprintf("app %03d", i)})
	}
	p := editorPlugin(t, studio)

	var view editorView
	if err := callRPC(t, p, "getEditor", map[string]interface{}{"_service_broker_id": 3}, &view); err != nil {
		t.Fatalf("getEditor: %v", err)
	}
	if len(view.LLMs) != 250 || len(view.Apps) != 120 || view.LLMsTruncated || view.AppsTruncated {
		t.Fatalf("listed %d LLMs and %d Apps (truncated %v/%v), want all 250 and 120", len(view.LLMs), len(view.Apps), view.LLMsTruncated, view.AppsTruncated)
	}
	if view.LLMs[0].Slug != "claude-prod-001" {
		t.Errorf("LLM slug = %q, want the gateway's slug of the name", view.LLMs[0].Slug)
	}
	if view.Config["engine_h_token"] != maskedToken {
		t.Errorf("a stored engine token reached the editor: %v", view.Config["engine_h_token"])
	}
	if view.Config["engine_k_token"] != "$SECRET/k" || view.Config["engine_h_url"] != "$SECRET/u" {
		t.Error("secret references and URLs should be shown as they are")
	}
	if len(view.Problems) != 0 {
		t.Errorf("valid stored config reported problems: %v", view.Problems)
	}
}

// setLLMField sets an LLMInfo field by name, so the test builds against host
// versions with and without it. It reports whether the field exists.
func setLLMField(l *studiopb.LLMInfo, name string, v protoreflect.Value) bool {
	f := l.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(name))
	if f == nil {
		return false
	}
	l.ProtoReflect().Set(f, v)
	return true
}

func TestLLMOptionUsesTheHostSlugAndFilterState(t *testing.T) {
	// A host without the fields: the slug is computed, the filter state unknown
	old := llmOptionFrom(&studiopb.LLMInfo{Id: 1, Name: "Claude Prod EU", Vendor: "anthropic"})
	if old.Slug != "claude-prod-eu" || old.FiltersKnown || old.HasRequestFilters {
		t.Errorf("without host fields: %+v", old)
	}

	withFilters := &studiopb.LLMInfo{Id: 2, Name: "Claude Prod EU"}
	if !setLLMField(withFilters, "slug", protoreflect.ValueOfString("claude-prod-eu-2")) {
		t.Skip("this host's LLMInfo has no slug field yet")
	}
	setLLMField(withFilters, "has_request_filters", protoreflect.ValueOfBool(true))
	got := llmOptionFrom(withFilters)
	if got.Slug != "claude-prod-eu-2" || !got.FiltersKnown || !got.HasRequestFilters {
		t.Errorf("with host fields: %+v, want the host's slug and filters known and present", got)
	}

	none := &studiopb.LLMInfo{Id: 3, Name: "Plain"}
	setLLMField(none, "slug", protoreflect.ValueOfString("plain"))
	if got := llmOptionFrom(none); got.FiltersKnown {
		t.Errorf("filters reported known without the field: %+v", got)
	}
	// A host that knows the field always sets it, false included.
	setLLMField(none, "has_request_filters", protoreflect.ValueOfBool(false))
	if got := llmOptionFrom(none); !got.FiltersKnown || got.HasRequestFilters {
		t.Errorf("no filters: %+v", got)
	}
}

func TestGetEditorReportsAnInvalidStoredConfig(t *testing.T) {
	p := editorPlugin(t, &fakeStudio{stored: `{"bindings": {"llm:a": "missing"}}`})
	var view editorView
	if err := callRPC(t, p, "getEditor", nil, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Problems) == 0 || !strings.Contains(strings.Join(view.Problems, " "), "missing") {
		t.Errorf("problems = %v, want the unknown policy named", view.Problems)
	}
	// An empty stored config is the defaults
	p = editorPlugin(t, &fakeStudio{stored: ``})
	if err := callRPC(t, p, "getEditor", nil, &view); err != nil || len(view.Problems) != 0 {
		t.Errorf("empty stored config: %v, %v", err, view.Problems)
	}
}

type saveResult struct {
	Saved  bool     `json:"saved"`
	Errors []string `json:"errors"`
}

func TestSaveConfigRoundTrip(t *testing.T) {
	studio := &fakeStudio{stored: `{"engine_h_url": "$SECRET/u", "engine_h_token": "plaintext"}`}
	p := editorPlugin(t, studio)

	// Create a policy, bind it, and save; the token comes back masked
	cfg := map[string]interface{}{
		"mode":           "enforce",
		"policies":       map[string]interface{}{"agents": map[string]interface{}{"level": 2, "protect_patterns": []string{"[A-Z]+-[0-9]+"}}},
		"bindings":       map[string]interface{}{"llm_id:5": "agents", "app:9": "off"},
		"engine_h_url":   "$SECRET/u",
		"engine_h_token": maskedToken,
	}
	var res saveResult
	if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": cfg, "_service_broker_id": 3}, &res); err != nil || !res.Saved {
		t.Fatalf("saveConfig = %+v, %v", res, err)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(studio.stored), &stored); err != nil {
		t.Fatal(err)
	}
	if stored["engine_h_token"] != "plaintext" {
		t.Errorf("the masked token was stored instead of the original: %v", stored["engine_h_token"])
	}
	if stored["mode"] != "enforce" || stored["bindings"].(map[string]interface{})["llm_id:5"] != "agents" {
		t.Errorf("stored = %v", stored)
	}

	// Edit, then delete the policy and its binding
	cfg["policies"] = map[string]interface{}{"agents": map[string]interface{}{"level": 1}}
	if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": cfg}, &res); err != nil || !res.Saved {
		t.Fatalf("edit: %+v, %v", res, err)
	}
	cfg["policies"] = map[string]interface{}{}
	cfg["bindings"] = map[string]interface{}{"app:9": "off"}
	if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": cfg}, &res); err != nil || !res.Saved {
		t.Fatalf("delete: %+v, %v", res, err)
	}
	if len(studio.updates) != 3 || strings.Contains(studio.stored, "agents") {
		t.Errorf("%d updates, stored %s", len(studio.updates), studio.stored)
	}
}

func TestSaveConfigRejectsInvalidConfigWithAMessage(t *testing.T) {
	studio := &fakeStudio{stored: `{}`}
	p := editorPlugin(t, studio)
	for name, cfg := range map[string]string{
		"bad regex":      `{"policies": {"p": {"protect_patterns": ["[a-"]}}}`,
		"unknown policy": `{"bindings": {"llm_id:1": "nope"}}`,
		"unknown key":    `{"log_text_samples": true}`,
	} {
		var res saveResult
		if err := callRPC(t, p, "saveConfig", map[string]json.RawMessage{"config": json.RawMessage(cfg)}, &res); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Saved || len(res.Errors) == 0 {
			t.Errorf("%s: saveConfig = %+v, want rejected with a message", name, res)
		}
	}
	if len(studio.updates) != 0 {
		t.Errorf("an invalid config was stored: %v", studio.updates)
	}

	// The host refusing the update is reported
	studio.updateErr = errors.New("failed to update plugin config: database is locked")
	var res saveResult
	if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": map[string]interface{}{"mode": "off"}}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Saved || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "database is locked") {
		t.Errorf("host refusal = %+v", res)
	}
	if err := callRPC(t, p, "saveConfig", map[string]interface{}{}, &res); err == nil {
		t.Error("saveConfig without a config did not fail")
	}
}

// eac.45: the editor owns only the policy keys. A policies:write user must
// not repoint an engine URL and keep the stored token, nor add or remove
// engine keys.
func TestSaveConfigRefusesEngineAndOtherNonEditorKeys(t *testing.T) {
	const stored = `{"mode": "audit", "engine_main_url": "http://10.0.0.5:8787", "engine_main_token": "$SECRET/ENGINE_TOKEN",
	  "engine_z_url": "http://10.0.0.6:8787", "engine_z_token": "plaintext-z"}`
	// What getEditor showed: the literal token masked, the reference as is
	shown := func() map[string]interface{} {
		return map[string]interface{}{
			"mode": "audit", "engine_main_url": "http://10.0.0.5:8787", "engine_main_token": "$SECRET/ENGINE_TOKEN",
			"engine_z_url": "http://10.0.0.6:8787", "engine_z_token": maskedToken,
		}
	}
	cases := map[string]struct {
		edit     func(c map[string]interface{})
		wantText string
	}{
		"repoint url, masked token": {func(c map[string]interface{}) {
			c["engine_z_url"] = "https://attacker.example"
		}, "engine_z_url: the policy editor cannot change"},
		"repoint url, kept reference": {func(c map[string]interface{}) {
			c["engine_main_url"] = "https://attacker.example"
		}, "engine_main_url: the policy editor cannot change"},
		"replace a reference": {func(c map[string]interface{}) {
			c["engine_main_token"] = "$SECRET/OTHER"
		}, "engine_main_token: the policy editor cannot change"},
		"unmask a token": {func(c map[string]interface{}) {
			c["engine_z_token"] = "guess"
		}, "engine_z_token: the policy editor cannot change"},
		"add a literal key": {func(c map[string]interface{}) {
			c["engine_y_url"] = "https://attacker.example"
			c["engine_y_token"] = "literal"
		}, "engine_y_token: the policy editor cannot add"},
		"delete a key": {func(c map[string]interface{}) {
			delete(c, "engine_z_url")
		}, "engine_z_url: the policy editor cannot remove"},
		"add another key": {func(c map[string]interface{}) {
			c["log_text_samples"] = true
		}, "log_text_samples: the policy editor cannot add"},
	}
	for name, tc := range cases {
		studio := &fakeStudio{stored: stored}
		p := editorPlugin(t, studio)
		cfg := shown()
		cfg["policies"] = map[string]interface{}{"agents": map[string]interface{}{"level": 2}}
		tc.edit(cfg)
		var res saveResult
		if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": cfg}, &res); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Saved || !strings.Contains(strings.Join(res.Errors, "\n"), tc.wantText) {
			t.Errorf("%s: saveConfig = %+v, want refused with %q", name, res, tc.wantText)
		}
		if len(studio.updates) != 0 || studio.stored != stored {
			t.Errorf("%s: the stored config changed: %s", name, studio.stored)
		}
	}

	// A refusal does not hide the policy problems in the same request
	{
		studio := &fakeStudio{stored: stored}
		p := editorPlugin(t, studio)
		cfg := map[string]interface{}{"bindings": map[string]interface{}{"llm_id:1": "no-such-policy"}}
		var res saveResult
		if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": cfg}, &res); err != nil {
			t.Fatal(err)
		}
		all := strings.Join(res.Errors, "\n")
		if res.Saved || !strings.Contains(all, "no-such-policy") || !strings.Contains(all, "engine_main_url: the policy editor cannot remove") {
			t.Errorf("saveConfig = %+v, want both the unknown policy and the removed keys reported", res)
		}
		if len(studio.updates) != 0 {
			t.Error("a refused config was stored")
		}
	}

	// Unchanged, as shown: the policy change saves and every other key is
	// stored exactly as it was, the literal token included
	studio := &fakeStudio{stored: stored}
	p := editorPlugin(t, studio)
	cfg := shown()
	cfg["mode"] = "enforce"
	cfg["policies"] = map[string]interface{}{"agents": map[string]interface{}{"level": 2}}
	var res saveResult
	if err := callRPC(t, p, "saveConfig", map[string]interface{}{"config": cfg}, &res); err != nil || !res.Saved {
		t.Fatalf("unchanged engine keys: %+v, %v", res, err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(studio.stored), &got); err != nil {
		t.Fatal(err)
	}
	var want map[string]interface{}
	_ = json.Unmarshal([]byte(stored), &want)
	for k, v := range want {
		if k == "mode" {
			continue
		}
		if !sameJSON(got[k], v) {
			t.Errorf("%s stored as %v, want %v", k, got[k], v)
		}
	}
	if got["mode"] != "enforce" || got["policies"] == nil {
		t.Errorf("the policy change was not saved: %v", got)
	}
}

func TestEditorNeedsStudio(t *testing.T) {
	p := NewPromptCompressorPlugin()
	_ = p.Initialize(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway}, map[string]string{"_plugin_id": "7"})
	if _, err := p.HandleRPC("getEditor", nil); err == nil {
		t.Error("getEditor worked without the Studio service API")
	}
	if _, err := p.HandleRPC("saveConfig", []byte(`{"config": {}}`)); err == nil {
		t.Error("saveConfig worked without the Studio service API")
	}
}

// Every RPC method the UI calls has a permission in the manifest, and the
// editor's write is gated by policies:write.
func TestManifestGatesEveryUIRPC(t *testing.T) {
	var m models.PluginManifest
	if err := json.Unmarshal(manifestFile, &m); err != nil {
		t.Fatal(err)
	}
	if m.RBAC == nil {
		t.Fatal("manifest has no rbac block")
	}
	if got := m.RBAC.RPCMethods["saveConfig"]; got != "policies:write" {
		t.Errorf("saveConfig needs %q, want policies:write", got)
	}
	if got := m.RBAC.RPCMethods["getEditor"]; got != "policies:read" {
		t.Errorf("getEditor needs %q, want policies:read", got)
	}
	found := false
	for _, r := range m.RBAC.Resources {
		found = found || r.Key == "policies"
	}
	if !found {
		t.Error("manifest does not declare the policies resource")
	}

	call := regexp.MustCompile(`pluginAPI\.call\(\s*'([A-Za-z]+)'`)
	files, _ := filepath.Glob("ui/webc/*.js")
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range call.FindAllStringSubmatch(string(src), -1) {
			if _, ok := m.RBAC.RPCMethods[match[1]]; !ok {
				t.Errorf("%s calls %s, which has no rbac.rpc_methods entry (it would need the base write)", f, match[1])
			}
			if _, err := NewPromptCompressorPlugin().HandleRPC(match[1], []byte(`{}`)); err != nil && strings.Contains(err.Error(), "unknown RPC method") {
				t.Errorf("%s calls %s, which HandleRPC does not know", f, match[1])
			}
		}
	}
	for _, asset := range m.Assets {
		if _, _, err := NewPromptCompressorPlugin().GetAsset(asset); err != nil {
			t.Errorf("manifest asset %s is not served: %v", asset, err)
		}
	}
}
