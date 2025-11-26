package main

import (
	"context"
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

	// Runtime mode detection
	runtime plugin_sdk.RuntimeType

	// Services reference for background operations (gateway mode)
	gatewayServices plugin_sdk.GatewayServices

	// Control plane mode: aggregated stats from edge instances
	aggregateMu      sync.RWMutex
	aggregatedStats  map[string]*EdgeStatsRecord
	stopStatsReporter chan struct{}

	// Track if reporter has been started (Initialize is called twice in gateway mode)
	reporterStarted bool
	reporterMu      sync.Mutex

	// Broker warmup - ensures broker connection is established on first RPC
	warmupBrokerOnce sync.Once
}

// NewLLMCachePlugin creates a new cache plugin
func NewLLMCachePlugin() *LLMCachePlugin {
	return &LLMCachePlugin{
		BasePlugin:        plugin_sdk.NewBasePlugin(PluginName, PluginVersion, "LLM Response Cache - reduces costs and latency by caching identical requests"),
		pendingCache:      make(map[string]*PendingCacheOp),
		metrics:           NewCacheMetrics(),
		aggregatedStats:   make(map[string]*EdgeStatsRecord),
		stopStatsReporter: make(chan struct{}),
	}
}

// Initialize implements plugin_sdk.Plugin
func (p *LLMCachePlugin) Initialize(ctx plugin_sdk.Context, configMap map[string]string) error {
	log.Printf("%s: Initializing in %s runtime", PluginName, ctx.Runtime)

	// Store runtime for mode-specific behavior
	p.runtime = ctx.Runtime

	// Parse configuration
	config, err := ParseConfig(configMap)
	if err != nil {
		log.Printf("%s: Failed to parse config, using defaults: %v", PluginName, err)
		config = DefaultConfig()
	}
	p.config = config

	// Initialize cache with config values (used in gateway mode)
	p.cache = NewMemoryCache(
		config.MaxCacheSizeMB,
		config.MaxEntrySizeKB,
		config.TTLSeconds,
	)

	// Start periodic cleanup goroutine
	go p.periodicCleanup()

	// Runtime-specific initialization
	if p.runtime == plugin_sdk.RuntimeGateway {
		// Gateway (edge) mode: Store services reference and start stats reporter
		// Broker ID is now passed in the first (and only) Initialize call
		p.gatewayServices = ctx.Services.Gateway()

		// Check if we have the broker ID
		hasBrokerID := false
		if _, ok := configMap["_service_broker_id"]; ok {
			hasBrokerID = true
		}

		if p.gatewayServices != nil && hasBrokerID {
			p.reporterMu.Lock()
			if !p.reporterStarted {
				p.reporterStarted = true
				p.reporterMu.Unlock()
				log.Printf("%s: Gateway mode - services available, starting stats reporter", PluginName)
				go p.reportStatsToControl()
				log.Printf("%s: Gateway mode - stats reporter started (interval: %ds)", PluginName, config.ReportIntervalSeconds)
			} else {
				p.reporterMu.Unlock()
				log.Printf("%s: Gateway mode - reporter already running", PluginName)
			}
		} else if !hasBrokerID {
			log.Printf("%s: Gateway mode - no broker ID available, stats reporting disabled", PluginName)
		} else {
			log.Printf("%s: Gateway mode - WARNING: Gateway services not available, stats reporting disabled", PluginName)
		}
	} else {
		// Studio (control) mode: We receive stats via AcceptEdgePayload
		log.Printf("%s: Studio mode - ready to receive stats from edge instances", PluginName)
	}

	log.Printf("%s: Initialized with TTL=%ds, MaxSize=%dMB, MaxEntry=%dKB, Namespaces=%v",
		PluginName, config.TTLSeconds, config.MaxCacheSizeMB, config.MaxEntrySizeKB, config.Namespaces)

	return nil
}

// Shutdown implements plugin_sdk.Plugin
func (p *LLMCachePlugin) Shutdown(ctx plugin_sdk.Context) error {
	log.Printf("%s: Shutting down", PluginName)

	// Stop the stats reporter if running (gateway mode)
	if p.runtime == plugin_sdk.RuntimeGateway {
		close(p.stopStatsReporter)
	}

	return nil
}

// HandlePostAuth implements plugin_sdk.PostAuthHandler
// This is called before the request is forwarded to the LLM
func (p *LLMCachePlugin) HandlePostAuth(ctx plugin_sdk.Context, req *pb.EnrichedRequest) (*pb.PluginResponse, error) {
	// Warm up the broker connection on first request if not already done
	// This is needed because the broker connection isn't fully established until
	// an RPC call triggers the dial from within the plugin RPC context
	p.warmupBrokerOnce.Do(func() {
		if p.gatewayServices != nil && p.runtime == plugin_sdk.RuntimeGateway {
			warmupCtx := context.Background()
			_, err := p.gatewayServices.SendToControlJSON(warmupCtx, map[string]string{
				"event": "broker_warmup",
				"plugin": PluginName,
			}, "broker-warmup", nil)
			if err != nil {
				log.Printf("%s: Broker warmup failed: %v (stats reporting may not work)", PluginName, err)
			} else {
				log.Printf("%s: Broker connection warmed up successfully", PluginName)
			}
		}
	})

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
	log.Printf("%s: rpcGetMetrics called, runtime=%s", PluginName, p.runtime)

	// In control plane (studio) mode, return aggregated stats from all edges
	if p.runtime == plugin_sdk.RuntimeStudio {
		result := p.getAggregatedMetrics()
		log.Printf("%s: Returning aggregated metrics: edge_count=%d", PluginName, len(p.aggregatedStats))
		return result
	}

	// In gateway (edge) mode, return local stats
	entries, sizeBytes, evictions := p.cache.Stats()

	return map[string]interface{}{
		"mode":               "local",
		"hit_count":          p.metrics.GetHitCount(),
		"miss_count":         p.metrics.GetMissCount(),
		"bypass_count":       p.metrics.GetBypassCount(),
		"eviction_count":     evictions,
		"active_entries":     entries,
		"cache_size_bytes":   sizeBytes,
		"max_size_bytes":     p.cache.GetMaxSize(),
		"hit_rate":           p.metrics.GetHitRate(),
		"total_tokens_saved": p.metrics.GetTotalTokensSaved(),
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

// ============================================================================
// Edge-to-Control Communication
// ============================================================================

// reportStatsToControl periodically sends cache statistics to the control plane
// This runs only in gateway (edge) mode
func (p *LLMCachePlugin) reportStatsToControl() {
	interval := time.Duration(p.config.ReportIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopStatsReporter:
			log.Printf("%s: Stats reporter stopped", PluginName)
			return
		case <-ticker.C:
			p.sendStatsToControl()
		}
	}
}

// sendStatsToControl collects current stats and sends them to the control plane
func (p *LLMCachePlugin) sendStatsToControl() {
	// Check if gateway services are available
	if p.gatewayServices == nil {
		log.Printf("%s: Cannot send stats - gateway services not available", PluginName)
		return
	}

	// Collect current stats
	entries, sizeBytes, evictions := p.cache.Stats()

	stats := EdgeCacheStats{
		HitCount:         p.metrics.GetHitCount(),
		MissCount:        p.metrics.GetMissCount(),
		BypassCount:      p.metrics.GetBypassCount(),
		EvictionCount:    evictions,
		ActiveEntries:    entries,
		CacheSizeBytes:   sizeBytes,
		MaxSizeBytes:     p.cache.GetMaxSize(),
		HitRate:          p.metrics.GetHitRate(),
		TotalTokensSaved: p.metrics.GetTotalTokensSaved(),
		Timestamp:        time.Now().Unix(),
	}

	// Send to control plane using unified SDK's gateway services
	ctx := context.Background()
	pendingCount, err := p.gatewayServices.SendToControlJSON(ctx, stats, "", map[string]string{
		"metric_type": "llm_cache_stats",
	})

	if err != nil {
		log.Printf("%s: Failed to send stats to control plane: %v", PluginName, err)
		return
	}

	log.Printf("%s: Stats sent to control plane (hits=%d, misses=%d, pending_queue=%d)",
		PluginName, stats.HitCount, stats.MissCount, pendingCount)
}

// AcceptEdgePayload implements plugin_sdk.EdgePayloadReceiver
// This is called on the control plane when stats arrive from edge instances
func (p *LLMCachePlugin) AcceptEdgePayload(ctx plugin_sdk.Context, payload *plugin_sdk.EdgePayload) (bool, error) {
	// Check if this payload is for us
	metricType, ok := payload.Metadata["metric_type"]
	if !ok || metricType != "llm_cache_stats" {
		// Not our payload, return handled=false so other plugins can process it
		return false, nil
	}

	// Parse the stats payload
	var stats EdgeCacheStats
	if err := json.Unmarshal(payload.Payload, &stats); err != nil {
		log.Printf("%s: Failed to parse edge stats from %s: %v", PluginName, payload.EdgeID, err)
		return true, fmt.Errorf("invalid payload format: %w", err)
	}

	// Store/update stats for this edge instance
	p.aggregateMu.Lock()
	p.aggregatedStats[payload.EdgeID] = &EdgeStatsRecord{
		EdgeID:     payload.EdgeID,
		Namespace:  payload.EdgeNamespace,
		Stats:      stats,
		LastUpdate: time.Unix(payload.EdgeTimestamp, 0),
	}
	p.aggregateMu.Unlock()

	log.Printf("%s: Received stats from edge %s: hits=%d, misses=%d, hit_rate=%.2f%%",
		PluginName, payload.EdgeID, stats.HitCount, stats.MissCount, stats.HitRate*100)

	return true, nil
}

// getAggregatedMetrics returns combined stats from all edge instances (control plane mode)
func (p *LLMCachePlugin) getAggregatedMetrics() map[string]interface{} {
	p.aggregateMu.RLock()
	defer p.aggregateMu.RUnlock()

	var totalHits, totalMisses, totalBypass, totalTokensSaved, totalEvictions int64
	var totalCacheSize, totalMaxSize int64
	var totalEntries int

	edgeStats := make([]map[string]interface{}, 0, len(p.aggregatedStats))

	for _, es := range p.aggregatedStats {
		totalHits += es.Stats.HitCount
		totalMisses += es.Stats.MissCount
		totalBypass += es.Stats.BypassCount
		totalEvictions += es.Stats.EvictionCount
		totalTokensSaved += es.Stats.TotalTokensSaved
		totalCacheSize += es.Stats.CacheSizeBytes
		totalMaxSize += es.Stats.MaxSizeBytes
		totalEntries += es.Stats.ActiveEntries

		edgeStats = append(edgeStats, map[string]interface{}{
			"edge_id":          es.EdgeID,
			"namespace":        es.Namespace,
			"hit_count":        es.Stats.HitCount,
			"miss_count":       es.Stats.MissCount,
			"bypass_count":     es.Stats.BypassCount,
			"hit_rate":         es.Stats.HitRate,
			"active_entries":   es.Stats.ActiveEntries,
			"cache_size_bytes": es.Stats.CacheSizeBytes,
			"max_size_bytes":   es.Stats.MaxSizeBytes,
			"tokens_saved":     es.Stats.TotalTokensSaved,
			"last_update":      es.LastUpdate.Unix(),
		})
	}

	// Calculate overall hit rate
	hitRate := float64(0)
	if totalHits+totalMisses > 0 {
		hitRate = float64(totalHits) / float64(totalHits+totalMisses)
	}

	return map[string]interface{}{
		"mode":               "aggregated",
		"edge_count":         len(p.aggregatedStats),
		"hit_count":          totalHits,
		"miss_count":         totalMisses,
		"bypass_count":       totalBypass,
		"eviction_count":     totalEvictions,
		"active_entries":     totalEntries,
		"cache_size_bytes":   totalCacheSize,
		"max_size_bytes":     totalMaxSize,
		"hit_rate":           hitRate,
		"total_tokens_saved": totalTokensSaved,
		"edges":              edgeStats,
	}
}

func main() {
	plugin := NewLLMCachePlugin()
	plugin_sdk.Serve(plugin)
}
