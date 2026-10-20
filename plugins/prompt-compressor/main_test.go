package main

import (
	"encoding/json"
	"testing"

	"github.com/TykTechnologies/midsommar/v2/models"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

func initializedPlugin(t *testing.T, runtime plugin_sdk.RuntimeType) *PromptCompressorPlugin {
	t.Helper()
	p := NewPromptCompressorPlugin()
	if err := p.Initialize(plugin_sdk.Context{Runtime: runtime}, map[string]string{}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return p
}

func TestManifestIsValidAndDeclaresTheHooksAndScopes(t *testing.T) {
	var m models.PluginManifest
	if err := json.Unmarshal(manifestFile, &m); err != nil {
		t.Fatalf("manifest.json does not parse: %v", err)
	}
	if err := m.ValidateManifest(); err != nil {
		t.Fatalf("manifest.json is invalid: %v", err)
	}
	if m.Version != PluginVersion {
		t.Errorf("manifest version %q, PluginVersion %q: bump both together", m.Version, PluginVersion)
	}
	// The manifest names the response hook on_response (the SDK's
	// HookTypeResponse is "response")
	for _, hook := range []string{"studio_ui", "post_auth", "on_response"} {
		found := false
		for _, h := range m.Capabilities.Hooks {
			found = found || h == hook
		}
		if !found {
			t.Errorf("manifest does not declare hook %q", hook)
		}
	}
	if m.Capabilities.PrimaryHook != "post_auth" {
		t.Errorf("primary_hook = %q, want post_auth", m.Capabilities.PrimaryHook)
	}
	for _, scope := range []string{"kv.readwrite", "control.send", "pricing.read", "apps.read"} {
		if !m.HasServiceScope(scope) {
			t.Errorf("manifest does not request service scope %q", scope)
		}
	}
}

func TestConfigSchemaIsAJSONSchemaObject(t *testing.T) {
	p := NewPromptCompressorPlugin()
	raw, err := p.GetConfigSchema()
	if err != nil {
		t.Fatalf("GetConfigSchema: %v", err)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("config schema does not parse: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("config schema type = %v, want object", schema["type"])
	}
}

func TestPostAuthPassesTheRequestThroughUntouched(t *testing.T) {
	for _, runtime := range []plugin_sdk.RuntimeType{plugin_sdk.RuntimeGateway, plugin_sdk.RuntimeStudio} {
		p := initializedPlugin(t, runtime)
		body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
		req := &pb.EnrichedRequest{Request: &pb.PluginRequest{
			Method:  "POST",
			Path:    "/llm/rest/openai/v1/chat/completions",
			Body:    body,
			Headers: map[string]string{"Content-Type": "application/json"},
			Context: &pb.PluginContext{RequestId: "r1", AppId: 1, LlmId: 1},
		}}
		resp, err := p.HandlePostAuth(plugin_sdk.Context{Runtime: runtime}, req)
		if err != nil {
			t.Fatalf("%s: HandlePostAuth: %v", runtime, err)
		}
		if resp.Modified || resp.Block || resp.Body != nil || resp.Headers != nil {
			t.Errorf("%s: HandlePostAuth = %+v, want an unmodified pass-through with no headers", runtime, resp)
		}
		if string(req.Request.Body) != string(body) {
			t.Errorf("%s: HandlePostAuth changed the request body", runtime)
		}
	}
}

func TestResponseHooksAreNoOps(t *testing.T) {
	p := initializedPlugin(t, plugin_sdk.RuntimeGateway)
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway}
	pctx := &pb.PluginContext{RequestId: "r1"}

	h, err := p.OnBeforeWriteHeaders(ctx, &pb.HeadersRequest{Headers: map[string]string{"X-A": "1"}, Context: pctx})
	if err != nil || h.Modified || h.Headers != nil {
		t.Errorf("OnBeforeWriteHeaders = %+v, %v; want unmodified", h, err)
	}
	w, err := p.OnBeforeWrite(ctx, &pb.ResponseWriteRequest{Body: []byte(`{}`), Context: pctx})
	if err != nil || w.Modified || w.Body != nil {
		t.Errorf("OnBeforeWrite = %+v, %v; want unmodified", w, err)
	}
	s, err := p.OnStreamComplete(ctx, &pb.StreamCompleteRequest{Context: pctx})
	if err != nil || s.Handled {
		t.Errorf("OnStreamComplete = %+v, %v; want not handled", s, err)
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	p := initializedPlugin(t, plugin_sdk.RuntimeGateway)
	// The gateway host initialises twice
	if err := p.Initialize(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway}, map[string]string{}); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := p.Shutdown(plugin_sdk.Context{}); err != nil {
			t.Fatalf("Shutdown %d: %v", i+1, err)
		}
	}
}

func TestGetAssetServesOnlyUIAndAssets(t *testing.T) {
	p := NewPromptCompressorPlugin()
	for _, tc := range []struct {
		path, mime string
	}{
		{"/ui/webc/dashboard.js", "application/javascript"},
		{"assets/icon.svg", "image/svg+xml"},
	} {
		data, mime, err := p.GetAsset(tc.path)
		if err != nil || len(data) == 0 || mime != tc.mime {
			t.Errorf("GetAsset(%q) = %d bytes, %q, %v; want %q", tc.path, len(data), mime, err, tc.mime)
		}
	}
	for _, bad := range []string{"/manifest.json", "config.schema.json", "/ui/../manifest.json", "/nope.js"} {
		if _, _, err := p.GetAsset(bad); err == nil {
			t.Errorf("GetAsset(%q) served a file outside ui/ and assets/", bad)
		}
	}
}

func TestHandleRPC(t *testing.T) {
	p := initializedPlugin(t, plugin_sdk.RuntimeStudio)
	out, err := p.HandleRPC("getStatus", []byte(`{}`))
	if err != nil {
		t.Fatalf("getStatus: %v", err)
	}
	var status map[string]interface{}
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("getStatus returned invalid JSON: %v", err)
	}
	if status["version"] != PluginVersion || status["runtime"] != string(plugin_sdk.RuntimeStudio) {
		t.Errorf("getStatus = %v", status)
	}
	if _, err := p.HandleRPC("nope", nil); err == nil {
		t.Error("an unknown RPC method did not fail")
	}
}
