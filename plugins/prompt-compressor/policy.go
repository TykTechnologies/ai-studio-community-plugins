package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	studiopb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
	mgmtpb "github.com/TykTechnologies/midsommar/v2/proto/microgateway_management"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// pluginState is the configuration the hooks read. Initialize replaces it
// as a whole, so a request sees one consistent config.
type pluginState struct {
	config *config.Config
	// configErr is why the admin's config was rejected; the plugin then
	// runs with compression off.
	configErr error
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
// the App metadata override, then the bindings, then the default policy.
//
// known is false when the request has an App whose metadata the plugin
// does not have (the lookup is pending or failing): the App may have opted
// out, so the request must not be compressed. Once the metadata is known a
// stale copy is used while it is refreshed.
func (p *PromptCompressorPlugin) resolvePolicy(ctx plugin_sdk.Context) (res config.Resolution, known bool) {
	s := p.state()
	var appPolicy string
	if ctx.AppID != 0 {
		md, ok := p.appMeta.getWait(ctx.AppID, lookupWait)
		if !ok {
			return config.Resolution{}, false
		}
		appPolicy = config.AppPolicyFromMetadata(md)
	}
	return s.config.Resolve(config.Target{AppID: ctx.AppID, LLMID: ctx.LLMID, LLMSlug: ctx.LLMSlug}, appPolicy), true
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

// App metadata cache settings.
const (
	appMetadataTTL      = 30 * time.Second
	appMetadataErrorTTL = 5 * time.Second
	appMetadataMaxSize  = 10000
)

// newAppMetadataLookup caches App metadata, fetched in the background. An
// App the host does not have (NotFound) has no override; any other failure
// leaves the metadata unknown.
func (p *PromptCompressorPlugin) newAppMetadataLookup() *asyncLookup[uint32, string] {
	return newAsyncLookup(appMetadataTTL, appMetadataErrorTTL, appMetadataMaxSize, func(appID uint32) (string, error) {
		md, err := fetchAppMetadata(withServices(p.backgroundContext()), appID)
		if status.Code(err) == codes.NotFound {
			return "", nil
		}
		return md, err
	})
}

// fetchAppMetadata reads an App's metadata JSON through the host. It needs
// the apps.read scope; until the admin approves it, the call fails and the
// App's requests are not compressed (resolvePolicy).
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
