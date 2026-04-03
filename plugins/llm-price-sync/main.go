package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

const PluginName = "llm-price-sync"

//go:embed ui assets manifest.json config.schema.json
var embeddedAssets embed.FS

type Config struct {
	PricesURL        string   `json:"prices_url"`
	SyncIntervalCron string   `json:"sync_interval_cron"`
	AutoCreateModels bool     `json:"auto_create_models"`
	VendorFilter     []string `json:"vendor_filter"`
	DryRun           bool     `json:"dry_run"`
}

type PriceSyncPlugin struct {
	plugin_sdk.BasePlugin
	pluginID uint32
	config   *Config
	kv       plugin_sdk.KVService
}

func NewPriceSyncPlugin() *PriceSyncPlugin {
	return &PriceSyncPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(PluginName, "1.0.0", "Automatically sync LLM model prices from llm-prices.com"),
		config: &Config{
			PricesURL:        "https://www.llm-prices.com/current-v1.json",
			SyncIntervalCron: "0 */6 * * *",
			AutoCreateModels: true,
		},
	}
}

func (p *PriceSyncPlugin) Initialize(ctx plugin_sdk.Context, configMap map[string]string) error {
	// Extract plugin ID
	if pluginIDStr, ok := configMap["plugin_id"]; ok {
		fmt.Sscanf(pluginIDStr, "%d", &p.pluginID)
		ai_studio_sdk.SetPluginID(p.pluginID)
	}

	// Store KV reference for later use in RPC handlers
	p.kv = ctx.Services.KV()

	// Try nested JSON first ("config" or "plugin_config" key),
	// then fall back to individual flat keys (how Studio actually passes them).
	configJSON := ""
	if v, ok := configMap["config"]; ok && v != "" {
		configJSON = v
	} else if v, ok := configMap["plugin_config"]; ok && v != "" {
		configJSON = v
	}

	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), p.config); err != nil {
			log.Printf("[%s] failed to parse config JSON: %v", PluginName, err)
		}
	} else {
		// Parse individual flat keys
		if v, ok := configMap["prices_url"]; ok && v != "" {
			p.config.PricesURL = v
		}
		if v, ok := configMap["sync_interval_cron"]; ok && v != "" {
			p.config.SyncIntervalCron = v
		}
		if v, ok := configMap["auto_create_models"]; ok {
			p.config.AutoCreateModels = v == "true"
		}
		if v, ok := configMap["dry_run"]; ok {
			p.config.DryRun = v == "true"
		}
		if v, ok := configMap["vendor_filter"]; ok && v != "" {
			var vendors []string
			if err := json.Unmarshal([]byte(v), &vendors); err != nil {
				log.Printf("[%s] failed to parse vendor_filter: %v", PluginName, err)
			} else {
				p.config.VendorFilter = vendors
			}
		}
	}

	log.Printf("[%s] initialized: url=%s cron=%s auto_create=%v dry_run=%v vendors=%v",
		PluginName, p.config.PricesURL, p.config.SyncIntervalCron,
		p.config.AutoCreateModels, p.config.DryRun, p.config.VendorFilter)

	return nil
}

func (p *PriceSyncPlugin) Shutdown(ctx plugin_sdk.Context) error {
	log.Printf("[%s] shutting down", PluginName)
	return nil
}

// --- SessionAware ---

func (p *PriceSyncPlugin) OnSessionReady(ctx plugin_sdk.Context) {
	if ai_studio_sdk.IsInitialized() {
		_, err := ai_studio_sdk.GetPluginsCount(context.Background())
		if err == nil {
			log.Printf("[%s] service API connection established", PluginName)
		} else {
			log.Printf("[%s] service API warmup failed: %v", PluginName, err)
		}
	}

	// Ensure default schedule exists
	p.ensureSchedule()
}

func (p *PriceSyncPlugin) OnSessionClosing(ctx plugin_sdk.Context) {}

func (p *PriceSyncPlugin) ensureSchedule() {
	schedules, err := ai_studio_sdk.ListSchedules(context.Background())
	if err != nil {
		log.Printf("[%s] failed to list schedules: %v", PluginName, err)
		return
	}

	for _, s := range schedules {
		if s.ScheduleId == "price-sync-default" {
			log.Printf("[%s] default schedule already exists", PluginName)
			return
		}
	}

	_, err = ai_studio_sdk.CreateSchedule(
		context.Background(),
		"price-sync-default",
		"LLM Price Sync",
		p.config.SyncIntervalCron,
		"UTC",
		300, // 5 min timeout
		nil,
		true,
	)
	if err != nil {
		log.Printf("[%s] failed to create default schedule: %v", PluginName, err)
	} else {
		log.Printf("[%s] created default schedule: %s", PluginName, p.config.SyncIntervalCron)
	}
}

// --- SchedulerPlugin ---

func (p *PriceSyncPlugin) ExecuteScheduledTask(ctx plugin_sdk.Context, schedule *plugin_sdk.Schedule) error {
	log.Printf("[%s] executing scheduled sync (schedule=%s, vendors=%v)", PluginName, schedule.ID, p.config.VendorFilter)
	result := p.syncPrices(ctx)
	if len(result.Errors) > 0 {
		return fmt.Errorf("sync completed with %d errors: %s", len(result.Errors), result.Errors[0])
	}
	return nil
}

// --- UIProvider ---

func (p *PriceSyncPlugin) GetAsset(assetPath string) ([]byte, string, error) {
	assetPath = strings.TrimPrefix(assetPath, "/")
	content, err := embeddedAssets.ReadFile(assetPath)
	if err != nil {
		return nil, "", fmt.Errorf("asset not found: %s", assetPath)
	}

	mimeType := "application/octet-stream"
	switch {
	case strings.HasSuffix(assetPath, ".js"):
		mimeType = "application/javascript"
	case strings.HasSuffix(assetPath, ".css"):
		mimeType = "text/css"
	case strings.HasSuffix(assetPath, ".svg"):
		mimeType = "image/svg+xml"
	case strings.HasSuffix(assetPath, ".json"):
		mimeType = "application/json"
	}
	return content, mimeType, nil
}

func (p *PriceSyncPlugin) ListAssets(pathPrefix string) ([]*pb.AssetInfo, error) {
	pathPrefix = strings.TrimPrefix(pathPrefix, "/")
	var assets []*pb.AssetInfo

	dirs := []string{"ui/webc", "assets"}
	for _, dir := range dirs {
		if pathPrefix != "" && !strings.HasPrefix(dir, pathPrefix) {
			continue
		}
		entries, err := embeddedAssets.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				assets = append(assets, &pb.AssetInfo{
					Path: "/" + dir + "/" + e.Name(),
				})
			}
		}
	}
	return assets, nil
}

func (p *PriceSyncPlugin) GetManifest() ([]byte, error) {
	return embeddedAssets.ReadFile("manifest.json")
}

func (p *PriceSyncPlugin) GetConfigSchema() ([]byte, error) {
	return embeddedAssets.ReadFile("config.schema.json")
}

// --- RPC ---

func (p *PriceSyncPlugin) HandleRPC(method string, payload []byte) ([]byte, error) {
	switch method {
	case "getStatus":
		return p.rpcGetStatus()
	case "triggerSync":
		return p.rpcTriggerSync()
	case "getSyncHistory":
		return p.rpcGetSyncHistory()
	default:
		return nil, fmt.Errorf("unknown RPC method: %s", method)
	}
}

func (p *PriceSyncPlugin) rpcGetStatus() ([]byte, error) {
	ctx := context.Background()

	data, err := p.kv.Read(ctx, "price-sync:last-result")
	if err != nil {
		return json.Marshal(map[string]interface{}{
			"status": "no_sync_yet",
			"config": p.config,
		})
	}

	var lastResult SyncResult
	json.Unmarshal(data, &lastResult)

	return json.Marshal(map[string]interface{}{
		"status":    "ok",
		"last_sync": lastResult,
		"config":    p.config,
	})
}

func (p *PriceSyncPlugin) rpcTriggerSync() ([]byte, error) {
	log.Printf("[%s] manual sync triggered via RPC (vendors=%v)", PluginName, p.config.VendorFilter)
	result := p.syncPricesFromRPC()

	return json.Marshal(map[string]interface{}{
		"status": "completed",
		"result": result,
	})
}

func (p *PriceSyncPlugin) rpcGetSyncHistory() ([]byte, error) {
	ctx := context.Background()

	data, err := p.kv.Read(ctx, "price-sync:history")
	if err != nil {
		return json.Marshal([]interface{}{})
	}

	return data, nil
}

func main() {
	plugin_sdk.Serve(NewPriceSyncPlugin())
}
