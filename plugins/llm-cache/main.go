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
	"github.com/google/uuid"
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
	PluginVersion = "1.2.1"
)

// PendingCacheOp represents a pending cache operation
type PendingCacheOp struct {
	CacheKey    string
	Namespace   string
	Model       string
	Timestamp   int64
	ShouldCache bool
	// Bypassed is set when the client asked to skip the cache (as opposed to
	// a response the cache cannot store or replay).
	Bypassed bool
}

// ClearCacheOperation tracks a pending distributed cache clear operation
type ClearCacheOperation struct {
	OperationID string           `json:"operation_id"`
	StartTime   int64            `json:"start_time"`
	Timeout     int64            `json:"timeout"`   // seconds
	AckEdges    map[string]int64 `json:"ack_edges"` // edge_id -> timestamp
	Completed   bool             `json:"completed"`
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
	aggregateMu     sync.RWMutex
	aggregatedStats map[string]*EdgeStatsRecord

	// Services reference for KV persistence (works in both modes)
	services plugin_sdk.ServiceBroker

	// Background loops (cache cleanup, edge stats reporter) run until
	// Shutdown cancels bgCtx; Shutdown waits for them on bgWG.
	bgCtx       context.Context
	bgCancel    context.CancelFunc
	bgWG        sync.WaitGroup
	bgMu        sync.Mutex // orders bgWG.Add against Shutdown's cancel and Wait
	cleanupOnce sync.Once
	// The reporter starts once, however often OnSessionReady runs
	reporterOnce sync.Once

	// fallbackEdgeID names this edge in clear acks when EDGE_ID is unset
	fallbackEdgeID string

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
	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &LLMCachePlugin{
		BasePlugin:      plugin_sdk.NewBasePlugin(PluginName, PluginVersion, "LLM Response Cache - reduces costs and latency by caching identical requests"),
		pendingCache:    make(map[string]*PendingCacheOp),
		metrics:         NewCacheMetrics(),
		aggregatedStats: make(map[string]*EdgeStatsRecord),
		bgCtx:           bgCtx,
		bgCancel:        bgCancel,
		fallbackEdgeID:  "edge-" + uuid.NewString(),
		clearOperations: make(map[string]*ClearCacheOperation),
		eventSubIDs:     make([]string, 0),
	}
}

// logger returns the SDK's levelled logger. Before Initialize, and in tests
// that run without a service broker, it falls back to the standard logger.
func (p *LLMCachePlugin) logger() plugin_sdk.LogService {
	if p.services != nil {
		if l := p.services.Logger(); l != nil {
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

// goBackground runs loop until Shutdown. It does nothing after Shutdown.
func (p *LLMCachePlugin) goBackground(loop func(ctx context.Context)) {
	p.bgMu.Lock()
	defer p.bgMu.Unlock()
	if p.bgCtx.Err() != nil {
		return
	}
	p.bgWG.Add(1)
	go func() {
		defer p.bgWG.Done()
		loop(p.bgCtx)
	}()
}

// Initialize implements plugin_sdk.Plugin
func (p *LLMCachePlugin) Initialize(ctx plugin_sdk.Context, configMap map[string]string) error {
	// Store runtime for mode-specific behavior
	p.runtime = ctx.Runtime

	// Store services reference for logging and event pub/sub (needed in both modes)
	p.services = ctx.Services

	// Parse configuration
	config, err := ParseConfig(configMap)
	if err != nil {
		p.logger().Warn("Config parse error, using defaults", "error", err)
		config = DefaultConfig()
	}
	p.config = config

	// Initialize cache with config values (used in gateway mode)
	p.cache = NewMemoryCache(
		config.MaxCacheSizeMB,
		config.MaxEntrySizeKB,
		config.TTLSeconds,
	)

	// Start the periodic cleanup loop once, however often Initialize runs
	p.cleanupOnce.Do(func() { p.goBackground(p.periodicCleanup) })

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
				p.logger().Warn("Failed to load persisted stats", "error", err)
			}
		}

		// NOTE: Event subscriptions are set up lazily during HandleRPC
		// because the broker connection is only active during RPC calls.
		// See ensureEventSubscription() which is called from HandleRPC.
	}

	return nil
}

// Shutdown implements plugin_sdk.Plugin. It stops every background loop,
// waits for them to return, and is safe to call more than once.
func (p *LLMCachePlugin) Shutdown(ctx plugin_sdk.Context) error {
	p.bgMu.Lock()
	p.bgCancel()
	p.bgMu.Unlock()
	p.bgWG.Wait()

	// Unsubscribe from events
	p.unsubscribeEvents()

	return nil
}

// OnSessionReady implements plugin_sdk.SessionAware
// This is called when the session-based broker connection is first established.
// The broker connection stays alive during the session, making it ideal for
// setting up event subscriptions and starting background services that need the broker.
func (p *LLMCachePlugin) OnSessionReady(ctx plugin_sdk.Context) {
	p.logger().Debug("Session broker active", "runtime", p.runtime)

	// Warm up the Service API connection first to ensure broker is ready
	// This prevents timeout errors on subsequent service API calls
	if p.runtime == plugin_sdk.RuntimeGateway {
		// A missing probe key is the expected result; the call only opens the connection
		_, _ = ctx.Services.KV().Read(ctx, "warmup-probe")
	}

	// Set up event subscriptions now that we have a stable broker connection
	p.setupEventSubscriptions()

	// In gateway mode, start the stats reporter now that the broker is ready
	if p.runtime == plugin_sdk.RuntimeGateway && p.gatewayServices != nil {
		p.reporterOnce.Do(func() {
			p.logger().Debug("Starting stats reporter")
			p.goBackground(p.reportStatsToControl)
		})
	}
}

// OnSessionClosing implements plugin_sdk.SessionAware
// This is called before the session is explicitly closed (not on timeout).
func (p *LLMCachePlugin) OnSessionClosing(ctx plugin_sdk.Context) {
	p.logger().Debug("Session closing, removing event subscriptions")

	// Clean up event subscriptions
	p.unsubscribeEvents()
}

// setupEventSubscriptions sets up the event subscriptions for distributed cache clearing.
// This is called from OnSessionReady when we have a stable broker connection.
func (p *LLMCachePlugin) setupEventSubscriptions() {
	// Use sync.Once to ensure we only set up subscriptions once
	p.eventSubOnce.Do(func() {
		if p.services == nil || p.services.Events() == nil {
			p.logger().Warn("Events service unavailable, distributed cache clearing is disabled")
			return
		}
		events := p.services.Events()

		// Edge mode: subscribe to cache clear commands from control.
		// Control mode: subscribe to cache clear acknowledgements from edges.
		topic, handler := TopicCacheClearAck, p.handleClearCacheAck
		if p.runtime == plugin_sdk.RuntimeGateway {
			topic, handler = TopicCacheClear, p.handleClearCacheEvent
		}

		subID, err := events.Subscribe(topic, handler)
		if err != nil {
			p.logger().Error("Failed to subscribe to events", "topic", topic, "error", err)
			return
		}
		p.eventSubIDs = append(p.eventSubIDs, subID)
		p.logger().Debug("Subscribed to events", "topic", topic, "subscription_id", subID)
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
				p.logger().Warn("Broker warmup failed", "error", err)
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
			Bypassed:    true,
		}
		p.pendingMu.Unlock()

		return &pb.PluginResponse{Modified: false}, nil
	}

	// Generate namespace
	namespace := p.getNamespace(ctx)

	// The key covers the LLM and the API the request is for, not only the
	// body: two LLMs may see the same model name and messages.
	vendor := pluginCtx.Vendor
	scope := CacheScope{Namespace: namespace, LLMID: pluginCtx.LlmId, Vendor: vendor, Path: apiPath(pluginReq.Path)}
	cacheKey, model, err := GenerateCacheKey(scope, pluginReq.Body, p.config.NormalizePrompts)
	if err != nil {
		return &pb.PluginResponse{Modified: false}, nil
	}

	format := apiFormat(vendor)
	streaming := requestStreams(vendor, pluginReq.Path, pluginReq.Body)
	if streaming && (format == "" || !p.config.CacheStreamingResponses) {
		// The cache can neither replay a stream for this vendor nor store
		// one: the request goes upstream uncached.
		p.metrics.IncrementMiss()
		p.pendingMu.Lock()
		p.pendingCache[requestID] = &PendingCacheOp{Timestamp: time.Now().Unix(), ShouldCache: false}
		p.pendingMu.Unlock()
		return &pb.PluginResponse{Modified: false}, nil
	}

	// Check cache for HIT. Entries hold JSON; a streaming request gets it
	// converted to the vendor's stream, when that loses nothing.
	entry := p.cache.Get(cacheKey)
	if entry != nil && (!streaming || jsonReplayableAsStream(format, entry.Response)) {
		responseBody := entry.Response
		if streaming {
			sseResponse, err := ConvertJSONToSSE(entry.Response, vendor)
			if err != nil {
				p.logger().Warn("Failed to convert cached response to SSE", "error", err)
				responseBody = nil
			} else {
				responseBody = sseResponse
			}
		}

		if responseBody != nil {
			p.metrics.IncrementHit()
			p.metrics.AddTokensSaved(int64(entry.TokensSaved))

			headers := hitHeaders(entry.Headers, responseBody, streaming)
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
				Body:       responseBody,
				Modified:   true,
			}, nil
		}
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
		modifiedHeaders["X-Cache-Status"] = "MISS"
		if pendingOp.Bypassed {
			modifiedHeaders["X-Cache-Status"] = "BYPASS"
		}
		return &pb.ResponseWriteResponse{
			Modified: true,
			Body:     req.Body,
			Headers:  modifiedHeaders,
		}, nil
	}

	// Check if response indicates an error (don't cache errors), and only
	// store JSON: entries are replayed as JSON or converted from it.
	if p.isErrorResponse(req.Body) || !json.Valid(req.Body) {
		modifiedHeaders["X-Cache-Status"] = "MISS"
		return &pb.ResponseWriteResponse{
			Modified: true,
			Body:     req.Body,
			Headers:  modifiedHeaders,
		}, nil
	}

	// Extract token usage for metrics
	tokensSaved := ExtractTokensFromResponse(req.Body)

	// Store in cache, without the headers that describe this particular
	// response on the wire (a hit sets its own, see hitHeaders)
	p.cache.Set(
		pendingOp.CacheKey,
		req.Body,
		storableHeaders(req.Headers),
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
		return &pb.StreamCompleteResponse{Handled: true, Cached: false}, nil
	}

	// Get vendor from context metadata for SSE parsing (the gateway sets it
	// there on this hook)
	vendor := pluginCtx.Vendor
	if pluginCtx.Metadata != nil && pluginCtx.Metadata["vendor"] != "" {
		vendor = pluginCtx.Metadata["vendor"]
	}

	// Only a stream the cache can rebuild and replay without losing anything
	// is stored: plain text in one choice (no tool calls or thinking).
	if !streamReplayable(apiFormat(vendor), req.AccumulatedResponse) {
		return &pb.StreamCompleteResponse{Handled: true, Cached: false}, nil
	}

	// Reconstruct a JSON response from the SSE stream
	reconstructedJSON, tokenUsage, err := ReconstructResponseFromSSE(req.AccumulatedResponse, vendor)
	if err != nil {
		p.logger().Warn("Failed to reconstruct response from SSE", "request_id", requestID, "error", err)
		return &pb.StreamCompleteResponse{Handled: true, Cached: false, ErrorMessage: err.Error()}, nil
	}

	// Check if the reconstructed response indicates an error
	if p.isErrorResponse(reconstructedJSON) {
		return &pb.StreamCompleteResponse{Handled: true, Cached: false}, nil
	}

	// Store in cache - store the reconstructed JSON, not the raw SSE
	// When a cache HIT occurs, HandlePostAuth will convert it back to SSE if needed
	p.cache.Set(
		pendingOp.CacheKey,
		reconstructedJSON,
		storableHeaders(req.Headers),
		pendingOp.Model,
		tokenUsage,
		nil, // Use default TTL
	)

	p.logger().Debug("Cached streaming response", "request_id", requestID, "tokens", tokenUsage)

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
			p.logger().Debug("Using per-request event service broker", "broker_id", brokerID)
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

// periodicCleanup runs periodic cache maintenance until ctx is cancelled
func (p *LLMCachePlugin) periodicCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.cleanup()
		}
	}
}

// cleanup drops expired cache entries, stale pending operations and old
// distributed clear operations
func (p *LLMCachePlugin) cleanup() {
	p.cache.CleanupExpired()
	p.cleanupStalePending()
	p.cleanupOldOperations()
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

// reportStatsToControl periodically sends cache statistics to the control
// plane until ctx is cancelled. This runs only in gateway (edge) mode.
func (p *LLMCachePlugin) reportStatsToControl(ctx context.Context) {
	interval := time.Duration(p.config.ReportIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.sendStatsToControl(ctx)
		}
	}
}

// sendStatsToControl collects current stats and sends them to the control plane
func (p *LLMCachePlugin) sendStatsToControl(ctx context.Context) {
	if p.gatewayServices == nil {
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

	pendingCount, err := p.gatewayServices.SendToControlJSON(ctx, stats, "", map[string]string{
		"metric_type": "llm_cache_stats",
	})
	if err != nil {
		p.logger().Error("Failed to send stats to control", "error", err)
		return
	}
	p.logger().Debug("Sent stats to control", "entries", entries, "hits", stats.HitCount,
		"misses", stats.MissCount, "pending", pendingCount)
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
		p.logger().Warn("Failed to persist edge stats", "error", err)
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
			p.logger().Warn("Failed to unsubscribe from events", "subscription_id", subID, "error", err)
		}
	}
	p.eventSubIDs = nil
}

// handleClearCacheEvent is called on edge instances when control sends a clear command
func (p *LLMCachePlugin) handleClearCacheEvent(event plugin_sdk.Event) {
	// Parse the clear event payload
	var clearEvent ClearCacheEvent
	if err := json.Unmarshal(event.Payload, &clearEvent); err != nil {
		p.logger().Error("Failed to parse cache clear event", "origin", event.Origin, "error", err)
		return
	}

	// Clear the local cache
	p.cache.Clear()
	p.metrics.Reset()
	p.logger().Info("Cache cleared by control plane", "operation_id", clearEvent.OperationID)

	// Send acknowledgement back to control
	p.sendClearAck(clearEvent.OperationID, true, "")
}

// sendClearAck sends an acknowledgement back to the control plane
func (p *LLMCachePlugin) sendClearAck(operationID string, success bool, message string) {
	if p.services == nil || p.services.Events() == nil {
		p.logger().Error("Events service unavailable, cannot acknowledge cache clear", "operation_id", operationID)
		return
	}

	edgeID := p.getEdgeID()
	ack := ClearCacheAck{
		OperationID: operationID,
		EdgeID:      edgeID,
		Success:     success,
		Message:     message,
		Timestamp:   time.Now().Unix(),
	}

	if err := p.services.Events().Publish(p.bgCtx, TopicCacheClearAck, ack, plugin_sdk.DirUp); err != nil {
		p.logger().Error("Failed to send cache clear ack", "operation_id", operationID, "error", err)
		return
	}
	p.logger().Debug("Sent cache clear ack", "operation_id", operationID, "edge_id", edgeID)
}

// getEdgeID returns EDGE_ID from the environment, else an ID generated once
// per plugin process (a UUID, so two edges never share it).
func (p *LLMCachePlugin) getEdgeID() string {
	if edgeID := strings.TrimSpace(os.Getenv("EDGE_ID")); edgeID != "" {
		return edgeID
	}
	return p.fallbackEdgeID
}

// handleClearCacheAck is called on control plane when edge sends an ack
func (p *LLMCachePlugin) handleClearCacheAck(event plugin_sdk.Event) {
	// Parse the ack payload
	var ack ClearCacheAck
	if err := json.Unmarshal(event.Payload, &ack); err != nil {
		p.logger().Error("Failed to parse cache clear ack", "origin", event.Origin, "error", err)
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
		p.logger().Debug("Ignoring cache clear ack for unknown operation", "operation_id", ack.OperationID, "edge_id", edgeID)
		return
	}

	if op.Completed {
		p.logger().Debug("Ignoring late cache clear ack", "operation_id", ack.OperationID, "edge_id", edgeID)
		return
	}

	op.AckEdges[edgeID] = ack.Timestamp
	p.logger().Debug("Recorded cache clear ack", "operation_id", ack.OperationID, "edge_id", edgeID, "acks", len(op.AckEdges))
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
	if p.services == nil || p.services.Events() == nil {
		p.logger().Warn("Events service unavailable, edges will not be cleared", "operation_id", operationID)
	} else {
		clearEvent := ClearCacheEvent{
			OperationID: operationID,
			Timestamp:   time.Now().Unix(),
		}
		if err := p.services.Events().Publish(p.bgCtx, TopicCacheClear, clearEvent, plugin_sdk.DirDown); err != nil {
			p.logger().Error("Failed to publish cache clear event", "operation_id", operationID, "error", err)
		} else {
			p.logger().Info("Distributed cache clear started", "operation_id", operationID)
		}
	}

	// Also clear local cache (control plane might have its own cache in some modes)
	p.cache.Clear()
	p.metrics.Reset()

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
