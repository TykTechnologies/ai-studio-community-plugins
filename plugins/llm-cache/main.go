package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
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

// ClearCacheOperation tracks a pending distributed cache clear operation
type ClearCacheOperation struct {
	OperationID string              `json:"operation_id"`
	StartTime   int64               `json:"start_time"`
	Timeout     int64               `json:"timeout"` // seconds
	AckEdges    map[string]int64    `json:"ack_edges"` // edge_id -> timestamp
	Completed   bool                `json:"completed"`
}

// ClearCacheEvent is the payload sent from control to edges
type ClearCacheEvent struct {
	OperationID string `json:"operation_id"`
	Timestamp   int64  `json:"timestamp"`
}

// ClearCacheAck is the acknowledgement sent from edge to control
type ClearCacheAck struct {
	OperationID string `json:"operation_id"`
	EdgeID      string `json:"edge_id"`
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	Timestamp   int64  `json:"timestamp"`
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

	// Services reference for KV persistence (works in both modes)
	services plugin_sdk.ServiceBroker

	// Track if reporter has been started (Initialize is called twice in gateway mode)
	reporterStarted bool
	reporterMu      sync.Mutex

	// Broker warmup - ensures broker connection is established on first RPC
	warmupBrokerOnce sync.Once

	// Event-based distributed cache clearing
	clearOpsMu      sync.RWMutex
	clearOperations map[string]*ClearCacheOperation // operation_id -> operation (control plane)
	eventSubIDs     []string                        // subscription IDs for cleanup

	// Lazy event subscription - must happen during RPC call when broker is active
	eventSubOnce sync.Once
}

// NewLLMCachePlugin creates a new cache plugin
func NewLLMCachePlugin() *LLMCachePlugin {
	return &LLMCachePlugin{
		BasePlugin:        plugin_sdk.NewBasePlugin(PluginName, PluginVersion, "LLM Response Cache - reduces costs and latency by caching identical requests"),
		pendingCache:      make(map[string]*PendingCacheOp),
		metrics:           NewCacheMetrics(),
		aggregatedStats:   make(map[string]*EdgeStatsRecord),
		stopStatsReporter: make(chan struct{}),
		clearOperations:   make(map[string]*ClearCacheOperation),
		eventSubIDs:       make([]string, 0),
	}
}

// Initialize implements plugin_sdk.Plugin
func (p *LLMCachePlugin) Initialize(ctx plugin_sdk.Context, configMap map[string]string) error {
	// Store runtime for mode-specific behavior
	p.runtime = ctx.Runtime

	// Parse configuration
	config, err := ParseConfig(configMap)
	if err != nil {
		log.Printf("%s: Config parse error, using defaults: %v", PluginName, err)
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

	// Store services reference for event pub/sub (needed in both modes)
	p.services = ctx.Services

	// Runtime-specific initialization
	if p.runtime == plugin_sdk.RuntimeGateway {
		// Gateway (edge) mode: Store services reference
		// The stats reporter is started in OnSessionReady when the broker is ready
		p.gatewayServices = ctx.Services.Gateway()

		// NOTE: Stats reporting is now started in OnSessionReady via the SessionAware pattern.
		// This ensures the broker connection is established before we try to send metrics.
		// Event subscriptions are also set up in OnSessionReady.
	} else {
		// Studio (control) mode: We receive stats via AcceptEdgePayload

		// Load persisted stats from KV storage
		if p.services != nil && p.services.KV() != nil {
			if err := p.loadStatsFromKV(); err != nil {
				log.Printf("%s: Failed to load persisted stats: %v", PluginName, err)
			}
		}

		// NOTE: Event subscriptions are set up lazily during HandleRPC
		// because the broker connection is only active during RPC calls.
		// See ensureEventSubscription() which is called from HandleRPC.
	}

	return nil
}

// Shutdown implements plugin_sdk.Plugin
func (p *LLMCachePlugin) Shutdown(ctx plugin_sdk.Context) error {
	// Stop the stats reporter if running (gateway mode)
	if p.runtime == plugin_sdk.RuntimeGateway {
		close(p.stopStatsReporter)
	}

	// Unsubscribe from events
	p.unsubscribeEvents()

	return nil
}

// debugLog writes to a file for debugging when stdout/stderr are not visible
func debugLog(format string, args ...interface{}) {
	f, err := os.OpenFile("/tmp/llm-cache-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	f.WriteString(msg)
}

// OnSessionReady implements plugin_sdk.SessionAware
// This is called when the session-based broker connection is first established.
// The broker connection stays alive during the session, making it ideal for
// setting up event subscriptions and starting background services that need the broker.
func (p *LLMCachePlugin) OnSessionReady(ctx plugin_sdk.Context) {
	debugLog("OnSessionReady called - session-based broker is now active (runtime=%v)", p.runtime)
	log.Printf("%s: [INFO] OnSessionReady called - session-based broker is now active (runtime: %v)", PluginName, p.runtime)

	// Warm up the Service API connection first to ensure broker is ready
	// This prevents timeout errors on subsequent service API calls
	if p.runtime == plugin_sdk.RuntimeGateway {
		log.Printf("%s: [INFO] Warming up Gateway Service API connection...", PluginName)
		_, err := ctx.Services.KV().Read(ctx, "warmup-probe")
		if err != nil {
			log.Printf("%s: [INFO] Gateway Service API warmup completed (probe key not found is expected)", PluginName)
		} else {
			log.Printf("%s: [INFO] Gateway Service API connection established successfully", PluginName)
		}
	}

	// Set up event subscriptions now that we have a stable broker connection
	p.setupEventSubscriptions()

	// In gateway mode, start the stats reporter now that the broker is ready
	if p.runtime == plugin_sdk.RuntimeGateway && p.gatewayServices != nil {
		p.reporterMu.Lock()
		if !p.reporterStarted {
			p.reporterStarted = true
			p.reporterMu.Unlock()
			log.Printf("%s: [INFO] Starting stats reporter (broker now ready)", PluginName)
			go p.reportStatsToControl()
		} else {
			p.reporterMu.Unlock()
		}
	}
}

// OnSessionClosing implements plugin_sdk.SessionAware
// This is called before the session is explicitly closed (not on timeout).
func (p *LLMCachePlugin) OnSessionClosing(ctx plugin_sdk.Context) {
	log.Printf("%s: [INFO] OnSessionClosing called - cleaning up event subscriptions", PluginName)

	// Clean up event subscriptions
	p.unsubscribeEvents()
}

// setupEventSubscriptions sets up the event subscriptions for distributed cache clearing.
// This is called from OnSessionReady when we have a stable broker connection.
func (p *LLMCachePlugin) setupEventSubscriptions() {
	// Use sync.Once to ensure we only set up subscriptions once
	p.eventSubOnce.Do(func() {
		log.Printf("%s: [INFO] Setting up event subscriptions via SessionAware pattern", PluginName)

		if p.services == nil {
			log.Printf("%s: [WARN] p.services is nil, cannot subscribe to events", PluginName)
			return
		}

		events := p.services.Events()
		if events == nil {
			log.Printf("%s: [WARN] p.services.Events() returned nil", PluginName)
			return
		}

		log.Printf("%s: [INFO] Events service available, setting up subscriptions (runtime=%v)", PluginName, p.runtime)

		if p.runtime == plugin_sdk.RuntimeGateway {
			// Edge mode: subscribe to cache clear commands from control
			log.Printf("%s: [INFO] Edge mode: subscribing to %s", PluginName, TopicCacheClear)
			subID, err := events.Subscribe(TopicCacheClear, p.handleClearCacheEvent)
			if err != nil {
				log.Printf("%s: [ERROR] Failed to subscribe to %s: %v", PluginName, TopicCacheClear, err)
			} else {
				p.eventSubIDs = append(p.eventSubIDs, subID)
				log.Printf("%s: [INFO] ✅ Successfully subscribed to %s events (edge mode), subID=%s", PluginName, TopicCacheClear, subID)
			}
		} else {
			// Control mode: subscribe to cache clear acknowledgements from edges
			log.Printf("%s: [INFO] Control mode: subscribing to %s", PluginName, TopicCacheClearAck)
			subID, err := events.Subscribe(TopicCacheClearAck, p.handleClearCacheAck)
			if err != nil {
				log.Printf("%s: [ERROR] Failed to subscribe to %s: %v", PluginName, TopicCacheClearAck, err)
			} else {
				p.eventSubIDs = append(p.eventSubIDs, subID)
				log.Printf("%s: [INFO] ✅ Successfully subscribed to %s events (control mode), subID=%s", PluginName, TopicCacheClearAck, subID)
			}
		}
	})
}

// HandlePostAuth implements plugin_sdk.PostAuthHandler
// This is called before the request is forwarded to the LLM
func (p *LLMCachePlugin) HandlePostAuth(ctx plugin_sdk.Context, req *pb.EnrichedRequest) (*pb.PluginResponse, error) {
	// Warm up the broker connection on first request if not already done
	p.warmupBrokerOnce.Do(func() {
		if p.gatewayServices != nil && p.runtime == plugin_sdk.RuntimeGateway {
			warmupCtx := context.Background()
			_, err := p.gatewayServices.SendToControlJSON(warmupCtx, map[string]string{
				"event":  "broker_warmup",
				"plugin": PluginName,
			}, "broker-warmup", nil)
			if err != nil {
				log.Printf("%s: Broker warmup failed: %v", PluginName, err)
			}
		}
	})

	// In gateway mode, set up event subscriptions during the first request
	// The broker connection is active during HandlePostAuth calls, so subscriptions will work
	if p.runtime == plugin_sdk.RuntimeGateway {
		p.ensureEventSubscription()
	}

	if !p.config.Enabled {
		return &pb.PluginResponse{Modified: false}, nil
	}

	pluginReq := req.Request
	pluginCtx := pluginReq.Context
	requestID := pluginCtx.RequestId

	// Check for bypass
	if p.shouldBypass(req) {
		p.metrics.IncrementBypass()

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
		return &pb.PluginResponse{Modified: false}, nil
	}

	// Check cache for HIT
	entry := p.cache.Get(cacheKey)
	if entry != nil {
		p.metrics.IncrementHit()
		p.metrics.AddTokensSaved(int64(entry.TokensSaved))

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

		// Determine response format based on request
		responseBody := entry.Response
		isStreamingRequest := IsStreamingRequest(pluginReq.Body)

		if isStreamingRequest && p.config.CacheStreamingResponses {
			// Convert cached JSON to SSE format for streaming requests
			vendor := string(pluginCtx.Vendor)
			sseResponse, err := ConvertJSONToSSE(entry.Response, vendor)
			if err != nil {
				log.Printf("%s: Failed to convert cache to SSE: %v", PluginName, err)
				// Fall back to JSON response
			} else {
				responseBody = sseResponse
				headers["Content-Type"] = "text/event-stream"
				headers["Cache-Control"] = "no-cache"
				headers["Connection"] = "keep-alive"
			}
		}

		// Return cached response - block the request from going upstream
		return &pb.PluginResponse{
			Block:      true,
			StatusCode: 200,
			Headers:    headers,
			Body:       responseBody,
			Modified:   true,
		}, nil
	}

	// Cache MISS - store pending operation for response phase
	p.metrics.IncrementMiss()

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
		modifiedHeaders["X-Cache-Status"] = "MISS"
		return &pb.ResponseWriteResponse{
			Modified: true,
			Body:     req.Body,
			Headers:  modifiedHeaders,
		}, nil
	}

	// Extract token usage for metrics
	tokensSaved := ExtractTokensFromResponse(req.Body)

	// Sanitize headers before caching - strip compression/transfer headers
	// since the body is already decompressed by the proxy layer
	cacheHeaders := make(map[string]string)
	for k, v := range req.Headers {
		cacheHeaders[k] = v
	}
	delete(cacheHeaders, "Content-Encoding")
	delete(cacheHeaders, "Transfer-Encoding")
	cacheHeaders["Content-Length"] = fmt.Sprintf("%d", len(req.Body))

	// Store in cache
	p.cache.Set(
		pendingOp.CacheKey,
		req.Body,
		cacheHeaders,
		pendingOp.Model,
		tokensSaved,
		nil, // Use default TTL
	)

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

// OnStreamComplete implements plugin_sdk.StreamCompleteHandler
// This is called after a streaming response has finished, providing the accumulated response.
func (p *LLMCachePlugin) OnStreamComplete(ctx plugin_sdk.Context, req *pb.StreamCompleteRequest) (*pb.StreamCompleteResponse, error) {
	if !p.config.Enabled {
		return &pb.StreamCompleteResponse{Handled: false}, nil
	}

	// Check if streaming caching is enabled
	if !p.config.CacheStreamingResponses {
		return &pb.StreamCompleteResponse{Handled: false}, nil
	}

	pluginCtx := req.Context
	requestID := pluginCtx.RequestId

	// Retrieve pending operation (set in HandlePostAuth)
	p.pendingMu.Lock()
	pendingOp, exists := p.pendingCache[requestID]
	if exists {
		delete(p.pendingCache, requestID)
	}
	p.pendingMu.Unlock()

	if !exists {
		// No pending operation - request might not have gone through PostAuth
		return &pb.StreamCompleteResponse{Handled: false}, nil
	}

	if !pendingOp.ShouldCache {
		// Bypass was requested
		return &pb.StreamCompleteResponse{Handled: true, Cached: false}, nil
	}

	// Get vendor from context metadata for SSE parsing
	vendor := ""
	if pluginCtx.Metadata != nil {
		vendor = pluginCtx.Metadata["vendor"]
	}

	// Reconstruct a JSON response from the SSE stream
	reconstructedJSON, tokenUsage, err := ReconstructResponseFromSSE(req.AccumulatedResponse, vendor)
	if err != nil {
		log.Printf("%s: Failed to reconstruct response from SSE: %v", PluginName, err)
		return &pb.StreamCompleteResponse{Handled: true, Cached: false, ErrorMessage: err.Error()}, nil
	}

	// Check if the reconstructed response indicates an error
	if p.isErrorResponse(reconstructedJSON) {
		return &pb.StreamCompleteResponse{Handled: true, Cached: false}, nil
	}

	// Sanitize headers before caching - strip compression/transfer headers
	// since the body is already decompressed by the proxy layer
	streamCacheHeaders := make(map[string]string)
	for k, v := range req.Headers {
		streamCacheHeaders[k] = v
	}
	delete(streamCacheHeaders, "Content-Encoding")
	delete(streamCacheHeaders, "Transfer-Encoding")
	streamCacheHeaders["Content-Length"] = fmt.Sprintf("%d", len(reconstructedJSON))

	// Store in cache - store the reconstructed JSON, not the raw SSE
	// When a cache HIT occurs, HandlePostAuth will convert it back to SSE if needed
	p.cache.Set(
		pendingOp.CacheKey,
		reconstructedJSON,
		streamCacheHeaders,
		pendingOp.Model,
		tokenUsage,
		nil, // Use default TTL
	)

	log.Printf("%s: Cached streaming response for request %s (tokens: %d)", PluginName, requestID, tokenUsage)

	return &pb.StreamCompleteResponse{Handled: true, Cached: true}, nil
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
	// CRITICAL: Extract the per-request broker ID from payload and update the event service.
	// In AI Studio, each RPC call gets a new per-request broker (IDs 2, 3, 4, etc.).
	// The long-lived broker (ID 1) from Initialize is no longer active by the time
	// we reach HandleRPC. We MUST use the per-request broker for this specific RPC.
	p.updateEventServiceBrokerFromPayload(payload)

	// Set up event subscriptions lazily during this active RPC call.
	p.ensureEventSubscription()

	var result interface{}

	switch method {
	case "getMetrics":
		result = p.rpcGetMetrics()
	case "clearCache":
		result = p.rpcClearCache()
	case "getConfig":
		result = p.rpcGetConfig()
	case "getClearStatus":
		result = p.rpcGetClearStatus(payload)
	default:
		return nil, fmt.Errorf("unknown RPC method: %s", method)
	}

	return json.Marshal(result)
}

// updateEventServiceBrokerFromPayload extracts the per-request broker ID from the RPC payload
// and updates the event service to use it. This is necessary because AI Studio uses
// per-request brokers, not a long-lived broker like Microgateway.
func (p *LLMCachePlugin) updateEventServiceBrokerFromPayload(payload []byte) {
	if payload == nil || len(payload) == 0 {
		return
	}

	var payloadMap map[string]interface{}
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return
	}

	// Extract _service_broker_id from payload (set by AI Studio plugin manager)
	if brokerIDFloat, ok := payloadMap["_service_broker_id"].(float64); ok {
		brokerID := uint32(brokerIDFloat)
		if brokerID > 0 {
			log.Printf("%s: [DEBUG] Updating event service broker ID to %d (per-request broker)", PluginName, brokerID)
			plugin_sdk.SetEventServiceBrokerID(brokerID)
		}
	}
}

// ensureEventSubscription is a legacy method that calls setupEventSubscriptions.
// With the SessionAware pattern, subscriptions are now set up in OnSessionReady
// when the session broker is established. This method remains as a fallback for
// hosts that don't support the session pattern yet.
func (p *LLMCachePlugin) ensureEventSubscription() {
	// Delegate to the centralized setup function (protected by sync.Once)
	p.setupEventSubscriptions()
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
		p.cache.CleanupExpired()
		p.cleanupStalePending()
	}
}

// cleanupStalePending removes pending operations older than 5 minutes
func (p *LLMCachePlugin) cleanupStalePending() {
	threshold := time.Now().Unix() - 300 // 5 minutes

	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()

	for key, op := range p.pendingCache {
		if op.Timestamp < threshold {
			delete(p.pendingCache, key)
		}
	}
}

// RPC method implementations

func (p *LLMCachePlugin) rpcGetMetrics() interface{} {
	// In control plane (studio) mode, return aggregated stats from all edges
	if p.runtime == plugin_sdk.RuntimeStudio {
		return p.getAggregatedMetrics()
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
	// In control plane (studio) mode, initiate distributed cache clear
	if p.runtime == plugin_sdk.RuntimeStudio {
		op := p.initiateDistributedClear()
		return map[string]interface{}{
			"success":      true,
			"message":      "Distributed cache clear initiated",
			"operation_id": op.OperationID,
			"distributed":  true,
		}
	}

	// In gateway (edge) mode, just clear local cache
	p.cache.Clear()
	p.metrics.Reset()
	return map[string]interface{}{
		"success":     true,
		"message":     "Cache cleared successfully",
		"distributed": false,
	}
}

func (p *LLMCachePlugin) rpcGetConfig() interface{} {
	return p.config
}

func (p *LLMCachePlugin) rpcGetClearStatus(payload []byte) interface{} {
	// Parse the request to get operation_id
	var req struct {
		OperationID string `json:"operation_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || req.OperationID == "" {
		return map[string]interface{}{
			"error": "operation_id is required",
		}
	}

	return p.getClearOperationStatus(req.OperationID)
}

// ============================================================================
// Edge-to-Control Communication
// ============================================================================

// reportStatsToControl periodically sends cache statistics to the control plane
// This runs only in gateway (edge) mode
func (p *LLMCachePlugin) reportStatsToControl() {
	interval := time.Duration(p.config.ReportIntervalSeconds) * time.Second
	log.Printf("%s: [INFO] Stats reporter goroutine started, interval=%v", PluginName, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopStatsReporter:
			log.Printf("%s: [INFO] Stats reporter goroutine stopped", PluginName)
			return
		case <-ticker.C:
			log.Printf("%s: [DEBUG] Stats reporter tick - sending stats", PluginName)
			p.sendStatsToControl()
		}
	}
}

// sendStatsToControl collects current stats and sends them to the control plane
func (p *LLMCachePlugin) sendStatsToControl() {
	if p.gatewayServices == nil {
		log.Printf("%s: [WARN] sendStatsToControl: gatewayServices is nil, skipping", PluginName)
		return
	}

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

	log.Printf("%s: [INFO] Sending stats to control: entries=%d, hits=%d, misses=%d, hitRate=%.2f",
		PluginName, entries, stats.HitCount, stats.MissCount, stats.HitRate)

	ctx := context.Background()
	pendingCount, err := p.gatewayServices.SendToControlJSON(ctx, stats, "", map[string]string{
		"metric_type": "llm_cache_stats",
	})
	if err != nil {
		log.Printf("%s: [ERROR] Failed to send stats to control: %v", PluginName, err)
	} else {
		log.Printf("%s: [INFO] Stats sent to control successfully, pending=%d", PluginName, pendingCount)
	}
}

// AcceptEdgePayload implements plugin_sdk.EdgePayloadReceiver
// This is called on the control plane when stats arrive from edge instances
func (p *LLMCachePlugin) AcceptEdgePayload(ctx plugin_sdk.Context, payload *plugin_sdk.EdgePayload) (bool, error) {
	// Check if this payload is for us
	metricType, ok := payload.Metadata["metric_type"]
	if !ok || metricType != "llm_cache_stats" {
		return false, nil
	}

	var stats EdgeCacheStats
	if err := json.Unmarshal(payload.Payload, &stats); err != nil {
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

	// Persist to KV storage for durability across restarts
	if err := p.saveStatsToKV(); err != nil {
		log.Printf("%s: Failed to persist stats: %v", PluginName, err)
	}

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

// ============================================================================
// KV Persistence for Aggregated Stats (Studio Mode)
// ============================================================================

const kvStatsKey = "aggregated_edge_stats"

// persistedStats is the structure saved to KV storage
type persistedStats struct {
	Stats     map[string]*EdgeStatsRecord `json:"stats"`
	UpdatedAt int64                       `json:"updated_at"`
}

// loadStatsFromKV loads persisted aggregated stats from KV storage
func (p *LLMCachePlugin) loadStatsFromKV() error {
	if p.services == nil || p.services.KV() == nil {
		return fmt.Errorf("KV service not available")
	}

	ctx := context.Background()
	data, err := p.services.KV().Read(ctx, kvStatsKey)
	if err != nil {
		// Key not found is not an error - just means no persisted data
		return nil
	}

	if len(data) == 0 {
		// No persisted data, start fresh
		return nil
	}

	var persisted persistedStats
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fmt.Errorf("failed to unmarshal persisted stats: %w", err)
	}

	p.aggregateMu.Lock()
	p.aggregatedStats = persisted.Stats
	if p.aggregatedStats == nil {
		p.aggregatedStats = make(map[string]*EdgeStatsRecord)
	}
	p.aggregateMu.Unlock()

	return nil
}

// saveStatsToKV persists current aggregated stats to KV storage
func (p *LLMCachePlugin) saveStatsToKV() error {
	if p.services == nil || p.services.KV() == nil {
		return fmt.Errorf("KV service not available")
	}

	p.aggregateMu.RLock()
	persisted := persistedStats{
		Stats:     p.aggregatedStats,
		UpdatedAt: time.Now().Unix(),
	}
	p.aggregateMu.RUnlock()

	data, err := json.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	ctx := context.Background()
	_, err = p.services.KV().Write(ctx, kvStatsKey, data, nil)
	if err != nil {
		return fmt.Errorf("failed to write to KV: %w", err)
	}

	return nil
}

// ============================================================================
// Event-based Distributed Cache Clearing
// ============================================================================

const (
	// Event topics for distributed cache clearing
	TopicCacheClear    = "llm-cache.clear"
	TopicCacheClearAck = "llm-cache.clear.ack"

	// Default timeout for waiting for acks (seconds)
	DefaultClearTimeout = 10
)

// unsubscribeEvents cleans up event subscriptions
func (p *LLMCachePlugin) unsubscribeEvents() {
	if p.services == nil || p.services.Events() == nil {
		return
	}

	events := p.services.Events()
	for _, subID := range p.eventSubIDs {
		if err := events.Unsubscribe(subID); err != nil {
			log.Printf("%s: Failed to unsubscribe %s: %v", PluginName, subID, err)
		}
	}
	p.eventSubIDs = nil
}

// handleClearCacheEvent is called on edge instances when control sends a clear command
func (p *LLMCachePlugin) handleClearCacheEvent(event plugin_sdk.Event) {
	log.Printf("%s: [INFO] handleClearCacheEvent called!", PluginName)
	log.Printf("%s: [INFO] Event details: id=%s, topic=%s, origin=%s, dir=%v",
		PluginName, event.ID, event.Topic, event.Origin, event.Dir)

	// Parse the clear event payload
	var clearEvent ClearCacheEvent
	if err := json.Unmarshal(event.Payload, &clearEvent); err != nil {
		log.Printf("%s: [ERROR] Failed to parse clear event: %v", PluginName, err)
		return
	}

	log.Printf("%s: [INFO] Parsed clear event: operationID=%s, timestamp=%d",
		PluginName, clearEvent.OperationID, clearEvent.Timestamp)

	// Clear the local cache
	p.cache.Clear()
	p.metrics.Reset()

	log.Printf("%s: [INFO] Cache cleared (operation %s)", PluginName, clearEvent.OperationID)

	// Send acknowledgement back to control
	log.Printf("%s: [INFO] Sending acknowledgement back to control", PluginName)
	p.sendClearAck(clearEvent.OperationID, true, "")
}

// sendClearAck sends an acknowledgement back to the control plane
func (p *LLMCachePlugin) sendClearAck(operationID string, success bool, message string) {
	log.Printf("%s: [INFO] sendClearAck called, operationID=%s, success=%v", PluginName, operationID, success)

	if p.services == nil {
		log.Printf("%s: [ERROR] Cannot send ack - p.services is nil", PluginName)
		return
	}
	if p.services.Events() == nil {
		log.Printf("%s: [ERROR] Cannot send ack - p.services.Events() is nil", PluginName)
		return
	}

	// Get edge ID from the context (set during initialization or from metadata)
	// In gateway mode, we need to get this from the gateway services or environment
	edgeID := p.getEdgeID()
	log.Printf("%s: [INFO] Using edgeID=%s for ack", PluginName, edgeID)

	ack := ClearCacheAck{
		OperationID: operationID,
		EdgeID:      edgeID,
		Success:     success,
		Message:     message,
		Timestamp:   time.Now().Unix(),
	}

	log.Printf("%s: [INFO] Publishing ack to topic=%s, dir=DirUp", PluginName, TopicCacheClearAck)
	ctx := context.Background()
	if err := p.services.Events().Publish(ctx, TopicCacheClearAck, ack, plugin_sdk.DirUp); err != nil {
		log.Printf("%s: [ERROR] Failed to send clear ack: %v", PluginName, err)
	} else {
		log.Printf("%s: [INFO] Successfully sent cache clear ack for operation %s from edge %s", PluginName, operationID, edgeID)
	}
}

// getEdgeID retrieves the edge ID for this instance
func (p *LLMCachePlugin) getEdgeID() string {
	// Try to get from environment (standard way in edge mode)
	if edgeID := getEnv("EDGE_ID", ""); edgeID != "" {
		return edgeID
	}
	// Fallback to a generated ID
	return fmt.Sprintf("edge-%d", time.Now().UnixNano()%10000)
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := strings.TrimSpace(getEnvRaw(key)); value != "" {
		return value
	}
	return defaultValue
}

// getEnvRaw gets raw environment variable (implemented via os.Getenv)
func getEnvRaw(key string) string {
	// Import os at top of file if not already imported
	return strings.TrimSpace(osGetenv(key))
}

// osGetenv is a wrapper to allow testing
var osGetenv = os.Getenv

// handleClearCacheAck is called on control plane when edge sends an ack
func (p *LLMCachePlugin) handleClearCacheAck(event plugin_sdk.Event) {
	log.Printf("%s: Received cache clear ack from %s", PluginName, event.Origin)

	// Parse the ack payload
	var ack ClearCacheAck
	if err := json.Unmarshal(event.Payload, &ack); err != nil {
		log.Printf("%s: Failed to parse clear ack: %v", PluginName, err)
		return
	}

	// Extract edge ID from the event origin if not in payload
	edgeID := ack.EdgeID
	if edgeID == "" {
		edgeID = extractEdgeIDFromOrigin(event.Origin)
	}

	// Update the operation with this ack
	p.clearOpsMu.Lock()
	defer p.clearOpsMu.Unlock()

	op, exists := p.clearOperations[ack.OperationID]
	if !exists {
		log.Printf("%s: Received ack for unknown operation %s from %s", PluginName, ack.OperationID, edgeID)
		return
	}

	if op.Completed {
		log.Printf("%s: Operation %s already completed, ignoring late ack from %s", PluginName, ack.OperationID, edgeID)
		return
	}

	op.AckEdges[edgeID] = ack.Timestamp
	log.Printf("%s: Recorded ack from %s for operation %s (%d acks)", PluginName, edgeID, ack.OperationID, len(op.AckEdges))
}

// extractEdgeIDFromOrigin extracts edge ID from event origin string
// Origin format: plugin:<plugin_id>@<node_id>
func extractEdgeIDFromOrigin(origin string) string {
	if idx := strings.LastIndex(origin, "@"); idx >= 0 {
		return origin[idx+1:]
	}
	return origin
}

// initiateDistributedClear starts a distributed cache clear operation
func (p *LLMCachePlugin) initiateDistributedClear() *ClearCacheOperation {
	operationID := fmt.Sprintf("clear-%d", time.Now().UnixNano())
	log.Printf("%s: [DEBUG] initiateDistributedClear called, operationID=%s", PluginName, operationID)

	op := &ClearCacheOperation{
		OperationID: operationID,
		StartTime:   time.Now().Unix(),
		Timeout:     DefaultClearTimeout,
		AckEdges:    make(map[string]int64),
		Completed:   false,
	}

	// Store the operation
	p.clearOpsMu.Lock()
	p.clearOperations[operationID] = op
	p.clearOpsMu.Unlock()

	// Publish the clear event to all edges
	if p.services == nil {
		log.Printf("%s: [DEBUG] p.services is nil, cannot publish clear event", PluginName)
	} else if p.services.Events() == nil {
		log.Printf("%s: [DEBUG] p.services.Events() is nil, cannot publish clear event", PluginName)
	} else {
		clearEvent := ClearCacheEvent{
			OperationID: operationID,
			Timestamp:   time.Now().Unix(),
		}

		log.Printf("%s: [DEBUG] Publishing cache clear event to topic=%s, dir=DirDown", PluginName, TopicCacheClear)
		ctx := context.Background()
		if err := p.services.Events().Publish(ctx, TopicCacheClear, clearEvent, plugin_sdk.DirDown); err != nil {
			log.Printf("%s: [ERROR] Failed to publish cache clear event: %v", PluginName, err)
		} else {
			log.Printf("%s: [DEBUG] Successfully published cache clear event (operation %s)", PluginName, operationID)
		}
	}

	// Also clear local cache (control plane might have its own cache in some modes)
	p.cache.Clear()
	p.metrics.Reset()
	log.Printf("%s: [DEBUG] Local cache cleared", PluginName)

	return op
}

// getClearOperationStatus returns the current status of a clear operation
func (p *LLMCachePlugin) getClearOperationStatus(operationID string) map[string]interface{} {
	p.clearOpsMu.RLock()
	defer p.clearOpsMu.RUnlock()

	op, exists := p.clearOperations[operationID]
	if !exists {
		return map[string]interface{}{
			"found": false,
		}
	}

	// Get list of acked edges
	ackedEdges := make([]map[string]interface{}, 0, len(op.AckEdges))
	for edgeID, timestamp := range op.AckEdges {
		ackedEdges = append(ackedEdges, map[string]interface{}{
			"edge_id":   edgeID,
			"timestamp": timestamp,
		})
	}

	// Calculate if operation is timed out
	elapsed := time.Now().Unix() - op.StartTime
	isTimedOut := elapsed > op.Timeout

	return map[string]interface{}{
		"found":        true,
		"operation_id": op.OperationID,
		"start_time":   op.StartTime,
		"timeout":      op.Timeout,
		"elapsed":      elapsed,
		"completed":    op.Completed,
		"timed_out":    isTimedOut,
		"ack_count":    len(op.AckEdges),
		"acked_edges":  ackedEdges,
	}
}

// cleanupOldOperations removes completed/expired operations older than 5 minutes
func (p *LLMCachePlugin) cleanupOldOperations() {
	threshold := time.Now().Unix() - 300 // 5 minutes

	p.clearOpsMu.Lock()
	defer p.clearOpsMu.Unlock()

	for opID, op := range p.clearOperations {
		if op.StartTime < threshold {
			delete(p.clearOperations, opID)
		}
	}
}

func main() {
	plugin := NewLLMCachePlugin()
	plugin_sdk.Serve(plugin)
}
