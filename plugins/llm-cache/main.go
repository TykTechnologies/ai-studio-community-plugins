package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

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
	PluginName    = "llm-cache"
	PluginVersion = "1.0.0"
)

// PendingCacheOp represents a pending cache operation
type PendingCacheOp struct {
	CacheKey    string
	Namespace   string
	Model       string
	Timestamp   int64
	ShouldCache bool
}

// LLMCachePlugin implements the caching plugin
type LLMCachePlugin struct {
	plugin_sdk.BasePlugin
	config       *CacheConfig
	cache        *MemoryCache
	metrics      *CacheMetrics
	pendingMu    sync.RWMutex
	pendingCache map[string]*PendingCacheOp
}

// NewLLMCachePlugin creates a new cache plugin
func NewLLMCachePlugin() *LLMCachePlugin {
	return &LLMCachePlugin{
		BasePlugin:   plugin_sdk.NewBasePlugin(PluginName, PluginVersion, "LLM Response Cache - reduces costs and latency by caching identical requests"),
		pendingCache: make(map[string]*PendingCacheOp),
		metrics:      NewCacheMetrics(),
	}
}

// Initialize implements plugin_sdk.Plugin
func (p *LLMCachePlugin) Initialize(ctx plugin_sdk.Context, configMap map[string]string) error {
	log.Printf("%s: Initializing in %s runtime", PluginName, ctx.Runtime)

	// Parse configuration
	config, err := ParseConfig(configMap)
	if err != nil {
		log.Printf("%s: Failed to parse config, using defaults: %v", PluginName, err)
		config = DefaultConfig()
	}
	p.config = config

	// Initialize cache with config values
	p.cache = NewMemoryCache(
		config.MaxCacheSizeMB,
		config.MaxEntrySizeKB,
		config.TTLSeconds,
	)

	// Start periodic cleanup goroutine
	go p.periodicCleanup()

	log.Printf("%s: Initialized with TTL=%ds, MaxSize=%dMB, MaxEntry=%dKB, Namespaces=%v",
		PluginName, config.TTLSeconds, config.MaxCacheSizeMB, config.MaxEntrySizeKB, config.Namespaces)

	return nil
}

// Shutdown implements plugin_sdk.Plugin
func (p *LLMCachePlugin) Shutdown(ctx plugin_sdk.Context) error {
	log.Printf("%s: Shutting down", PluginName)
	return nil
}

// HandlePostAuth implements plugin_sdk.PostAuthHandler
// This is called before the request is forwarded to the LLM
func (p *LLMCachePlugin) HandlePostAuth(ctx plugin_sdk.Context, req *pb.EnrichedRequest) (*pb.PluginResponse, error) {
	if !p.config.Enabled {
		return &pb.PluginResponse{Modified: false}, nil
	}

	pluginReq := req.Request
	pluginCtx := pluginReq.Context
	requestID := pluginCtx.RequestId

	// Check for bypass
	if p.shouldBypass(req) {
		p.metrics.IncrementBypass()
		log.Printf("%s: Cache bypass requested for request %s", PluginName, requestID)

		// Store pending op with ShouldCache=false
		p.pendingMu.Lock()
		p.pendingCache[requestID] = &PendingCacheOp{
			Timestamp:   time.Now().Unix(),
			ShouldCache: false,
		}
		p.pendingMu.Unlock()

		return &pb.PluginResponse{Modified: false}, nil
	}

	// Generate namespace
	namespace := p.getNamespace(ctx)

	// Generate cache key from request
	cacheKey, model, err := GenerateCacheKey(namespace, pluginReq.Body, p.config.NormalizePrompts)
	if err != nil {
		log.Printf("%s: Failed to generate cache key: %v", PluginName, err)
		return &pb.PluginResponse{Modified: false}, nil
	}

	// Check cache for HIT
	entry := p.cache.Get(cacheKey)
	if entry != nil {
		// Cache HIT!
		p.metrics.IncrementHit()
		p.metrics.AddTokensSaved(int64(entry.TokensSaved))

		log.Printf("%s: Cache HIT for key %s (model=%s, tokens_saved=%d)",
			PluginName, cacheKey, model, entry.TokensSaved)

		// Build response headers
		headers := make(map[string]string)
		for k, v := range entry.Headers {
			headers[k] = v
		}
		headers["X-Cache-Status"] = "HIT"
		headers["X-Cache-Age"] = fmt.Sprintf("%d", time.Now().Unix()-entry.CreatedAt)
		headers["X-Cache-TTL"] = fmt.Sprintf("%d", entry.ExpiresAt-time.Now().Unix())

		if p.config.ExposeCacheKeyHeader {
			headers["X-Cache-Key"] = cacheKey
		}

		// Return cached response - block the request from going upstream
		return &pb.PluginResponse{
			Block:      true,
			StatusCode: 200,
			Headers:    headers,
			Body:       entry.Response,
			Modified:   true,
		}, nil
	}

	// Cache MISS - store pending operation for response phase
	p.metrics.IncrementMiss()
	log.Printf("%s: Cache MISS for key %s (model=%s)", PluginName, cacheKey, model)

	p.pendingMu.Lock()
	p.pendingCache[requestID] = &PendingCacheOp{
		CacheKey:    cacheKey,
		Namespace:   namespace,
		Model:       model,
		Timestamp:   time.Now().Unix(),
		ShouldCache: true,
	}
	p.pendingMu.Unlock()

	return &pb.PluginResponse{Modified: false}, nil
}

// OnBeforeWriteHeaders implements plugin_sdk.ResponseHandler
func (p *LLMCachePlugin) OnBeforeWriteHeaders(ctx plugin_sdk.Context, req *pb.HeadersRequest) (*pb.HeadersResponse, error) {
	// We handle everything in OnBeforeWrite
	return &pb.HeadersResponse{
		Modified: false,
		Headers:  req.Headers,
	}, nil
}

// OnBeforeWrite implements plugin_sdk.ResponseHandler
// This is called when the response is received from the LLM
func (p *LLMCachePlugin) OnBeforeWrite(ctx plugin_sdk.Context, req *pb.ResponseWriteRequest) (*pb.ResponseWriteResponse, error) {
	if !p.config.Enabled {
		return &pb.ResponseWriteResponse{Modified: false, Body: req.Body, Headers: req.Headers}, nil
	}

	pluginCtx := req.Context
	requestID := pluginCtx.RequestId

	// Skip streaming chunks - only cache complete responses
	if req.IsStreamChunk {
		return &pb.ResponseWriteResponse{Modified: false, Body: req.Body, Headers: req.Headers}, nil
	}

	// Retrieve pending operation
	p.pendingMu.Lock()
	pendingOp, exists := p.pendingCache[requestID]
	if exists {
		delete(p.pendingCache, requestID)
	}
	p.pendingMu.Unlock()

	if !exists {
		// No pending operation - request might not have gone through PostAuth
		return &pb.ResponseWriteResponse{Modified: false, Body: req.Body, Headers: req.Headers}, nil
	}

	// Build response headers
	modifiedHeaders := make(map[string]string)
	for k, v := range req.Headers {
		modifiedHeaders[k] = v
	}

	if !pendingOp.ShouldCache {
		// Bypass was requested
		modifiedHeaders["X-Cache-Status"] = "BYPASS"
		return &pb.ResponseWriteResponse{
			Modified: true,
			Body:     req.Body,
			Headers:  modifiedHeaders,
		}, nil
	}

	// Check if response indicates an error (don't cache errors)
	if p.isErrorResponse(req.Body) {
		log.Printf("%s: Not caching error response for request %s", PluginName, requestID)
		modifiedHeaders["X-Cache-Status"] = "MISS"
		return &pb.ResponseWriteResponse{
			Modified: true,
			Body:     req.Body,
			Headers:  modifiedHeaders,
		}, nil
	}

	// Extract token usage for metrics
	tokensSaved := ExtractTokensFromResponse(req.Body)

	// Store in cache
	stored := p.cache.Set(
		pendingOp.CacheKey,
		req.Body,
		req.Headers,
		pendingOp.Model,
		tokensSaved,
		nil, // Use default TTL
	)

	if stored {
		log.Printf("%s: Cached response for key %s (model=%s, tokens=%d, size=%d)",
			PluginName, pendingOp.CacheKey, pendingOp.Model, tokensSaved, len(req.Body))
	} else {
		log.Printf("%s: Failed to cache response for key %s (entry too large)", PluginName, pendingOp.CacheKey)
	}

	// Add cache status headers
	modifiedHeaders["X-Cache-Status"] = "MISS"
	if p.config.ExposeCacheKeyHeader {
		modifiedHeaders["X-Cache-Key"] = pendingOp.CacheKey
	}

	return &pb.ResponseWriteResponse{
		Modified: true,
		Body:     req.Body,
		Headers:  modifiedHeaders,
	}, nil
}

// GetAsset implements plugin_sdk.UIProvider
func (p *LLMCachePlugin) GetAsset(assetPath string) ([]byte, string, error) {
	if strings.HasPrefix(assetPath, "/") {
		assetPath = strings.TrimPrefix(assetPath, "/")
	}

	content, err := embeddedAssets.ReadFile(assetPath)
	if err != nil {
		return nil, "", fmt.Errorf("asset not found: %s", assetPath)
	}

	mimeType := "application/octet-stream"
	if strings.HasSuffix(assetPath, ".js") {
		mimeType = "application/javascript"
	} else if strings.HasSuffix(assetPath, ".css") {
		mimeType = "text/css"
	} else if strings.HasSuffix(assetPath, ".svg") {
		mimeType = "image/svg+xml"
	} else if strings.HasSuffix(assetPath, ".json") {
		mimeType = "application/json"
	} else if strings.HasSuffix(assetPath, ".html") {
		mimeType = "text/html"
	}

	return content, mimeType, nil
}

// ListAssets implements plugin_sdk.UIProvider
func (p *LLMCachePlugin) ListAssets(pathPrefix string) ([]*pb.AssetInfo, error) {
	return []*pb.AssetInfo{}, nil
}

// GetManifest implements plugin_sdk.UIProvider
func (p *LLMCachePlugin) GetManifest() ([]byte, error) {
	return manifestFile, nil
}

// HandleRPC implements plugin_sdk.UIProvider
func (p *LLMCachePlugin) HandleRPC(method string, payload []byte) ([]byte, error) {
	log.Printf("%s: RPC Call - method: %s", PluginName, method)

	var result interface{}
	var err error

	switch method {
	case "getMetrics":
		result = p.rpcGetMetrics()
	case "clearCache":
		result = p.rpcClearCache()
	case "getConfig":
		result = p.rpcGetConfig()
	default:
		return nil, fmt.Errorf("unknown RPC method: %s", method)
	}

	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}

// GetConfigSchema implements plugin_sdk.ConfigProvider
func (p *LLMCachePlugin) GetConfigSchema() ([]byte, error) {
	return configSchemaFile, nil
}

// shouldBypass checks if the request should bypass the cache
func (p *LLMCachePlugin) shouldBypass(req *pb.EnrichedRequest) bool {
	headers := req.Request.Headers

	// Header bypass: X-Cache: bypass (case-insensitive)
	for k, v := range headers {
		if strings.ToLower(k) == "x-cache" && strings.ToLower(v) == "bypass" {
			return true
		}
	}

	// Query param bypass: ?cache=bypass
	path := req.Request.Path
	if strings.Contains(path, "cache=bypass") {
		return true
	}

	return false
}

// getNamespace generates the namespace string for cache key isolation
func (p *LLMCachePlugin) getNamespace(ctx plugin_sdk.Context) string {
	parts := []string{}

	for _, ns := range p.config.Namespaces {
		switch ns {
		case "api_key":
			// Use app ID as a proxy for API key (API key itself not directly available)
			if ctx.AppID > 0 {
				parts = append(parts, fmt.Sprintf("app:%d", ctx.AppID))
			}
		case "app_id":
			if ctx.AppID > 0 {
				parts = append(parts, fmt.Sprintf("app:%d", ctx.AppID))
			}
		case "org_id":
			// Organization ID might be in metadata
			if orgID, ok := ctx.Metadata["org_id"]; ok {
				parts = append(parts, fmt.Sprintf("org:%s", orgID))
			}
		}
	}

	if len(parts) == 0 {
		return "default"
	}

	return strings.Join(parts, ":")
}

// isErrorResponse checks if the response indicates an error
func (p *LLMCachePlugin) isErrorResponse(body []byte) bool {
	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return false
	}

	// Check for error field
	if errField, ok := response["error"]; ok && errField != nil {
		return true
	}

	return false
}

// periodicCleanup runs periodic cache maintenance
func (p *LLMCachePlugin) periodicCleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		// Clean up expired cache entries
		removed := p.cache.CleanupExpired()
		if removed > 0 {
			log.Printf("%s: Cleaned up %d expired cache entries", PluginName, removed)
		}

		// Clean up stale pending operations (older than 5 minutes)
		p.cleanupStalePending()
	}
}

// cleanupStalePending removes pending operations older than 5 minutes
func (p *LLMCachePlugin) cleanupStalePending() {
	threshold := time.Now().Unix() - 300 // 5 minutes

	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()

	removed := 0
	for key, op := range p.pendingCache {
		if op.Timestamp < threshold {
			delete(p.pendingCache, key)
			removed++
		}
	}

	if removed > 0 {
		log.Printf("%s: Cleaned up %d stale pending operations", PluginName, removed)
	}
}

// RPC method implementations

func (p *LLMCachePlugin) rpcGetMetrics() interface{} {
	entries, sizeBytes, evictions := p.cache.Stats()

	return map[string]interface{}{
		"hit_count":           p.metrics.GetHitCount(),
		"miss_count":          p.metrics.GetMissCount(),
		"bypass_count":        p.metrics.GetBypassCount(),
		"eviction_count":      evictions,
		"active_entries":      entries,
		"cache_size_bytes":    sizeBytes,
		"max_size_bytes":      p.cache.GetMaxSize(),
		"hit_rate":            p.metrics.GetHitRate(),
		"total_tokens_saved":  p.metrics.GetTotalTokensSaved(),
	}
}

func (p *LLMCachePlugin) rpcClearCache() interface{} {
	p.cache.Clear()
	p.metrics.Reset()
	log.Printf("%s: Cache cleared via RPC", PluginName)
	return map[string]interface{}{
		"success": true,
		"message": "Cache cleared successfully",
	}
}

func (p *LLMCachePlugin) rpcGetConfig() interface{} {
	return p.config
}

func main() {
	plugin := NewLLMCachePlugin()
	plugin_sdk.Serve(plugin)
}
