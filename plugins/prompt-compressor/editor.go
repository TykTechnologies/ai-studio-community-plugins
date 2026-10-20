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
// update. The edges pick the change up on their next config sync.

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
	next := unmaskTokens(req.Config, stored)
	raw, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("saveConfig: %w", err)
	}
	if problems := problemsOf(raw); len(problems) > 0 {
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

// unmaskTokens puts the stored value back for every token the editor sent
// masked.
func unmaskTokens(next, stored map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(next))
	for k, v := range next {
		if s, ok := v.(string); ok && s == maskedToken && engineTokenKey.MatchString(k) {
			if prev, ok := stored[k]; ok {
				v = prev
			}
		}
		out[k] = v
	}
	return out
}

func listAllLLMs(ctx context.Context, api studioAPI) ([]llmOption, bool, error) {
	out := []llmOption{}
	for page := int32(1); len(out) < maxListRows; page++ {
		rows, total, err := api.ListLLMs(ctx, page, listPageSize)
		if err != nil {
			return nil, false, err
		}
		for _, l := range rows {
			out = append(out, llmOption{ID: l.GetId(), Name: l.GetName(), Slug: slug.Make(l.GetName()), Vendor: l.GetVendor(), Active: l.GetActive()})
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
