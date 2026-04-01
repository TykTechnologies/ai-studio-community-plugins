package main

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// Rule defines a single rate limit rule.
type Rule struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Match      map[string]string `json:"match,omitempty"`   // optional WHERE clause: all conditions must match
	Dimensions []string          `json:"dimensions"`        // GROUP BY keys for counter bucketing
	Limit      Limit             `json:"limit"`
	Action     string            `json:"action"`            // "enforce" or "log"
	Enabled    bool              `json:"enabled"`
	Priority   int               `json:"priority"`          // lower = evaluated first
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
}

// Limit defines what is being limited and to what value.
type Limit struct {
	Type  string `json:"type"`  // "requests", "tokens", "concurrent"
	Value int    `json:"value"` // max per window or max concurrent
}

// RuleSet is the complete set of rules stored in KV.
// Version is incremented on every write for optimistic concurrency control.
type RuleSet struct {
	Rules     []Rule `json:"rules"`
	UpdatedAt string `json:"updated_at"`
	Version   int    `json:"version"`
}

// ValidDimensions is the set of allowed dimension names.
var ValidDimensions = map[string]bool{
	"app_id":  true,
	"user_id": true,
	"model":   true,
	"llm_id":  true,
	"api_key": true,
	"global":  true,
}

// ValidLimitTypes is the set of allowed limit type names.
var ValidLimitTypes = map[string]bool{
	"requests":   true,
	"tokens":     true,
	"concurrent": true,
}

// SortedRules returns rules sorted by priority (lower first).
func SortedRules(rules []Rule) []Rule {
	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})
	return sorted
}

// ResolveKey builds a composite rate-limit key from the rule's dimensions
// and the current request context. Returns ("", false) if any required
// dimension is missing from the context — the rule should be skipped.
func ResolveKey(rule Rule, pluginCtx *pb.PluginContext, req *pb.EnrichedRequest) (string, bool) {
	parts := make([]string, 0, len(rule.Dimensions))

	for _, dim := range rule.Dimensions {
		val, ok := resolveDimension(dim, pluginCtx, req)
		if !ok {
			return "", false
		}
		parts = append(parts, dim+":"+val)
	}

	// Sort for deterministic key regardless of dimension order in config
	sort.Strings(parts)
	return strings.Join(parts, "|"), true
}

// MatchesContext returns true if the request context satisfies all of the rule's
// match conditions. A rule with no match conditions matches every request.
func MatchesContext(rule Rule, pluginCtx *pb.PluginContext, req *pb.EnrichedRequest) bool {
	for dim, expected := range rule.Match {
		actual, ok := resolveDimension(dim, pluginCtx, req)
		if !ok || actual != expected {
			return false
		}
	}
	return true
}

func resolveDimension(dim string, ctx *pb.PluginContext, req *pb.EnrichedRequest) (string, bool) {
	switch dim {
	case "app_id":
		if ctx.AppId == 0 {
			return "", false
		}
		return fmt.Sprintf("%d", ctx.AppId), true

	case "user_id":
		if ctx.UserId == 0 {
			return "", false
		}
		return fmt.Sprintf("%d", ctx.UserId), true

	case "model":
		if ctx.LlmSlug == "" {
			return "", false
		}
		return ctx.LlmSlug, true

	case "llm_id":
		if ctx.LlmId == 0 {
			return "", false
		}
		return fmt.Sprintf("%d", ctx.LlmId), true

	case "api_key":
		// Try auth claims first
		if req != nil && req.AuthClaims != nil {
			if kid, ok := req.AuthClaims["api_key_id"]; ok && kid != "" {
				return kid, true
			}
		}
		// Fall back to hashed Authorization header
		if req != nil && req.Request != nil && req.Request.Headers != nil {
			if auth, ok := req.Request.Headers["Authorization"]; ok && auth != "" {
				h := sha256.Sum256([]byte(auth))
				return fmt.Sprintf("%x", h), true
			}
		}
		return "", false

	case "global":
		return "_", true

	default:
		return "", false
	}
}

// WindowKey builds the KV key for a windowed counter bucket.
func WindowKey(ruleID, dimensionKey string, bucketEpoch int64) string {
	return fmt.Sprintf("rl:w:%s:%s:%d", ruleID, dimensionKey, bucketEpoch)
}

// ConcurrentKey builds the KV key for a concurrent counter.
func ConcurrentKey(ruleID, dimensionKey string) string {
	return fmt.Sprintf("rl:c:%s:%s", ruleID, dimensionKey)
}

// RequestStateKey builds the KV key for linking post_auth to response phase.
func RequestStateKey(requestID string) string {
	return fmt.Sprintf("rl:req:%s", requestID)
}

// ValidateRule checks that a rule has valid fields.
func ValidateRule(r Rule) error {
	if r.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	// Validate match conditions
	for dim, val := range r.Match {
		if dim == "global" {
			return fmt.Errorf("'global' is not valid as a match condition")
		}
		if !ValidDimensions[dim] {
			return fmt.Errorf("invalid match dimension: %s", dim)
		}
		if val == "" {
			return fmt.Errorf("match value for %s must not be empty", dim)
		}
	}
	if len(r.Dimensions) == 0 && len(r.Match) == 0 {
		return fmt.Errorf("at least one dimension or match condition is required")
	}
	for _, d := range r.Dimensions {
		if !ValidDimensions[d] {
			return fmt.Errorf("invalid dimension: %s", d)
		}
	}
	if !ValidLimitTypes[r.Limit.Type] {
		return fmt.Errorf("invalid limit type: %s (must be requests, tokens, or concurrent)", r.Limit.Type)
	}
	if r.Limit.Value <= 0 {
		return fmt.Errorf("limit value must be > 0")
	}
	if r.Action != "" && r.Action != "enforce" && r.Action != "log" {
		return fmt.Errorf("action must be 'enforce' or 'log'")
	}
	return nil
}
