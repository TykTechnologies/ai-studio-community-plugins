package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strings"
	"sync"

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
}

// NewPromptCompressorPlugin creates a new plugin instance
func NewPromptCompressorPlugin() *PromptCompressorPlugin {
	return &PromptCompressorPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(
			PluginName,
			PluginVersion,
			"Prompt Compressor - cache-safe, measured compression of LLM request bodies",
		),
	}
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
func (p *PromptCompressorPlugin) Initialize(ctx plugin_sdk.Context, config map[string]string) error {
	p.mu.Lock()
	p.runtime = ctx.Runtime
	p.services = ctx.Services
	p.mu.Unlock()

	p.logger().Info("Initialized", "runtime", ctx.Runtime, "version", PluginVersion)
	return nil
}

// Shutdown implements plugin_sdk.Plugin. It is safe to call more than once.
func (p *PromptCompressorPlugin) Shutdown(ctx plugin_sdk.Context) error {
	p.logger().Debug("Shutting down")
	return nil
}

// OnSessionReady implements plugin_sdk.SessionAware
func (p *PromptCompressorPlugin) OnSessionReady(ctx plugin_sdk.Context) {
	p.logger().Debug("Session broker active", "runtime", ctx.Runtime)
}

// OnSessionClosing implements plugin_sdk.SessionAware
func (p *PromptCompressorPlugin) OnSessionClosing(ctx plugin_sdk.Context) {
	p.logger().Debug("Session closing")
}

// HandlePostAuth implements plugin_sdk.PostAuthHandler. The scaffold passes
// every request through untouched.
//
// Headers stay nil on every response: the host Sets each returned header on
// the upstream request and never removes one, so echoing the request headers
// back would collapse multi-value headers.
func (p *PromptCompressorPlugin) HandlePostAuth(ctx plugin_sdk.Context, req *pb.EnrichedRequest) (*pb.PluginResponse, error) {
	return &pb.PluginResponse{Modified: false}, nil
}

// OnBeforeWriteHeaders implements plugin_sdk.ResponseHandler. The host
// replaces the whole header set when Modified is true, so a no-op leaves
// Modified false.
func (p *PromptCompressorPlugin) OnBeforeWriteHeaders(ctx plugin_sdk.Context, req *pb.HeadersRequest) (*pb.HeadersResponse, error) {
	return &pb.HeadersResponse{Modified: false}, nil
}

// OnBeforeWrite implements plugin_sdk.ResponseHandler
func (p *PromptCompressorPlugin) OnBeforeWrite(ctx plugin_sdk.Context, req *pb.ResponseWriteRequest) (*pb.ResponseWriteResponse, error) {
	return &pb.ResponseWriteResponse{Modified: false}, nil
}

// OnStreamComplete implements plugin_sdk.StreamCompleteHandler
func (p *PromptCompressorPlugin) OnStreamComplete(ctx plugin_sdk.Context, req *pb.StreamCompleteRequest) (*pb.StreamCompleteResponse, error) {
	return &pb.StreamCompleteResponse{Handled: false}, nil
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
		return json.Marshal(map[string]interface{}{
			"plugin":  PluginName,
			"version": PluginVersion,
			"runtime": runtime,
		})
	default:
		return nil, fmt.Errorf("unknown RPC method: %s", method)
	}
}

func main() {
	plugin_sdk.Serve(NewPromptCompressorPlugin())
}
