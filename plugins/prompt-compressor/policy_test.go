package main

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	mu       sync.Mutex
	metadata map[uint32]string
}

// appNotFound is an App ID fakeGateway does not have; appDown one it
// cannot reach.
const (
	appNotFound = 404
	appDown     = 503
)

func (g *fakeGateway) GetApp(ctx context.Context, appID uint32) (interface{}, error) {
	g.calls.Add(1)
	switch appID {
	case appNotFound:
		return nil, status.Error(codes.NotFound, "app not found")
	case appDown:
		return nil, status.Error(codes.Unavailable, "service client unavailable")
	}
	g.mu.Lock()
	md, ok := g.metadata[appID]
	g.mu.Unlock()
	if !ok || md == "" {
		return nil, status.Error(codes.PermissionDenied, "insufficient scope: apps.read")
	}
	return &mgmtpb.GetAppResponse{App: &mgmtpb.AppInfo{Id: appID, Metadata: md}}, nil
}

// setMetadata changes an App's metadata; "" makes GetApp fail.
func (g *fakeGateway) setMetadata(appID uint32, md string) {
	g.mu.Lock()
	g.metadata[appID] = md
	g.mu.Unlock()
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

// primeApp fetches an App's metadata and waits for it, so a test does not
// depend on the fetch finishing within lookupWait.
func primeApp(p *PromptCompressorPlugin, appID uint32) {
	if appID != 0 {
		p.appMeta.get(appID)
		p.appMeta.inflight.Wait()
	}
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
		9: `{}`,
	}}
	p, base := gatewayPlugin(t, testConfig, gw)
	cases := []struct {
		app         uint32
		llmSlug     string
		wantUnknown string
		wantPolicy  string
		wantSource  config.Source
	}{
		{1, "claude", "", "off", config.SourceAppMetadata},
		{2, "claude", "", "max", config.SourceAppMetadata},
		{3, "claude", "", "agents", config.SourceLLMBinding},
		{9, "claude", "", "max", config.SourceAppBinding},
		{10, "gpt", ReasonAppScopeMissing, "", ""},                // apps.read refused: the App may have opted out
		{appDown, "gpt", ReasonAppPolicyUnknown, "", ""},          // host unreachable
		{appNotFound, "gpt", "", "default", config.SourceDefault}, // the host has no such App
		{0, "claude", "", "agents", config.SourceLLMBinding},      // no App: no lookup
	}
	for _, tc := range cases {
		ctx := base
		ctx.AppID, ctx.LLMSlug, ctx.LLMID = tc.app, tc.llmSlug, 4
		primeApp(p, tc.app)
		r, unknown := p.resolvePolicy(ctx)
		if unknown != tc.wantUnknown || r.Name != tc.wantPolicy || r.Source != tc.wantSource {
			t.Errorf("App %d: %s from %s (unknown %q), want %s from %s (unknown %q)", tc.app, r.Name, r.Source, unknown, tc.wantPolicy, tc.wantSource, tc.wantUnknown)
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

// Once an App's opt-out is known it holds while refreshes fail.
func TestResolvePolicyKeepsAStaleOptOut(t *testing.T) {
	gw := &fakeGateway{metadata: map[uint32]string{1: `{"prompt_compressor.policy": "off"}`}}
	p, ctx := gatewayPlugin(t, testConfig, gw)
	now := time.Now()
	var mu sync.Mutex
	p.appMeta.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	ctx.AppID, ctx.LLMSlug = 1, "claude"
	primeApp(p, 1)
	if r, unknown := p.resolvePolicy(ctx); unknown != "" || r.Name != "off" {
		t.Fatalf("got %s (unknown %q)", r.Name, unknown)
	}
	gw.setMetadata(1, "")
	mu.Lock()
	now = now.Add(time.Hour)
	mu.Unlock()
	for i := 0; i < 2; i++ {
		if r, unknown := p.resolvePolicy(ctx); unknown != "" || r.Name != "off" {
			t.Errorf("refresh %d: got %s (unknown %q), want the stale off", i, r.Name, unknown)
		}
		p.appMeta.inflight.Wait()
	}
	if gw.calls.Load() < 2 {
		t.Error("the stale value was not refreshed")
	}
}

// When the host refuses App lookups (apps.read not approved) the App's
// requests are forwarded with reason app_scope_missing, and getStatus says
// why; the warning goes once a lookup succeeds.
func TestAppScopeMissingIsVisible(t *testing.T) {
	gw := &fakeGateway{metadata: map[uint32]string{1: `{}`}}
	p, ctx := gatewayPlugin(t, `{"mode":"enforce"}`, gw)
	ctx.RequestID, ctx.AppID, ctx.Vendor = "req-1", 10, "openai"
	primeApp(p, 10)
	resp, _ := p.HandlePostAuth(ctx, &pb.EnrichedRequest{Request: &pb.PluginRequest{Method: "POST", Path: "/llm/rest/gpt/v1/chat/completions", Body: []byte(`{"messages":[]}`)}})
	rec, _ := p.pending.take("req-1")
	if resp.Modified || rec.Outcome != "skipped" || rec.Report.Reason != ReasonAppScopeMissing {
		t.Fatalf("resp %+v, record %+v", resp, rec)
	}
	warnings := func() []string {
		raw, err := p.HandleRPC("getStatus", nil)
		if err != nil {
			t.Fatal(err)
		}
		var st struct {
			Warnings []string `json:"warnings"`
		}
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatal(err)
		}
		return st.Warnings
	}
	if w := warnings(); len(w) != 1 || w[0] != appScopeMissingStatus {
		t.Errorf("warnings %q", w)
	}
	primeApp(p, 1)
	if w := warnings(); len(w) != 0 {
		t.Errorf("warnings after a successful lookup %q", w)
	}
}

func TestResolvePolicyWithoutServices(t *testing.T) {
	p := NewPromptCompressorPlugin()
	if err := p.Initialize(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway}, hostConfig(t, testConfig)); err != nil {
		t.Fatal(err)
	}
	if _, unknown := p.resolvePolicy(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, AppID: 5, LLMSlug: "claude"}); unknown != ReasonAppPolicyUnknown {
		t.Errorf("without a broker an App's policy was %q", unknown)
	}
	r, unknown := p.resolvePolicy(plugin_sdk.Context{Runtime: plugin_sdk.RuntimeGateway, LLMSlug: "claude"})
	if unknown != "" || r.Name != "agents" {
		t.Errorf("without a broker or App resolved %s, want the llm binding", r.Name)
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
