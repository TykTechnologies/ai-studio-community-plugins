package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/ai_studio_sdk"
	"github.com/google/uuid"
)

// --- RPC Request/Response types ---

type CreateRuleRequest struct {
	Name       string            `json:"name"`
	Match      map[string]string `json:"match,omitempty"`
	Dimensions []string          `json:"dimensions"`
	Limit      Limit             `json:"limit"`
	Action     string            `json:"action"`
}

type UpdateRuleRequest struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Match      map[string]string `json:"match,omitempty"`
	Dimensions []string          `json:"dimensions"`
	Limit      Limit             `json:"limit"`
	Action     string            `json:"action"`
	Enabled    bool              `json:"enabled"`
}

type DeleteRuleRequest struct {
	ID string `json:"id"`
}

type ReorderRulesRequest struct {
	RuleIDs []string `json:"rule_ids"`
}

type GetRuleStatsRequest struct {
	ID string `json:"id"`
}

type RuleResponse struct {
	Success bool   `json:"success"`
	Rule    *Rule  `json:"rule,omitempty"`
	Message string `json:"message,omitempty"`
}

type RulesListResponse struct {
	Rules []Rule `json:"rules"`
	Count int    `json:"count"`
}

// RuleStatsResponse describes a rule's limit and window. It carries no usage
// figure: usage is counted per dimension key (app, user, ...) where requests
// are enforced, on each gateway's KV or in Redis, so there is no single
// rule-level number to report from here.
type RuleStatsResponse struct {
	RuleID      string            `json:"rule_id"`
	RuleName    string            `json:"rule_name"`
	LimitType   string            `json:"limit_type"`
	LimitValue  int               `json:"limit_value"`
	WindowReset string            `json:"window_reset"`
	Dimensions  map[string]string `json:"dimensions,omitempty"`
}

// --- KV key for the rules blob ---

const rulesKVKey = "rules"

// --- RPC handlers ---

func (p *RateLimiterPlugin) rpcListRules() (interface{}, error) {
	rs, err := p.loadRuleSetFromKV()
	if err != nil {
		return nil, err
	}
	sorted := SortedRules(rs.Rules)
	return RulesListResponse{Rules: sorted, Count: len(sorted)}, nil
}

func (p *RateLimiterPlugin) rpcCreateRule(payload []byte) (interface{}, error) {
	var req CreateRuleRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	if req.Action == "" {
		req.Action = "enforce"
	}

	rule := Rule{
		ID:         uuid.New().String(),
		Name:       req.Name,
		Match:      req.Match,
		Dimensions: req.Dimensions,
		Limit:      req.Limit,
		Action:     req.Action,
		Enabled:    true,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	if err := ValidateRule(rule); err != nil {
		return nil, err
	}

	rs, err := p.loadRuleSetFromKV()
	if err != nil {
		return nil, err
	}

	rule.Priority = len(rs.Rules)
	rs.Rules = append(rs.Rules, rule)

	if err := p.saveRuleSetToKV(rs); err != nil {
		return nil, err
	}

	log.Printf("rate-limiter: created rule %q (id=%s)", rule.Name, rule.ID)
	return RuleResponse{Success: true, Rule: &rule, Message: "Rule created"}, nil
}

func (p *RateLimiterPlugin) rpcUpdateRule(payload []byte) (interface{}, error) {
	var req UpdateRuleRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	rs, err := p.loadRuleSetFromKV()
	if err != nil {
		return nil, err
	}

	idx := -1
	for i, r := range rs.Rules {
		if r.ID == req.ID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("rule not found: %s", req.ID)
	}

	rs.Rules[idx].Name = req.Name
	rs.Rules[idx].Match = req.Match
	rs.Rules[idx].Dimensions = req.Dimensions
	rs.Rules[idx].Limit = req.Limit
	rs.Rules[idx].Action = req.Action
	rs.Rules[idx].Enabled = req.Enabled
	rs.Rules[idx].UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := ValidateRule(rs.Rules[idx]); err != nil {
		return nil, err
	}

	if err := p.saveRuleSetToKV(rs); err != nil {
		return nil, err
	}

	log.Printf("rate-limiter: updated rule %q (id=%s)", rs.Rules[idx].Name, rs.Rules[idx].ID)
	return RuleResponse{Success: true, Rule: &rs.Rules[idx], Message: "Rule updated"}, nil
}

func (p *RateLimiterPlugin) rpcDeleteRule(payload []byte) (interface{}, error) {
	var req DeleteRuleRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	rs, err := p.loadRuleSetFromKV()
	if err != nil {
		return nil, err
	}

	found := false
	filtered := make([]Rule, 0, len(rs.Rules))
	for _, r := range rs.Rules {
		if r.ID == req.ID {
			found = true
			continue
		}
		filtered = append(filtered, r)
	}

	if !found {
		return nil, fmt.Errorf("rule not found: %s", req.ID)
	}

	for i := range filtered {
		filtered[i].Priority = i
	}

	rs.Rules = filtered
	if err := p.saveRuleSetToKV(rs); err != nil {
		return nil, err
	}

	log.Printf("rate-limiter: deleted rule id=%s", req.ID)
	return RuleResponse{Success: true, Message: "Rule deleted"}, nil
}

func (p *RateLimiterPlugin) rpcReorderRules(payload []byte) (interface{}, error) {
	var req ReorderRulesRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	rs, err := p.loadRuleSetFromKV()
	if err != nil {
		return nil, err
	}

	byID := make(map[string]*Rule, len(rs.Rules))
	for i := range rs.Rules {
		byID[rs.Rules[i].ID] = &rs.Rules[i]
	}

	reordered := make([]Rule, 0, len(rs.Rules))
	seen := make(map[string]bool)
	for i, id := range req.RuleIDs {
		r, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("rule not found: %s", id)
		}
		r.Priority = i
		reordered = append(reordered, *r)
		seen[id] = true
	}

	for _, r := range rs.Rules {
		if !seen[r.ID] {
			r.Priority = len(reordered)
			reordered = append(reordered, r)
		}
	}

	rs.Rules = reordered
	if err := p.saveRuleSetToKV(rs); err != nil {
		return nil, err
	}

	return RulesListResponse{Rules: SortedRules(reordered), Count: len(reordered)}, nil
}

func (p *RateLimiterPlugin) rpcGetRuleStats(payload []byte) (interface{}, error) {
	var req GetRuleStatsRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	rs, err := p.loadRuleSetFromKV()
	if err != nil {
		return nil, err
	}

	var rule *Rule
	for _, r := range rs.Rules {
		if r.ID == req.ID {
			rule = &r
			break
		}
	}
	if rule == nil {
		return nil, fmt.Errorf("rule not found: %s", req.ID)
	}

	now := time.Now()
	windowEnd := time.Unix(BucketEpoch(now, p.config.WindowSizeSeconds)+int64(p.config.WindowSizeSeconds), 0)

	return RuleStatsResponse{
		RuleID:      rule.ID,
		RuleName:    rule.Name,
		LimitType:   rule.Limit.Type,
		LimitValue:  rule.Limit.Value,
		WindowReset: windowEnd.UTC().Format(time.RFC3339),
	}, nil
}

// --- KV persistence with optimistic locking ---

// ruleStore is where the rule set is kept: the plugin's KV in AI Studio,
// whatever backend counts the requests. The rules used to be read from and
// written to the counter store, so switching storage_backend from kv to redis
// (or back) made the Rules page show none, and the next rule saved from the
// page replaced the stored configuration's rules with that one rule: edges
// lost the others without a word.
func (p *RateLimiterPlugin) ruleStore() Store {
	if p.rulesStore != nil {
		return p.rulesStore
	}
	return p.store
}

// loadRuleSetFromKV reads the full RuleSet including its version.
func (p *RateLimiterPlugin) loadRuleSetFromKV() (*RuleSet, error) {
	return loadRuleSetFrom(p.ruleStore())
}

// loadRuleSetFrom reads a RuleSet from store; a store without one has an
// empty set at version 0.
func loadRuleSetFrom(store Store) (*RuleSet, error) {
	ctx := context.Background()
	data, err := store.Get(ctx, rulesKVKey)
	if errors.Is(err, ErrKeyNotFound) {
		return &RuleSet{Version: 0}, nil
	}
	if err != nil {
		// Not an empty rule set: enforcement keeps its cached rules, and an
		// edit must not save over rules it could not read.
		return nil, fmt.Errorf("failed to read rules: %w", err)
	}

	var rs RuleSet
	if err := json.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("failed to parse rules: %v", err)
	}
	return &rs, nil
}

// saveRuleSetToKV writes the RuleSet with optimistic concurrency control.
// It re-reads the current version from KV and rejects the write if it has
// been modified since the caller's read. This prevents lost updates when
// two administrators edit rules concurrently.
func (p *RateLimiterPlugin) saveRuleSetToKV(rs *RuleSet) error {
	if err := p.storeRuleSet(rs); err != nil {
		return err
	}

	// Sync rules to plugin config so they propagate to edge gateways
	// via the config snapshot. This is a best-effort operation — KV is
	// already saved, so we log but don't fail on config sync errors.
	p.syncRulesToConfig(rs.Rules)

	return nil
}

// storeRuleSet writes the RuleSet to the rule store with the version check,
// without touching the plugin's stored configuration.
func (p *RateLimiterPlugin) storeRuleSet(rs *RuleSet) error {
	ctx := context.Background()
	store := p.ruleStore()

	// The KV has no compare-and-swap, so the version check and the write are
	// made atomic within this plugin process: of two saves based on the same
	// version, one wins and the other gets ErrVersionConflict.
	p.rulesSaveMu.Lock()
	defer p.rulesSaveMu.Unlock()

	// Re-read current version to detect concurrent modifications
	currentData, err := store.Get(ctx, rulesKVKey)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("failed to read current rules: %v", err)
	}

	if currentData != nil {
		var current RuleSet
		if err := json.Unmarshal(currentData, &current); err != nil {
			return fmt.Errorf("corrupted ruleset in KV store (cannot unmarshal): %w", err)
		}
		if current.Version != rs.Version {
			return ErrVersionConflict
		}
	}

	// Bump version and save
	rs.Version++
	rs.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	return store.Set(ctx, rulesKVKey, data, 0)
}

// sameRules reports whether two rule lists are the same rules in the same
// order.
func sameRules(a, b []Rule) bool {
	if len(a) != len(b) {
		return false
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

// reconcileRules runs when the plugin starts in AI Studio. It makes the
// rule store and the stored configuration agree, so the Rules page shows
// exactly the rules gateways enforce:
//
//   - The stored configuration has rules: they are what gateways enforce, so
//     they are what the page must show and what the next edit must start
//     from. A rule store that has other rules, or none (after an upgrade from
//     a version that kept them in Redis, or in a store that was switched
//     away from), takes the configuration's.
//   - The stored configuration has none and the rule store has some (a
//     configuration saved without its "rules" key, or rules from before they
//     were synced): the configuration gets the rule store's.
//   - Neither has any: rules a 1.2.x plugin on the Redis backend kept in
//     Redis are moved to the rule store and the configuration.
//
// In every case the copy 1.2.x left in Redis is deleted once the rule store
// is the place the rules are read from (dropCounterStoreRules). Left there,
// it would come back as case three after an administrator deleted every
// rule, and gateways would enforce deleted rules again.
//
// A rule store that cannot be read is left alone, and so is the
// configuration.
func (p *RateLimiterPlugin) reconcileRules() {
	rs, err := p.loadRuleSetFromKV()
	if err != nil {
		log.Printf("%s: rules not reconciled with the configuration, the rule store cannot be read: %v", PluginName, err)
		return
	}

	configRules := p.config.Rules
	switch {
	case len(configRules) > 0:
		if !sameRules(rs.Rules, configRules) {
			log.Printf("%s: the rule store had %d rules and the stored configuration has %d; the configuration's are the ones gateways enforce, so the Rules page now shows them",
				PluginName, len(rs.Rules), len(configRules))
			rs.Rules = configRules
			if err := p.storeRuleSet(rs); err != nil {
				// The Redis copy stays until the rule store has the rules.
				log.Printf("%s: could not write the configuration's rules to the rule store: %v", PluginName, err)
				return
			}
			p.invalidateRulesCache()
		}
		p.dropCounterStoreRules()

	case len(rs.Rules) > 0:
		log.Printf("%s: found %d rules in the rule store but none in the configuration, syncing to config", PluginName, len(rs.Rules))
		p.syncRulesToConfig(rs.Rules)
		p.dropCounterStoreRules()
		p.invalidateRulesCache()

	case p.countersInRedis():
		legacy, err := loadRuleSetFrom(p.store)
		if err != nil || len(legacy.Rules) == 0 {
			return
		}
		log.Printf("%s: moving %d rules from the Redis counter store to the rule store", PluginName, len(legacy.Rules))
		rs.Rules = legacy.Rules
		if err := p.saveRuleSetToKV(rs); err != nil {
			log.Printf("%s: could not move the rules: %v", PluginName, err)
			return
		}
		p.dropCounterStoreRules()
		p.invalidateRulesCache()
	}
}

// countersInRedis reports whether the counters are in Redis, that is, in a
// store other than the plugin's KV, where the rule store is. It goes by the
// store Initialize actually opened, not by the storage_backend setting: with
// "redis" configured and no redis_url, or a Redis that could not be reached,
// the counters are in the plugin's KV, and deleting "the Redis copy" there
// would delete the rule store's own rules.
func (p *RateLimiterPlugin) countersInRedis() bool {
	return p.rulesStore != nil && p.store != nil && p.countersSeparate
}

// dropCounterStoreRules deletes the rule set a 1.2.x plugin kept in Redis.
// It is called only once the rule store holds the rules. On the kv backend
// the counter store is the plugin's KV, the rule store itself, and nothing
// is deleted.
func (p *RateLimiterPlugin) dropCounterStoreRules() {
	if !p.countersInRedis() {
		return
	}
	ctx := context.Background()
	if _, err := p.store.Get(ctx, rulesKVKey); err != nil {
		return // none there (or unreadable: left alone)
	}
	if err := p.store.Delete(ctx, rulesKVKey); err != nil {
		log.Printf("%s: could not delete the rule set left in Redis by an earlier version: %v", PluginName, err)
		return
	}
	log.Printf("%s: deleted the rule set an earlier version left in Redis; the rules are kept in the plugin's own storage", PluginName)
}

// loadRulesFromKV is a convenience that returns just the rules slice.
func (p *RateLimiterPlugin) loadRulesFromKV() ([]Rule, error) {
	rs, err := p.loadRuleSetFromKV()
	if err != nil {
		return nil, err
	}
	return rs.Rules, nil
}

// loadRulesCached returns rules from in-memory cache, refreshing from KV if stale.
func (p *RateLimiterPlugin) loadRulesCached() []Rule {
	p.mu.RLock()
	if p.rulesCache != nil && time.Now().Before(p.rulesTTL) {
		rules := p.rulesCache.Rules
		p.mu.RUnlock()
		return rules
	}
	p.mu.RUnlock()

	rules, err := p.loadRulesFromKV()
	if err != nil {
		log.Printf("rate-limiter: failed to load rules from KV: %v", err)
		p.mu.RLock()
		defer p.mu.RUnlock()
		if p.rulesCache != nil {
			return p.rulesCache.Rules
		}
		return nil
	}

	p.mu.Lock()
	p.rulesCache = &RuleSet{Rules: rules}
	p.rulesTTL = time.Now().Add(30 * time.Second)
	p.mu.Unlock()

	return rules
}

// invalidateRulesCache forces a reload on next access.
func (p *RateLimiterPlugin) invalidateRulesCache() {
	p.mu.Lock()
	p.rulesTTL = time.Time{}
	p.mu.Unlock()
}

// The Service API calls syncRulesToConfig makes; tests replace them.
var (
	getPluginForSync          = ai_studio_sdk.GetPlugin
	updatePluginConfigForSync = ai_studio_sdk.UpdatePluginConfig
)

// syncRulesToConfig writes the current rules into the plugin's persisted config
// via the Studio Service API. This ensures rules are included in the config snapshot
// that gets pushed to edge gateways. Only works in Studio runtime.
//
// Only the "rules" key is replaced: the other settings are read from the
// stored config, not from the config this process started with, so a
// settings change saved since (Redis backend, password, fail_open, window)
// is kept. If the stored config cannot be read, nothing is written.
func (p *RateLimiterPlugin) syncRulesToConfig(rules []Rule) {
	if p.pluginID == 0 {
		return
	}

	ctx := context.Background()
	got, err := getPluginForSync(ctx, p.pluginID)
	if err != nil {
		log.Printf("%s: config sync skipped, cannot read the stored config: %v", PluginName, err)
		return
	}
	stored := map[string]interface{}{}
	if got != nil && got.Plugin != nil && got.Plugin.ConfigJson != "" {
		if err := json.Unmarshal([]byte(got.Plugin.ConfigJson), &stored); err != nil {
			log.Printf("%s: config sync skipped, stored config is not a JSON object: %v", PluginName, err)
			return
		}
	}
	if rules == nil {
		rules = []Rule{}
	}
	stored["rules"] = rules

	data, err := json.Marshal(stored)
	if err != nil {
		log.Printf("%s: failed to marshal config for sync: %v", PluginName, err)
		return
	}

	resp, err := updatePluginConfigForSync(ctx, p.pluginID, string(data))
	if err != nil {
		log.Printf("%s: config sync failed: %v", PluginName, err)
		return
	}
	if resp != nil && !resp.Success {
		log.Printf("%s: config sync rejected: %s", PluginName, resp.Message)
		return
	}

	log.Printf("%s: synced %d rules to plugin config (will propagate to gateways)", PluginName, len(rules))
}
