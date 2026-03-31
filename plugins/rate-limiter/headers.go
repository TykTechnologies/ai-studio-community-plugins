package main

import (
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

// RateLimitInfo captures the state of a single evaluated rule for header generation.
type RateLimitInfo struct {
	RuleName  string
	LimitType string // "requests", "tokens", "concurrent"
	Limit     int
	Current   int
	Action    string // "enforce" or "log"
	ResetAt   time.Time
}

// Remaining returns limit - current, floored at 0.
func (r RateLimitInfo) Remaining() int {
	rem := r.Limit - r.Current
	if rem < 0 {
		return 0
	}
	return rem
}

// MostRestrictiveHeaders picks the evaluated rule with the lowest remaining
// and generates standard rate limit headers.
func MostRestrictiveHeaders(infos []RateLimitInfo) map[string]string {
	if len(infos) == 0 {
		return nil
	}

	// Find the most restrictive (lowest remaining)
	best := infos[0]
	for _, info := range infos[1:] {
		if info.Remaining() < best.Remaining() {
			best = info
		}
	}

	headers := map[string]string{
		"X-RateLimit-Limit":     fmt.Sprintf("%d", best.Limit),
		"X-RateLimit-Remaining": fmt.Sprintf("%d", best.Remaining()),
		"X-RateLimit-Reset":     fmt.Sprintf("%d", best.ResetAt.Unix()),
	}

	return headers
}

// BlockResponse builds a 429 PluginResponse for a rate limit breach.
func BlockResponse(info RateLimitInfo) *pb.PluginResponse {
	retryAfter := int(time.Until(info.ResetAt).Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}

	errorBody := map[string]interface{}{
		"error":         "Rate limit exceeded",
		"rule":          info.RuleName,
		"limit_type":    info.LimitType,
		"limit_value":   info.Limit,
		"current_usage": info.Current,
		"reset_at":      info.ResetAt.Format(time.RFC3339),
	}
	body, _ := json.Marshal(errorBody)

	return &pb.PluginResponse{
		Block:      true,
		StatusCode: 429,
		Headers: map[string]string{
			"Content-Type":        "application/json",
			"X-RateLimit-Limit":   fmt.Sprintf("%d", info.Limit),
			"X-RateLimit-Remaining": "0",
			"X-RateLimit-Reset":   fmt.Sprintf("%d", info.ResetAt.Unix()),
			"Retry-After":         fmt.Sprintf("%d", retryAfter),
		},
		Body: body,
	}
}

// ShadowBreachHeaders returns headers to add when a shadow-mode rule is breached.
func ShadowBreachHeaders(ruleName string) map[string]string {
	return map[string]string{
		"X-RateLimit-Shadow-Breach": ruleName,
	}
}

// StorageErrorHeaders returns headers to add when storage is unavailable and fail-open is active.
func StorageErrorHeaders() map[string]string {
	return map[string]string{
		"X-RateLimit-Error": "storage_unavailable",
	}
}

// MergeHeaders copies all entries from src into dst.
func MergeHeaders(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}
