package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// --- RPC Request/Response types ---

type CreateRuleRequest struct {
	Name       string   `json:"name"`
	Dimensions []string `json:"dimensions"`
	Limit      Limit    `json:"limit"`
	Action     string   `json:"action"`
}

type UpdateRuleRequest struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Dimensions []string `json:"dimensions"`
	Limit      Limit    `json:"limit"`
	Action     string   `json:"action"`
	Enabled    bool     `json:"enabled"`
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

type RuleStatsResponse struct {
	RuleID       string            `json:"rule_id"`
	RuleName     string            `json:"rule_name"`
	LimitType    string            `json:"limit_type"`
	LimitValue   int               `json:"limit_value"`
	CurrentUsage int               `json:"current_usage"`
	WindowReset  string            `json:"window_reset"`
	Dimensions   map[string]string `json:"dimensions,omitempty"`
}

// --- KV key for the rules blob ---

const rulesKVKey = "rules"

// --- RPC handlers ---

func (p *RateLimiterPlugin) rpcListRules() (interface{}, error) {
	rules, err := p.loadRulesFromKV()
	if err != nil {
		return nil, err
	}
	sorted := SortedRules(rules)
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

	rules, err := p.loadRulesFromKV()
	if err != nil {
		return nil, err
	}

	// Set priority to end of list
	rule.Priority = len(rules)
	rules = append(rules, rule)

	if err := p.saveRulesToKV(rules); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}

	log.Printf("rate-limiter: created rule %q (id=%s)", rule.Name, rule.ID)
	return RuleResponse{Success: true, Rule: &rule, Message: "Rule created"}, nil
}

func (p *RateLimiterPlugin) rpcUpdateRule(payload []byte) (interface{}, error) {
	var req UpdateRuleRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	rules, err := p.loadRulesFromKV()
	if err != nil {
		return nil, err
	}

	idx := -1
	for i, r := range rules {
		if r.ID == req.ID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("rule not found: %s", req.ID)
	}

	// Apply updates
	rules[idx].Name = req.Name
	rules[idx].Dimensions = req.Dimensions
	rules[idx].Limit = req.Limit
	rules[idx].Action = req.Action
	rules[idx].Enabled = req.Enabled
	rules[idx].UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := ValidateRule(rules[idx]); err != nil {
		return nil, err
	}

	if err := p.saveRulesToKV(rules); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}

	log.Printf("rate-limiter: updated rule %q (id=%s)", rules[idx].Name, rules[idx].ID)
	return RuleResponse{Success: true, Rule: &rules[idx], Message: "Rule updated"}, nil
}

func (p *RateLimiterPlugin) rpcDeleteRule(payload []byte) (interface{}, error) {
	var req DeleteRuleRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	rules, err := p.loadRulesFromKV()
	if err != nil {
		return nil, err
	}

	found := false
	filtered := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if r.ID == req.ID {
			found = true
			continue
		}
		filtered = append(filtered, r)
	}

	if !found {
		return nil, fmt.Errorf("rule not found: %s", req.ID)
	}

	// Re-assign priorities
	for i := range filtered {
		filtered[i].Priority = i
	}

	if err := p.saveRulesToKV(filtered); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}

	log.Printf("rate-limiter: deleted rule id=%s", req.ID)
	return RuleResponse{Success: true, Message: "Rule deleted"}, nil
}

func (p *RateLimiterPlugin) rpcReorderRules(payload []byte) (interface{}, error) {
	var req ReorderRulesRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	rules, err := p.loadRulesFromKV()
	if err != nil {
		return nil, err
	}

	// Build lookup
	byID := make(map[string]*Rule, len(rules))
	for i := range rules {
		byID[rules[i].ID] = &rules[i]
	}

	reordered := make([]Rule, 0, len(rules))
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

	// Append any rules not in the reorder list (shouldn't happen but be safe)
	for _, r := range rules {
		if !seen[r.ID] {
			r.Priority = len(reordered)
			reordered = append(reordered, r)
		}
	}

	if err := p.saveRulesToKV(reordered); err != nil {
		return nil, fmt.Errorf("failed to save: %v", err)
	}

	return RulesListResponse{Rules: SortedRules(reordered), Count: len(reordered)}, nil
}

func (p *RateLimiterPlugin) rpcGetRuleStats(payload []byte) (interface{}, error) {
	var req GetRuleStatsRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	rules, err := p.loadRulesFromKV()
	if err != nil {
		return nil, err
	}

	var rule *Rule
	for _, r := range rules {
		if r.ID == req.ID {
			rule = &r
			break
		}
	}
	if rule == nil {
		return nil, fmt.Errorf("rule not found: %s", req.ID)
	}

	// For stats we return aggregate info; real per-key stats require knowing the dimension values.
	// Return the rule config so the UI can display it.
	now := time.Now()
	windowEnd := time.Unix(BucketEpoch(now, p.config.WindowSizeSeconds)+int64(p.config.WindowSizeSeconds), 0)

	return RuleStatsResponse{
		RuleID:     rule.ID,
		RuleName:   rule.Name,
		LimitType:  rule.Limit.Type,
		LimitValue: rule.Limit.Value,
		WindowReset: windowEnd.UTC().Format(time.RFC3339),
	}, nil
}

// --- KV persistence ---

func (p *RateLimiterPlugin) loadRulesFromKV() ([]Rule, error) {
	ctx := context.Background()
	data, err := p.store.Get(ctx, rulesKVKey)
	if err != nil {
		// No rules yet
		return []Rule{}, nil
	}

	var rs RuleSet
	if err := json.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("failed to parse rules: %v", err)
	}
	return rs.Rules, nil
}

func (p *RateLimiterPlugin) saveRulesToKV(rules []Rule) error {
	ctx := context.Background()
	rs := RuleSet{
		Rules:     rules,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	// No TTL — rules persist indefinitely
	return p.store.Set(ctx, rulesKVKey, data, 0)
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

	// Cache miss — reload from KV
	rules, err := p.loadRulesFromKV()
	if err != nil {
		log.Printf("rate-limiter: failed to load rules from KV: %v", err)
		// Return stale cache if available
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
