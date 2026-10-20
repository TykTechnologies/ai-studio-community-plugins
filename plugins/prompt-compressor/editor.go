package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	studiopb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
	"github.com/gosimple/slug"
)

// The policy editor (Studio UI) reads the stored config, the LLMs and the
// Apps it can bind, and saves the config through the host's plugin config
// update. Edges get the change with the next configuration push to them;
// they do not pull it.

// studioAPI is what the editor needs from Studio. The plugin's service
// broker provides it; tests use a fake.
type studioAPI interface {
	GetPluginConfig(ctx context.Context, pluginID uint32) (string, error)
	UpdatePluginConfig(ctx context.Context, pluginID uint32, configJSON string) error
	ListLLMs(ctx context.Context, page, limit int32) ([]*studiopb.LLMInfo, int64, error)
	ListApps(ctx context.Context, page, limit int32) ([]*studiopb.AppInfo, int64, error)
}

// Paging through the management API. Its pages hold at most 100 rows.
const (
	listPageSize = 100
	maxListRows  = 5000
	editorRPCTTL = 30 * time.Second
)

// maskedToken stands for a stored engine token the editor must not see.
// Saving it back keeps the stored value.
const maskedToken = "********"

var engineTokenKey = regexp.MustCompile(`^engine_[a-z0-9]+(?:_[a-z0-9]+)*_token$`)

type llmOption struct {
	ID     uint32 `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Vendor string `json:"vendor"`
	Active bool   `json:"active"`
	// HasRequestFilters says whether the LLM has request filters (level 3
	// and engines are then skipped unless lossy_with_request_filters).
	// FiltersKnown is false on a host too old to report it.
	HasRequestFilters bool `json:"has_request_filters"`
	FiltersKnown      bool `json:"filters_known"`
}

// llmOptionFrom reads an LLM row. The slug and has_request_filters fields
// are new in the host's LLMInfo, so they are read by name: the plugin then
// builds against hosts with and without them. Without them the slug is
// computed the way the gateway does today (slug.Make of the name). The
// filter state is known only when the host set the field (it is optional,
// so an older host's LLMInfo has none).
func llmOptionFrom(l *studiopb.LLMInfo) llmOption {
	o := llmOption{ID: l.GetId(), Name: l.GetName(), Vendor: l.GetVendor(), Active: l.GetActive()}
	msg := l.ProtoReflect()
	fields := msg.Descriptor().Fields()
	if f := fields.ByName("slug"); f != nil {
		o.Slug = msg.Get(f).String()
	}
	if f := fields.ByName("has_request_filters"); f != nil && msg.Has(f) {
		o.HasRequestFilters = msg.Get(f).Bool()
		o.FiltersKnown = true
	}
	if o.Slug == "" {
		o.Slug = slug.Make(l.GetName())
	}
	return o
}

type appOption struct {
	ID     uint32 `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type editorView struct {
	Config        map[string]interface{} `json:"config"`
	LLMs          []llmOption            `json:"llms"`
	Apps          []appOption            `json:"apps"`
	LLMsTruncated bool                   `json:"llms_truncated"`
	AppsTruncated bool                   `json:"apps_truncated"`
	ConfigError   string                 `json:"config_error"`
	// Problems are what the plugin would reject in the stored config.
	Problems        []string `json:"problems"`
	StrategyVersion int      `json:"strategy_version"`
}

// rpcGetEditor returns everything the policy editor shows.
func (p *PromptCompressorPlugin) rpcGetEditor(api studioAPI, pluginID uint32) (*editorView, error) {
	if api == nil || pluginID == 0 {
		return nil, errors.New("the policy editor needs the Studio service API")
	}
	ctx, cancel := context.WithTimeout(context.Background(), editorRPCTTL)
	defer cancel()

	stored, err := loadStoredConfig(ctx, api, pluginID)
	if err != nil {
		return nil, err
	}
	view := &editorView{Config: maskTokens(stored), StrategyVersion: config.StrategyVersion, Problems: []string{}}
	if raw, err := json.Marshal(stored); err == nil {
		view.Problems = problemsOf(raw)
	}
	if s := p.state(); s.configErr != nil {
		view.ConfigError = s.configErr.Error()
	}

	view.LLMs, view.LLMsTruncated, err = listAllLLMs(ctx, api)
	if err != nil {
		return nil, fmt.Errorf("list LLMs: %w", err)
	}
	view.Apps, view.AppsTruncated, err = listAllApps(ctx, api)
	if err != nil {
		return nil, fmt.Errorf("list Apps: %w", err)
	}
	return view, nil
}

// rpcSaveConfig validates and stores a config from the editor. Nothing is
// stored when the plugin would reject it.
func (p *PromptCompressorPlugin) rpcSaveConfig(api studioAPI, pluginID uint32, payload []byte) (map[string]interface{}, error) {
	if api == nil || pluginID == 0 {
		return nil, errors.New("the policy editor needs the Studio service API")
	}
	var req struct {
		Config map[string]interface{} `json:"config"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || req.Config == nil {
		return nil, errors.New(`saveConfig: payload must be {"config": {...}}`)
	}
	ctx, cancel := context.WithTimeout(context.Background(), editorRPCTTL)
	defer cancel()

	stored, err := loadStoredConfig(ctx, api, pluginID)
	if err != nil {
		return nil, err
	}
	next, refused := editorMerge(req.Config, stored)
	raw, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("saveConfig: %w", err)
	}
	// Report the policy problems with the refusal, so one fix is not hidden
	// behind another
	if problems := append(problemsOf(raw), refused...); len(problems) > 0 {
		return map[string]interface{}{"saved": false, "errors": problems}, nil
	}
	if err := api.UpdatePluginConfig(ctx, pluginID, string(raw)); err != nil {
		return map[string]interface{}{"saved": false, "errors": []string{err.Error()}}, nil
	}
	return map[string]interface{}{"saved": true, "errors": []string{}}, nil
}

// problemsOf lists what ParseJSON rejects in a config, one line each.
func problemsOf(raw []byte) []string {
	problems := []string{}
	if _, err := config.ParseJSON(raw); err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			if line != "" {
				problems = append(problems, line)
			}
		}
	}
	return problems
}

func loadStoredConfig(ctx context.Context, api studioAPI, pluginID uint32) (map[string]interface{}, error) {
	raw, err := api.GetPluginConfig(ctx, pluginID)
	if err != nil {
		return nil, fmt.Errorf("read the plugin config: %w", err)
	}
	stored := map[string]interface{}{}
	if strings.TrimSpace(raw) != "" && strings.TrimSpace(raw) != "null" {
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return nil, fmt.Errorf("the stored plugin config is not a JSON object: %w", err)
		}
	}
	return stored, nil
}

// maskTokens hides stored engine tokens. A $SECRET/ reference is shown: it
// names a secret, it is not one.
func maskTokens(cfg map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		if s, ok := v.(string); ok && engineTokenKey.MatchString(k) && s != "" && !strings.HasPrefix(s, "$SECRET/") {
			v = maskedToken
		}
		out[k] = v
	}
	return out
}

// editorKeys are the top-level settings the policy editor owns. Every other
// key, engine credentials above all, belongs to the host's plugin config
// form, which needs plugin edit rights (and secrets read for $SECRET/
// references). Otherwise a policies:write user could point
// engine_<name>_url elsewhere while the stored token is kept, and every
// edge would send that token and tool outputs there.
var editorKeys = map[string]bool{
	"mode":                       true,
	"default_policy":             true,
	"policies":                   true,
	"bindings":                   true,
	"allow_client_bypass":        true,
	"failure_mode":               true,
	"lossy_with_request_filters": true,
	"max_body_bytes":             true,
	"cache_drop_alert":           true,
}

// editorMerge builds the config to store: the editor's keys from next, every
// other key exactly as stored. A request that changes, adds or removes any
// other key must be refused (refused lists why), so a client that tries is
// told rather than silently overruled; the merged config is still returned
// so its policy problems can be reported in the same answer. next must carry
// those keys as getEditor showed them, tokens masked.
func editorMerge(next, stored map[string]interface{}) (map[string]interface{}, []string) {
	shown := maskTokens(stored)
	var refused []string
	for k, v := range next {
		if editorKeys[k] {
			continue
		}
		prev, ok := shown[k]
		switch {
		case !ok:
			refused = append(refused, fmt.Sprintf("%s: the policy editor cannot add this setting", k))
		case !sameJSON(v, prev):
			refused = append(refused, fmt.Sprintf("%s: the policy editor cannot change this setting", k))
		}
	}
	for k := range shown {
		if _, ok := next[k]; !ok && !editorKeys[k] {
			refused = append(refused, fmt.Sprintf("%s: the policy editor cannot remove this setting", k))
		}
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		refused = append(refused, "Engine credentials and other settings outside the policy editor are changed in the plugin's configuration form.")
	}
	out := make(map[string]interface{}, len(next)+len(stored))
	for k, v := range stored {
		if !editorKeys[k] {
			out[k] = v
		}
	}
	for k, v := range next {
		if editorKeys[k] {
			out[k] = v
		}
	}
	return out, refused
}

// sameJSON compares two decoded JSON values by their encoding.
func sameJSON(a, b interface{}) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

func listAllLLMs(ctx context.Context, api studioAPI) ([]llmOption, bool, error) {
	out := []llmOption{}
	for page := int32(1); len(out) < maxListRows; page++ {
		rows, total, err := api.ListLLMs(ctx, page, listPageSize)
		if err != nil {
			return nil, false, err
		}
		for _, l := range rows {
			out = append(out, llmOptionFrom(l))
		}
		if len(rows) < listPageSize || int64(len(out)) >= total {
			sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
			return out, false, nil
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, true, nil
}

func listAllApps(ctx context.Context, api studioAPI) ([]appOption, bool, error) {
	out := []appOption{}
	for page := int32(1); len(out) < maxListRows; page++ {
		rows, total, err := api.ListApps(ctx, page, listPageSize)
		if err != nil {
			return nil, false, err
		}
		for _, a := range rows {
			out = append(out, appOption{ID: a.GetId(), Name: a.GetName(), Active: a.GetIsActive()})
		}
		if len(rows) < listPageSize || int64(len(out)) >= total {
			sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
			return out, false, nil
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, true, nil
}
