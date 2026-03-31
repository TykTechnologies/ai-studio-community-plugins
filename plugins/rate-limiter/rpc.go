package main

import (
	"context"
	"encoding/json"
	"errors"
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

// loadRuleSetFromKV reads the full RuleSet including its version.
func (p *RateLimiterPlugin) loadRuleSetFromKV() (*RuleSet, error) {
	ctx := context.Background()
	data, err := p.store.Get(ctx, rulesKVKey)
	if err != nil {
		return &RuleSet{Version: 0}, nil
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
	ctx := context.Background()

	// Re-read current version to detect concurrent modifications
	currentData, err := p.store.Get(ctx, rulesKVKey)
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		return fmt.Errorf("failed to read current rules: %v", err)
	}

	if currentData != nil {
		var current RuleSet
		if err := json.Unmarshal(currentData, &current); err == nil {
			if current.Version != rs.Version {
				return ErrVersionConflict
			}
		}
	}

	// Bump version and save
	rs.Version++
	rs.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(rs)
	if err != nil {
		return err
	}
	return p.store.Set(ctx, rulesKVKey, data, 0)
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
