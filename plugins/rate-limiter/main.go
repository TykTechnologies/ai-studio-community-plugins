package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
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

	// stripedLockCount is the fixed size of the striped lock pool.
	// Must be a power of 2 for efficient modulo via bitmask.
	stripedLockCount = 256
)

// RateLimiterPlugin implements sliding-window rate limiting with composable dimensions.
type RateLimiterPlugin struct {
	plugin_sdk.BasePlugin
	pluginID    uint32
	config      *Config
	store       Store
	mu          sync.RWMutex
	stripeLocks [stripedLockCount]sync.Mutex // bounded lock pool
	rulesCache  *RuleSet
	rulesTTL    time.Time
}

func NewRateLimiterPlugin() *RateLimiterPlugin {
	return &RateLimiterPlugin{
		BasePlugin: plugin_sdk.NewBasePlugin(PluginName, PluginVersion, "Sliding-window rate limiting with composable key dimensions"),
	}
}

// Initialize parses global config and sets up the storage backend.
func (p *RateLimiterPlugin) Initialize(ctx plugin_sdk.Context, config map[string]string) error {
	// Extract plugin ID for service API calls
	if pluginIDStr, ok := config["plugin_id"]; ok {
		fmt.Sscanf(pluginIDStr, "%d", &p.pluginID)
		ai_studio_sdk.SetPluginID(p.pluginID)
	}

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

	// Seed rules from config (these come from the config snapshot on gateways,
	// or from the persisted config on Studio). This ensures gateway instances
	// have rules without needing access to Studio's KV store.
	if len(p.config.Rules) > 0 {
		log.Printf("%s: loaded %d rules from config", PluginName, len(p.config.Rules))
		p.mu.Lock()
		p.rulesCache = &RuleSet{Rules: p.config.Rules}
		if ctx.Runtime == plugin_sdk.RuntimeGateway {
			// On the gateway, rules only change via config push (which triggers a
			// full plugin reload), so the cache never needs to expire and refresh
			// from KV. This avoids depending on Redis for rule storage.
			p.rulesTTL = time.Now().Add(365 * 24 * time.Hour)
		} else {
			p.rulesTTL = time.Now().Add(30 * time.Second)
		}
		p.mu.Unlock()
	} else if ctx.Runtime == plugin_sdk.RuntimeStudio {
		// On Studio, rules may exist in KV from before config sync was added.
		// Sync them to plugin config so they propagate to gateways.
		if rules, err := p.loadRulesFromKV(); err == nil && len(rules) > 0 {
			log.Printf("%s: found %d rules in KV but not in config, syncing to config", PluginName, len(rules))
			p.syncRulesToConfig(rules)
		}
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

// --- SessionAware ---

// OnSessionReady implements plugin_sdk.SessionAware.
// It warms up the broker connection so subsequent RPC calls from the UI don't time out.
func (p *RateLimiterPlugin) OnSessionReady(ctx plugin_sdk.Context) {
	log.Printf("%s: OnSessionReady called - session broker is now active (runtime=%s)", PluginName, ctx.Runtime)

	// Warm up the broker connection by making a lightweight call.
	// On the gateway, use KV (which goes through the microgateway SDK broker).
	// On Studio, use the AI Studio SDK.
	if ctx.Runtime == plugin_sdk.RuntimeGateway {
		log.Printf("%s: Warming up Gateway service API connection...", PluginName)
		_, err := ctx.Services.KV().Read(ctx, "warmup-probe")
		if err != nil {
			log.Printf("%s: Gateway service API warmup completed (probe key not found is expected)", PluginName)
		} else {
			log.Printf("%s: Gateway service API connection established successfully", PluginName)
		}
	} else if ai_studio_sdk.IsInitialized() {
		log.Printf("%s: Warming up Studio service API connection...", PluginName)
		_, err := ai_studio_sdk.GetPluginsCount(context.Background())
		if err != nil {
			log.Printf("%s: Studio service API warmup failed: %v", PluginName, err)
		} else {
			log.Printf("%s: Studio service API connection established successfully", PluginName)
		}
	} else {
		log.Printf("%s: SDK not initialized yet, skipping warmup", PluginName)
	}
}

// OnSessionClosing implements plugin_sdk.SessionAware.
func (p *RateLimiterPlugin) OnSessionClosing(ctx plugin_sdk.Context) {
	log.Printf("%s: OnSessionClosing called", PluginName)
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
	// The sliding window blends the previous and current buckets, so counts
	// don't fully expire until one full window from now — not at the bucket boundary.
	windowEnd := now.Add(time.Duration(W) * time.Second)
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

		// Check match conditions (WHERE clause) before evaluating counters
		if !MatchesContext(rule, pluginCtx, req) {
			continue
		}

		dimensionKey, ok := ResolveKey(rule, pluginCtx, req)
		if !ok {
			continue // missing dimension — skip silently
		}

		// Acquire striped lock for local atomicity (kvStore needs this;
		// redisStore operations are inherently atomic but the lock is harmless)
		lock := p.getLock(rule.ID + ":" + dimensionKey)
		lock.Lock()

		var effectiveCount int
		var storageErr error

		currentKey := WindowKey(rule.ID, dimensionKey, currentEpoch)
		previousKey := WindowKey(rule.ID, dimensionKey, previousEpoch)

		switch rule.Limit.Type {
		case "requests":
			// Atomic: evaluate sliding window + increment current bucket
			effectiveCount, storageErr = p.store.EvalSlidingWindow(ctx, currentKey, previousKey, W, now.Unix(), 1, bucketTTL)

		case "tokens":
			// Evaluate only — tokens counted on response phase
			effectiveCount, storageErr = p.store.EvalSlidingWindow(ctx, currentKey, previousKey, W, now.Unix(), 0, bucketTTL)
			reqState.TokenRuleKeys = append(reqState.TokenRuleKeys, TokenRuleRef{
				RuleID:       rule.ID,
				DimensionKey: dimensionKey,
			})

		case "concurrent":
			concKey := ConcurrentKey(rule.ID, dimensionKey)
			// Atomic check-and-increment: only increments if count < limit
			var allowed bool
			effectiveCount, allowed, storageErr = p.store.IncrementIfBelow(ctx, concKey, rule.Limit.Value, 5*time.Minute)
			if storageErr == nil && allowed {
				reqState.ConcRuleKeys = append(reqState.ConcRuleKeys, concKey)
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
		return &pb.ResponseWriteResponse{Modified: false, Body: req.Body, Headers: req.Headers}, nil
	}

	W := p.config.WindowSizeSeconds
	bucketTTL := time.Duration(2*W) * time.Second

	// Extract actual token usage
	usage := ExtractTokenUsage(req.Body)

	// Update token counters atomically
	if usage.TotalTokens > 0 && len(reqState.TokenRuleKeys) > 0 {
		for _, ref := range reqState.TokenRuleKeys {
			currentKey := WindowKey(ref.RuleID, ref.DimensionKey, reqState.BucketEpoch)
			previousKey := WindowKey(ref.RuleID, ref.DimensionKey, reqState.BucketEpoch-int64(W))
			lock := p.getLock(ref.RuleID + ":" + ref.DimensionKey)
			lock.Lock()
			_, err := p.store.EvalSlidingWindow(ctx, currentKey, previousKey, W, time.Now().Unix(), usage.TotalTokens, bucketTTL)
			if err != nil {
				log.Printf("%s: failed to update token count: %v", PluginName, err)
			}
			lock.Unlock()
		}
	}

	// Decrement concurrent counters atomically
	for _, concKey := range reqState.ConcRuleKeys {
		lock := p.getLock(concKey)
		lock.Lock()
		_, err := p.store.DecrementCounter(ctx, concKey, 5*time.Minute)
		if err != nil {
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

// --- Striped lock pool ---

// getLock returns a mutex from the fixed-size striped pool, selected by hashing the key.
// This bounds memory to exactly stripedLockCount mutexes regardless of key cardinality.
func (p *RateLimiterPlugin) getLock(key string) *sync.Mutex {
	h := fnv.New32a()
	h.Write([]byte(key))
	return &p.stripeLocks[h.Sum32()%stripedLockCount]
}

func main() {
	plugin := NewRateLimiterPlugin()
	plugin_sdk.Serve(plugin)
}
