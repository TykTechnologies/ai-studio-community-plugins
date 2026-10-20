package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/records"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// Embed UI assets and manifest into the binary
//
//go:embed ui assets manifest.json config.schema.json
var embeddedAssets embed.FS

//go:embed manifest.json
var manifestFile []byte

//go:embed config.schema.json
var configSchemaFile []byte

const (
	PluginName    = "prompt-compressor"
	PluginVersion = "0.1.0"
)

// PromptCompressorPlugin compresses LLM request bodies on the Microgateway
// (post_auth) and records what it saved (on_response). The Studio side serves
// the admin UI and receives savings records from the edges.
//
// The request hooks always fail open: any error passes the request through
// unchanged.
type PromptCompressorPlugin struct {
	plugin_sdk.BasePlugin

	mu       sync.RWMutex
	runtime  plugin_sdk.RuntimeType
	services plugin_sdk.ServiceBroker

	// pluginID is the plugin's own ID, from the host's config keys.
	pluginID atomic.Uint32

	// current is the configuration the hooks read; see state().
	current atomic.Pointer[pluginState]

	// testStudio replaces the Studio service API in tests.
	testStudio studioAPI

	// The request path (postauth.go): compressFn is the compressor's
	// Compress (replaced in tests), pending holds what post_auth learned
	// for the response hook, llms caches LLM endpoints and appMeta App
	// metadata, both fetched in the background so a request never waits
	// for the host.
	compressFn func(compress.Input) ([]byte, compress.Report)
	pending    *pendingCache
	llms       *asyncLookup[uint32, llmInfo]
	appMeta    *asyncLookup[uint32, string]
	counters   hookCounters
	// appScopeMissing: the host refused the last App lookup (apps.read is
	// not approved), so App traffic is forwarded uncompressed.
	appScopeMissing atomic.Bool

	// rec takes completed requests (response.go); nil is records.Nop.
	rec records.Recorder

	// telemetry sends savings records to Studio and keeps Studio's
	// rollups (telemetry.go).
	telemetry *telemetry
}

// recorder returns the recorder completed requests go to.
func (p *PromptCompressorPlugin) recorder() records.Recorder {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.rec == nil {
		return records.Nop{}
	}
	return p.rec
}

// SetRecorder sets where completed requests go.
func (p *PromptCompressorPlugin) SetRecorder(r records.Recorder) {
	p.mu.Lock()
	p.rec = r
	p.mu.Unlock()
}

// NewPromptCompressorPlugin creates a new plugin instance
func NewPromptCompressorPlugin() *PromptCompressorPlugin {
	p := &PromptCompressorPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(
			PluginName,
			PluginVersion,
			"Prompt Compressor - cache-safe, measured compression of LLM request bodies",
		),
		compressFn: compress.New(compress.Options{}).Compress,
		pending:    newPendingCache(pendingTTL, pendingMaxSize),
		telemetry:  newTelemetry(),
	}
	p.llms = p.newLLMLookup()
	p.appMeta = p.newAppMetadataLookup()
	return p
}

// logger returns the SDK's levelled logger. Before Initialize, and in tests
// that run without a service broker, it falls back to the standard logger.
func (p *PromptCompressorPlugin) logger() plugin_sdk.LogService {
	p.mu.RLock()
	services := p.services
	p.mu.RUnlock()
	if services != nil {
		if l := services.Logger(); l != nil {
			return l
		}
	}
	return stdLogger{}
}

// stdLogger writes in the SDK logger's format while no broker exists.
type stdLogger struct{}

func (stdLogger) Debug(msg string, fields ...interface{}) { stdLog("DEBUG", msg, fields) }
func (stdLogger) Info(msg string, fields ...interface{})  { stdLog("INFO", msg, fields) }
func (stdLogger) Warn(msg string, fields ...interface{})  { stdLog("WARN", msg, fields) }
func (stdLogger) Error(msg string, fields ...interface{}) { stdLog("ERROR", msg, fields) }

func stdLog(level, msg string, fields []interface{}) {
	log.Printf("[%s] %s: %s %v", level, PluginName, msg, fields)
}

// Initialize implements plugin_sdk.Plugin. The gateway host may call it more
// than once.
// An invalid config is logged and turns compression off; it never fails
// the load.
func (p *PromptCompressorPlugin) Initialize(ctx plugin_sdk.Context, configMap map[string]string) error {
	p.mu.Lock()
	p.runtime = ctx.Runtime
	p.services = ctx.Services
	p.mu.Unlock()

	// Studio passes plugin_id, the gateway _plugin_id
	for _, key := range []string{"plugin_id", "_plugin_id"} {
		if id, err := strconv.ParseUint(configMap[key], 10, 32); err == nil && id > 0 {
			p.pluginID.Store(uint32(id))
			break
		}
	}

	cfg, err := loadConfig(configMap)
	p.current.Store(&pluginState{config: cfg, configErr: err})
	if err != nil {
		p.logger().Error("Invalid configuration, compression is off until it is fixed", "error", err.Error())
	} else if len(cfg.Ignored) > 0 {
		p.logger().Warn("Ignoring unknown configuration keys", "keys", strings.Join(cfg.Ignored, ","))
	}

	p.initTelemetry(ctx)

	p.logger().Info("Initialized", "runtime", ctx.Runtime, "version", PluginVersion, "mode", cfg.Mode)
	return nil
}

// Shutdown implements plugin_sdk.Plugin. It is safe to call more than once.
// It sends the savings records still held.
func (p *PromptCompressorPlugin) Shutdown(ctx plugin_sdk.Context) error {
	p.logger().Debug("Shutting down")
	p.stopTelemetry()
	return nil
}

// OnSessionReady implements plugin_sdk.SessionAware. On an edge it starts
// sending savings records, now that the broker is up.
func (p *PromptCompressorPlugin) OnSessionReady(ctx plugin_sdk.Context) {
	p.logger().Debug("Session broker active", "runtime", ctx.Runtime)
	if ctx.Runtime == plugin_sdk.RuntimeGateway {
		p.startTelemetry()
	}
}

// OnSessionClosing implements plugin_sdk.SessionAware
func (p *PromptCompressorPlugin) OnSessionClosing(ctx plugin_sdk.Context) {
	p.logger().Debug("Session closing")
}

// GetManifest implements plugin_sdk.ManifestProvider
func (p *PromptCompressorPlugin) GetManifest() ([]byte, error) {
	return manifestFile, nil
}

// GetConfigSchema implements plugin_sdk.ConfigProvider
func (p *PromptCompressorPlugin) GetConfigSchema() ([]byte, error) {
	return configSchemaFile, nil
}

// assetTypes are the content types of the files the UI serves.
var assetTypes = map[string]string{
	".js":   "application/javascript",
	".css":  "text/css",
	".svg":  "image/svg+xml",
	".json": "application/json",
	".html": "text/html",
}

// GetAsset implements plugin_sdk.UIProvider. Only the ui/ and assets/ trees
// are served.
func (p *PromptCompressorPlugin) GetAsset(assetPath string) ([]byte, string, error) {
	clean := strings.TrimPrefix(path.Clean("/"+assetPath), "/")
	if !strings.HasPrefix(clean, "ui/") && !strings.HasPrefix(clean, "assets/") {
		return nil, "", fmt.Errorf("asset not found: %s", assetPath)
	}
	content, err := embeddedAssets.ReadFile(clean)
	if err != nil {
		return nil, "", fmt.Errorf("asset not found: %s", assetPath)
	}
	mimeType, ok := assetTypes[path.Ext(clean)]
	if !ok {
		mimeType = "application/octet-stream"
	}
	return content, mimeType, nil
}

// ListAssets implements plugin_sdk.UIProvider
func (p *PromptCompressorPlugin) ListAssets(pathPrefix string) ([]*pb.AssetInfo, error) {
	return []*pb.AssetInfo{}, nil
}

// HandleRPC implements plugin_sdk.UIProvider
func (p *PromptCompressorPlugin) HandleRPC(method string, payload []byte) ([]byte, error) {
	switch method {
	case "getStatus":
		p.mu.RLock()
		runtime := p.runtime
		p.mu.RUnlock()
		s := p.state()
		configError := ""
		if s.configErr != nil {
			configError = s.configErr.Error()
		}
		var warnings []string
		if p.appScopeMissing.Load() {
			warnings = append(warnings, appScopeMissingStatus)
		}
		return json.Marshal(map[string]interface{}{
			"plugin":           PluginName,
			"version":          PluginVersion,
			"runtime":          runtime,
			"mode":             s.config.Mode,
			"config_error":     configError,
			"strategy_version": config.StrategyVersion,
			"requests":         p.counters.snapshot(),
			"warnings":         warnings,
		})
	case "validateConfig":
		return rpcValidateConfig(payload)
	case "getEditor":
		view, err := p.rpcGetEditor(p.studioAPI(), p.pluginID.Load())
		if err != nil {
			return nil, err
		}
		return json.Marshal(view)
	case "saveConfig":
		res, err := p.rpcSaveConfig(p.studioAPI(), p.pluginID.Load(), payload)
		if err != nil {
			return nil, err
		}
		return json.Marshal(res)
	case "getSavings":
		return p.rpcGetSavings(payload)
	case "preview":
		return p.rpcPreview(payload)
	case "getPreviewOptions":
		return p.rpcPreviewOptions()
	default:
		return nil, fmt.Errorf("unknown RPC method: %s", method)
	}
}

func main() {
	plugin_sdk.Serve(NewPromptCompressorPlugin())
}
