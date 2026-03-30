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

//go:embed ui assets manifest.json config.schema.json
var embeddedAssets embed.FS

//go:embed manifest.json
var manifestBytes []byte

//go:embed config.schema.json
var configSchemaBytes []byte

const (
	PluginName    = "rate-limiter"
	PluginVersion = "1.0.0"
)

// RateLimiterPlugin implements sliding-window rate limiting with composable dimensions.
type RateLimiterPlugin struct {
	plugin_sdk.BasePlugin
	config     *Config
	store      Store
	mu         sync.RWMutex
	locks      map[string]*sync.Mutex
	rulesCache *RuleSet
	rulesTTL   time.Time
}

func NewRateLimiterPlugin() *RateLimiterPlugin {
	return &RateLimiterPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(PluginName, PluginVersion, "Sliding-window rate limiting with composable key dimensions"),
		locks:      make(map[string]*sync.Mutex),
	}
}

// Initialize parses global config and sets up the storage backend.
func (p *RateLimiterPlugin) Initialize(ctx plugin_sdk.Context, config map[string]string) error {
	p.config = ParseConfig(config)

	// Initialize storage backend
	if p.config.StorageBackend == "redis" && p.config.RedisURL != "" {
		store, err := newRedisStore(p.config.RedisURL)
		if err != nil {
			log.Printf("%s: redis init failed, falling back to KV: %v", PluginName, err)
			p.store = newKVStore(ctx.Services.KV())
			p.config.StorageBackend = "kv"
		} else {
			p.store = store
			log.Printf("%s: using Redis backend", PluginName)
		}
	} else {
		p.store = newKVStore(ctx.Services.KV())
	}

	log.Printf("%s: initialized (runtime=%s, backend=%s, window=%ds, fail_open=%v)",
		PluginName, ctx.Runtime, p.config.StorageBackend, p.config.WindowSizeSeconds, p.config.FailOpen)
	return nil
}

// Shutdown cleans up resources.
func (p *RateLimiterPlugin) Shutdown(ctx plugin_sdk.Context) error {
	log.Printf("%s: shutting down", PluginName)
	return nil
}

// --- UIProvider ---

func (p *RateLimiterPlugin) GetAsset(assetPath string) ([]byte, string, error) {
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

func (p *RateLimiterPlugin) ListAssets(pathPrefix string) ([]*pb.AssetInfo, error) {
	return []*pb.AssetInfo{}, nil
}

func (p *RateLimiterPlugin) GetManifest() ([]byte, error) {
	return manifestBytes, nil
}

func (p *RateLimiterPlugin) GetConfigSchema() ([]byte, error) {
	return configSchemaBytes, nil
}

// HandleRPC dispatches UI RPC calls.
func (p *RateLimiterPlugin) HandleRPC(method string, payload []byte) ([]byte, error) {
	log.Printf("%s: RPC call: %s", PluginName, method)

	var result interface{}
	var err error

	switch method {
	case "listRules":
		result, err = p.rpcListRules()
	case "createRule":
		result, err = p.rpcCreateRule(payload)
		if err == nil {
			p.invalidateRulesCache()
		}
	case "updateRule":
		result, err = p.rpcUpdateRule(payload)
		if err == nil {
			p.invalidateRulesCache()
		}
	case "deleteRule":
		result, err = p.rpcDeleteRule(payload)
		if err == nil {
			p.invalidateRulesCache()
		}
	case "reorderRules":
		result, err = p.rpcReorderRules(payload)
		if err == nil {
			p.invalidateRulesCache()
		}
	case "getRuleStats":
		result, err = p.rpcGetRuleStats(payload)
	default:
		return nil, fmt.Errorf("unknown RPC method: %s", method)
	}

	if err != nil {
		log.Printf("%s: RPC error (%s): %v", PluginName, method, err)
		return nil, err
	}

	return json.Marshal(result)
}

// --- PostAuth: Evaluate rate limits ---

func (p *RateLimiterPlugin) HandlePostAuth(ctx plugin_sdk.Context, req *pb.EnrichedRequest) (*pb.PluginResponse, error) {
	pluginCtx := req.Request.Context
	if pluginCtx == nil {
		return &pb.PluginResponse{Modified: false}, nil
	}

	rules := p.loadRulesCached()
	if len(rules) == 0 {
		return &pb.PluginResponse{Modified: false}, nil
	}

	now := time.Now()
	W := p.config.WindowSizeSeconds
	currentEpoch := BucketEpoch(now, W)
	previousEpoch := PreviousBucketEpoch(now, W)
	windowEnd := time.Unix(currentEpoch+int64(W), 0)
	bucketTTL := time.Duration(2*W) * time.Second

	sorted := SortedRules(rules)
	responseHeaders := make(map[string]string)
	var rateLimitInfos []RateLimitInfo
	reqState := &RequestState{
		BucketEpoch: currentEpoch,
		Timestamp:   now.Unix(),
	}

	for _, rule := range sorted {
		if !rule.Enabled {
			continue
		}

		dimensionKey, ok := ResolveKey(rule, pluginCtx, req)
		if !ok {
			continue // missing dimension — skip silently
		}

		// Acquire per-key lock for atomic read-modify-write
		lock := p.getLock(rule.ID + ":" + dimensionKey)
		lock.Lock()

		var effectiveCount int
		var storageErr error

		switch rule.Limit.Type {
		case "requests":
			effectiveCount, storageErr = p.evalWindow(ctx, rule, dimensionKey, currentEpoch, previousEpoch, now, W)
			if storageErr == nil {
				// Increment current bucket
				storageErr = p.incrementWindow(ctx, rule, dimensionKey, currentEpoch, 1, bucketTTL)
			}

		case "tokens":
			effectiveCount, storageErr = p.evalWindow(ctx, rule, dimensionKey, currentEpoch, previousEpoch, now, W)
			// Don't increment — tokens counted on response phase
			reqState.TokenRuleKeys = append(reqState.TokenRuleKeys, TokenRuleRef{
				RuleID:       rule.ID,
				DimensionKey: dimensionKey,
			})

		case "concurrent":
			effectiveCount, storageErr = p.evalConcurrent(ctx, rule, dimensionKey)
			if storageErr == nil {
				storageErr = p.incrementConcurrent(ctx, rule, dimensionKey, 1)
				reqState.ConcRuleKeys = append(reqState.ConcRuleKeys, ConcurrentKey(rule.ID, dimensionKey))
			}
		}

		lock.Unlock()

		// Handle storage errors
		if storageErr != nil {
			log.Printf("%s: storage error for rule %q: %v", PluginName, rule.Name, storageErr)
			if p.config.FailOpen {
				MergeHeaders(responseHeaders, StorageErrorHeaders())
				continue
			}
			return &pb.PluginResponse{
				Block:      true,
				StatusCode: 503,
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       []byte(`{"error":"rate limiter storage unavailable"}`),
			}, nil
		}

		info := RateLimitInfo{
			RuleName:  rule.Name,
			LimitType: rule.Limit.Type,
			Limit:     rule.Limit.Value,
			Current:   effectiveCount,
			Action:    rule.Action,
			ResetAt:   windowEnd,
		}
		rateLimitInfos = append(rateLimitInfos, info)

		// Check breach
		if effectiveCount >= rule.Limit.Value {
			if rule.Action == "log" {
				log.Printf("%s: shadow breach on rule %q (%s): %d/%d", PluginName, rule.Name, rule.Limit.Type, effectiveCount, rule.Limit.Value)
				MergeHeaders(responseHeaders, ShadowBreachHeaders(rule.Name))
				continue
			}
			// Enforce: block the request
			log.Printf("%s: enforcing breach on rule %q (%s): %d/%d", PluginName, rule.Name, rule.Limit.Type, effectiveCount, rule.Limit.Value)
			// Save request state so response phase can still decrement concurrent counters
			if len(reqState.ConcRuleKeys) > 0 || len(reqState.TokenRuleKeys) > 0 {
				WriteRequestState(ctx, p.store, RequestStateKey(pluginCtx.RequestId), reqState, 5*time.Minute)
			}
			return BlockResponse(info), nil
		}
	}

	// Save request state for response phase
	if len(reqState.ConcRuleKeys) > 0 || len(reqState.TokenRuleKeys) > 0 {
		if err := WriteRequestState(ctx, p.store, RequestStateKey(pluginCtx.RequestId), reqState, 5*time.Minute); err != nil {
			log.Printf("%s: failed to save request state: %v", PluginName, err)
		}
	}

	// Build response with rate limit headers
	if h := MostRestrictiveHeaders(rateLimitInfos); h != nil {
		MergeHeaders(responseHeaders, h)
	}

	if len(responseHeaders) > 0 {
		return &pb.PluginResponse{Modified: true, Headers: responseHeaders}, nil
	}
	return &pb.PluginResponse{Modified: false}, nil
}

// --- Response: Record tokens and decrement concurrency ---

func (p *RateLimiterPlugin) OnBeforeWriteHeaders(ctx plugin_sdk.Context, req *pb.HeadersRequest) (*pb.HeadersResponse, error) {
	return &pb.HeadersResponse{Modified: false, Headers: req.Headers}, nil
}

func (p *RateLimiterPlugin) OnBeforeWrite(ctx plugin_sdk.Context, req *pb.ResponseWriteRequest) (*pb.ResponseWriteResponse, error) {
	pluginCtx := req.Context
	if pluginCtx == nil {
		return &pb.ResponseWriteResponse{Modified: false, Body: req.Body, Headers: req.Headers}, nil
	}

	// Skip streaming chunks
	if req.IsStreamChunk {
		return &pb.ResponseWriteResponse{Modified: false, Body: req.Body, Headers: req.Headers}, nil
	}

	// Load request state
	reqState, err := ReadRequestState(ctx, p.store, RequestStateKey(pluginCtx.RequestId))
	if err != nil {
		// No request state — post_auth may have skipped or state expired
		return &pb.ResponseWriteResponse{Modified: false, Body: req.Body, Headers: req.Headers}, nil
	}

	now := time.Now()
	W := p.config.WindowSizeSeconds
	bucketTTL := time.Duration(2*W) * time.Second

	// Extract actual token usage
	usage := ExtractTokenUsage(req.Body)

	// Update token counters
	if usage.TotalTokens > 0 && len(reqState.TokenRuleKeys) > 0 {
		for _, ref := range reqState.TokenRuleKeys {
			// Use the bucket epoch from post_auth to update the correct window
			key := WindowKey(ref.RuleID, ref.DimensionKey, reqState.BucketEpoch)
			lock := p.getLock(ref.RuleID + ":" + ref.DimensionKey)
			lock.Lock()
			state := ReadWindowState(ctx, p.store, key)
			state.Count += usage.TotalTokens
			state.UpdatedAt = now.Unix()
			if err := WriteWindowState(ctx, p.store, key, state, bucketTTL); err != nil {
				log.Printf("%s: failed to update token count: %v", PluginName, err)
			}
			lock.Unlock()
		}
	}

	// Decrement concurrent counters
	for _, concKey := range reqState.ConcRuleKeys {
		// Extract rule+dimension from the key to get lock
		lock := p.getLock(concKey)
		lock.Lock()
		state := ReadConcurrentState(ctx, p.store, concKey)
		state.Count--
		if state.Count < 0 {
			state.Count = 0
		}
		state.UpdatedAt = now.Unix()
		if err := WriteConcurrentState(ctx, p.store, concKey, state, 5*time.Minute); err != nil {
			log.Printf("%s: failed to decrement concurrent: %v", PluginName, err)
		}
		lock.Unlock()
	}

	// Cleanup request state
	p.store.Delete(ctx, RequestStateKey(pluginCtx.RequestId))

	// Add tracking headers to response
	responseHeaders := make(map[string]string)
	for k, v := range req.Headers {
		responseHeaders[k] = v
	}

	if usage.TotalTokens > 0 {
		responseHeaders["X-RateLimit-Tokens-Used"] = fmt.Sprintf("%d", usage.TotalTokens)
	}

	return &pb.ResponseWriteResponse{
		Modified: true,
		Body:     req.Body,
		Headers:  responseHeaders,
	}, nil
}

// --- Internal helpers ---

func (p *RateLimiterPlugin) evalWindow(ctx plugin_sdk.Context, rule Rule, dimensionKey string, currentEpoch, previousEpoch int64, now time.Time, windowSeconds int) (int, error) {
	currentKey := WindowKey(rule.ID, dimensionKey, currentEpoch)
	previousKey := WindowKey(rule.ID, dimensionKey, previousEpoch)

	currentState := ReadWindowState(ctx, p.store, currentKey)
	previousState := ReadWindowState(ctx, p.store, previousKey)

	return SlidingWindowCount(previousState.Count, currentState.Count, now, windowSeconds), nil
}

func (p *RateLimiterPlugin) incrementWindow(ctx plugin_sdk.Context, rule Rule, dimensionKey string, bucketEpoch int64, delta int, ttl time.Duration) error {
	key := WindowKey(rule.ID, dimensionKey, bucketEpoch)
	state := ReadWindowState(ctx, p.store, key)
	state.Count += delta
	state.UpdatedAt = time.Now().Unix()
	return WriteWindowState(ctx, p.store, key, state, ttl)
}

func (p *RateLimiterPlugin) evalConcurrent(ctx plugin_sdk.Context, rule Rule, dimensionKey string) (int, error) {
	key := ConcurrentKey(rule.ID, dimensionKey)
	state := ReadConcurrentState(ctx, p.store, key)
	return state.Count, nil
}

func (p *RateLimiterPlugin) incrementConcurrent(ctx plugin_sdk.Context, rule Rule, dimensionKey string, delta int) error {
	key := ConcurrentKey(rule.ID, dimensionKey)
	state := ReadConcurrentState(ctx, p.store, key)
	state.Count += delta
	state.UpdatedAt = time.Now().Unix()
	return WriteConcurrentState(ctx, p.store, key, state, 5*time.Minute)
}

func (p *RateLimiterPlugin) getLock(key string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.locks[key]; !ok {
		p.locks[key] = &sync.Mutex{}
	}
	return p.locks[key]
}

func main() {
	plugin := NewRateLimiterPlugin()
	plugin_sdk.Serve(plugin)
}
