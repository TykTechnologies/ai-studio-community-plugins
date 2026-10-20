package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	studiopb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
)

// pluginState is the configuration the hooks read. Initialize replaces it
// as a whole, so a request sees one consistent config.
type pluginState struct {
	config *config.Config
	// configErr is why the admin's config was rejected; the plugin then
	// runs with compression off.
	configErr error
	apps      *config.AppPolicyCache
}

// loadConfig parses the host's config map. An invalid config turns
// compression off rather than failing to load: the plugin must never break
// requests, and an edge cannot report a load failure to the admin.
func loadConfig(m map[string]string) (*config.Config, error) {
	c, err := config.Parse(m)
	if err != nil {
		off := config.Default()
		off.Mode = config.ModeOff
		return off, err
	}
	return c, nil
}

// state returns the current configuration. Before Initialize it is the
// defaults with compression off.
func (p *PromptCompressorPlugin) state() *pluginState {
	if s := p.current.Load(); s != nil {
		return s
	}
	return initialState
}

var initialState = func() *pluginState {
	c := config.Default()
	c.Mode = config.ModeOff
	return &pluginState{config: c}
}()

// resolvePolicy picks the policy for a request from the post_auth context:
// the App metadata override, then the bindings, then the default policy. A
// failed App lookup reads as no override, so it never fails the request.
func (p *PromptCompressorPlugin) resolvePolicy(ctx plugin_sdk.Context) config.Resolution {
	s := p.state()
	var appPolicy string
	if ctx.AppID != 0 && s.apps != nil {
		appPolicy = s.apps.Policy(withServices(ctx), ctx.AppID)
	}
	return s.config.Resolve(config.Target{AppID: ctx.AppID, LLMID: ctx.LLMID, LLMSlug: ctx.LLMSlug}, appPolicy)
}

// servicesKey carries the request's plugin_sdk.Context through the
// fetcher's context.Context.
type servicesKey struct{}

func withServices(ctx plugin_sdk.Context) context.Context {
	base := ctx.Context
	if base == nil {
		base = context.Background()
	}
	return context.WithValue(base, servicesKey{}, ctx)
}

// rpcValidateConfig checks a config the admin is editing, so the editor can
// show what the plugin would reject before it is saved. The payload is
// {"config": {...}}; the answer lists one message per problem.
func rpcValidateConfig(payload []byte) ([]byte, error) {
	var req struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || len(req.Config) == 0 {
		return nil, errors.New(`validateConfig: payload must be {"config": {...}}`)
	}
	problems := []string{}
	if _, err := config.ParseJSON(req.Config); err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			if line != "" {
				problems = append(problems, line)
			}
		}
	}
	return json.Marshal(map[string]interface{}{"valid": len(problems) == 0, "errors": problems})
}

// newAppMetadataLookup caches App metadata, fetched in the background.
func (p *PromptCompressorPlugin) newAppMetadataLookup() *asyncLookup[uint32, string] {
	return newAsyncLookup(config.DefaultAppPolicyTTL, config.DefaultAppPolicyErrorTTL, config.DefaultAppPolicyMax, func(appID uint32) (string, error) {
		return fetchAppMetadata(withServices(p.backgroundContext()), appID)
	})
}

// appMetadata is the AppPolicyCache fetcher: the App's metadata when it is
// known, errLookupPending (no override, briefly cached) while it is
// fetched. It waits for the host at most lookupWait.
func (p *PromptCompressorPlugin) appMetadata(_ context.Context, appID uint32) (string, error) {
	if md, ok := p.appMeta.getWait(appID, lookupWait); ok {
		return md, nil
	}
	return "", errLookupPending
}

// fetchAppMetadata reads an App's metadata JSON through the host. It needs
// the apps.read scope; until the admin approves it, the call fails and the
// App has no override.
func fetchAppMetadata(c context.Context, appID uint32) (string, error) {
	ctx, ok := c.Value(servicesKey{}).(plugin_sdk.Context)
	if !ok || ctx.Services == nil {
		return "", errors.New("no service broker")
	}
	if ctx.Runtime == plugin_sdk.RuntimeGateway {
		gw := ctx.Services.Gateway()
		if gw == nil {
			return "", errors.New("gateway services unavailable")
		}
		resp, err := gw.GetApp(c, appID)
		if err != nil {
			return "", err
		}
		if r, ok := resp.(*mgmtpb.GetAppResponse); ok {
			return r.GetApp().GetMetadata(), nil
		}
		return "", errors.New("unexpected GetApp response")
	}
	st := ctx.Services.Studio()
	if st == nil {
		return "", errors.New("studio services unavailable")
	}
	resp, err := st.GetApp(c, appID)
	if err != nil {
		return "", err
	}
	if r, ok := resp.(*studiopb.GetAppResponse); ok {
		return r.GetApp().GetMetadata(), nil
	}
	return "", errors.New("unexpected GetApp response")
}
