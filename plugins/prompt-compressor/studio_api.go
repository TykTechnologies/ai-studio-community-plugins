package main

import (
	"context"
	"errors"

	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	studiopb "github.com/TykTechnologies/midsommar/v2/proto/ai_studio_management"
)

// brokerStudioAPI is the editor's studioAPI over the plugin's service
// broker. Reading and updating its own config needs no scope; listing LLMs
// needs llms.read and listing Apps apps.read.
type brokerStudioAPI struct {
	studio plugin_sdk.StudioServices
}

func (a brokerStudioAPI) GetPluginConfig(ctx context.Context, pluginID uint32) (string, error) {
	resp, err := ai_studio_sdk.GetPlugin(ctx, pluginID)
	if err != nil {
		return "", err
	}
	if resp.GetPlugin() == nil {
		return "", errors.New("plugin not found")
	}
	return resp.GetPlugin().GetConfigJson(), nil
}

func (a brokerStudioAPI) UpdatePluginConfig(ctx context.Context, pluginID uint32, configJSON string) error {
	ok, msg, err := a.studio.UpdatePluginConfig(ctx, pluginID, configJSON)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New(msg)
	}
	return nil
}

func (a brokerStudioAPI) ListLLMs(ctx context.Context, page, limit int32) ([]*studiopb.LLMInfo, int64, error) {
	resp, err := a.studio.ListLLMs(ctx, page, limit)
	if err != nil {
		return nil, 0, err
	}
	r, ok := resp.(*studiopb.ListLLMsResponse)
	if !ok {
		return nil, 0, errors.New("unexpected ListLLMs response")
	}
	return r.GetLlms(), r.GetTotalCount(), nil
}

func (a brokerStudioAPI) ListApps(ctx context.Context, page, limit int32) ([]*studiopb.AppInfo, int64, error) {
	resp, err := a.studio.ListApps(ctx, page, limit)
	if err != nil {
		return nil, 0, err
	}
	r, ok := resp.(*studiopb.ListAppsResponse)
	if !ok {
		return nil, 0, errors.New("unexpected ListApps response")
	}
	return r.GetApps(), r.GetTotalCount(), nil
}

// studioAPI returns the editor's Studio API, or nil outside Studio.
func (p *PromptCompressorPlugin) studioAPI() studioAPI {
	if p.testStudio != nil {
		return p.testStudio
	}
	p.mu.RLock()
	runtime, services := p.runtime, p.services
	p.mu.RUnlock()
	if runtime != plugin_sdk.RuntimeStudio || services == nil || services.Studio() == nil {
		return nil
	}
	return brokerStudioAPI{studio: services.Studio()}
}
