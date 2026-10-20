package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
)

// fakeBroker is a service broker with only what the policy lookup uses.
type fakeBroker struct {
	plugin_sdk.ServiceBroker
	gateway *fakeGateway
}

func (b *fakeBroker) Gateway() plugin_sdk.GatewayServices { return b.gateway }
func (b *fakeBroker) Logger() plugin_sdk.LogService       { return nil }

type fakeGateway struct {
	plugin_sdk.GatewayServices
	calls    atomic.Int32
	metadata map[uint32]string
}

func (g *fakeGateway) GetApp(ctx context.Context, appID uint32) (interface{}, error) {
	g.calls.Add(1)
	md, ok := g.metadata[appID]
	if !ok {
		return nil, errors.New("rpc error: code = PermissionDenied desc = scope apps.read not granted")
	}
	return &mgmtpb.GetAppResponse{App: &mgmtpb.AppInfo{Id: appID, Metadata: md}}, nil
}

const testConfig = `{
  "mode": "enforce",
  "policies": {"max": {"level": 3}, "agents": {"level": 2}},
  "bindings": {"llm:claude": "agents", "app:9": "max"}
}`

func gatewayPlugin(t *testing.T, configJSON string, gw *fakeGateway) (*PromptCompressorPlugin, plugin_sdk.Context) {
	t.Helper()
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, Services: &fakeBroker{gateway: gw}, Context: context.Background()}
	p := NewPromptCompressorPlugin()
	if err := p.Initialize(ctx, hostConfig(t, configJSON)); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return p, ctx
}

// hostConfig flattens a JSON config the way the host does for Initialize.
func hostConfig(t *testing.T, raw string) map[string]string {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		t.Fatalf("test config: %v", err)
	}
	m := map[string]string{"_plugin_id": "1"}
	for k, v := range obj {
		var s string
		if json.Unmarshal(v, &s) == nil {
			m[k] = s
		} else {
			m[k] = string(v)
		}
	}
	return m
}

func TestInitializeAppliesTheConfig(t *testing.T) {
	p, _ := gatewayPlugin(t, testConfig, &fakeGateway{})
	s := p.state()
	if s.configErr != nil || s.config.Mode != config.ModeEnforce || len(s.config.Policies) != 2 {
		t.Fatalf("state = %+v, err %v", s.config, s.configErr)
	}
}

func TestInvalidConfigTurnsCompressionOff(t *testing.T) {
	p, _ := gatewayPlugin(t, `{"mode": "enforce", "bindings": {"llm:claude": "missing"}}`, &fakeGateway{})
	s := p.state()
	if s.configErr == nil || s.config.Mode != config.ModeOff {
		t.Fatalf("invalid config: mode %s, err %v; want off with the error kept", s.config.Mode, s.configErr)
	}
	out, err := p.HandleRPC("getStatus", nil)
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]interface{}
	_ = json.Unmarshal(out, &status)
	if status["mode"] != "off" || status["config_error"] == "" {
		t.Errorf("getStatus = %v, want mode off and the config error", status)
	}
}

func TestStateBeforeInitializeIsOff(t *testing.T) {
	p := NewPromptCompressorPlugin()
	if p.state().config.Mode != config.ModeOff {
		t.Error("an uninitialised plugin would compress")
	}
}

func TestResolvePolicyReadsAppMetadata(t *testing.T) {
	gw := &fakeGateway{metadata: map[uint32]string{
		1: `{"prompt_compressor.policy": "off"}`,
		2: `{"prompt_compressor": {"policy": "max"}}`,
		3: `{}`,
	}}
	p, base := gatewayPlugin(t, testConfig, gw)
	cases := []struct {
		app        uint32
		llmSlug    string
		wantPolicy string
		wantSource config.Source
	}{
		{1, "claude", "off", config.SourceAppMetadata},
		{2, "claude", "max", config.SourceAppMetadata},
		{3, "claude", "agents", config.SourceLLMBinding},
		{9, "claude", "max", config.SourceAppBinding},    // GetApp fails: bindings still apply
		{10, "gpt", "default", config.SourceDefault},     // GetApp fails, nothing bound
		{0, "claude", "agents", config.SourceLLMBinding}, // no App: no lookup
	}
	for _, tc := range cases {
		ctx := base
		ctx.AppID, ctx.LLMSlug, ctx.LLMID = tc.app, tc.llmSlug, 4
		r := p.resolvePolicy(ctx)
		if r.Name != tc.wantPolicy || r.Source != tc.wantSource {
			t.Errorf("App %d: %s from %s, want %s from %s", tc.app, r.Name, r.Source, tc.wantPolicy, tc.wantSource)
		}
	}
	before := gw.calls.Load()
	ctx := base
	ctx.AppID = 1
	p.resolvePolicy(ctx)
	if gw.calls.Load() != before {
		t.Error("App metadata was fetched again within the cache TTL")
	}
}

func TestResolvePolicyWithoutServices(t *testing.T) {
	p := NewPromptCompressorPlugin()
	if err := p.Initialize(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway}, hostConfig(t, testConfig)); err != nil {
		t.Fatal(err)
	}
	r := p.resolvePolicy(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, AppID: 5, LLMSlug: "claude"})
	if r.Name != "agents" {
		t.Errorf("without a broker resolved %s, want the llm binding", r.Name)
	}
}

func TestValidateConfigRPC(t *testing.T) {
	p := NewPromptCompressorPlugin()
	for _, tc := range []struct {
		payload   string
		valid     bool
		minErrors int
	}{
		{`{"config": ` + testConfig + `, "_service_broker_id": 3}`, true, 0},
		{`{"config": {"mode": "on", "bindings": {"llm:a": "nope"}}}`, false, 2},
	} {
		out, err := p.HandleRPC("validateConfig", []byte(tc.payload))
		if err != nil {
			t.Fatalf("validateConfig: %v", err)
		}
		var res struct {
			Valid  bool     `json:"valid"`
			Errors []string `json:"errors"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			t.Fatal(err)
		}
		if res.Valid != tc.valid || len(res.Errors) < tc.minErrors || res.Errors == nil {
			t.Errorf("validateConfig(%s) = %+v", tc.payload, res)
		}
	}
	if _, err := p.HandleRPC("validateConfig", []byte(`{}`)); err == nil {
		t.Error("validateConfig without a config did not fail")
	}
}
