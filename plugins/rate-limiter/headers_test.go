package main

import (
	"testing"
	"time"
)

func TestRateLimitInfo_Remaining(t *testing.T) {
	info := RateLimitInfo{Limit: 100, Current: 60}
	if info.Remaining() != 40 {
		t.Errorf("expected 40, got %d", info.Remaining())
	}

	info = RateLimitInfo{Limit: 100, Current: 150}
	if info.Remaining() != 0 {
		t.Errorf("expected 0 (floored), got %d", info.Remaining())
	}
}

func TestMostRestrictiveHeaders(t *testing.T) {
	reset := time.Date(2024, 4, 1, 0, 2, 0, 0, time.UTC)
	infos := []RateLimitInfo{
		{RuleName: "wide", Limit: 1000, Current: 100, ResetAt: reset},
		{RuleName: "tight", Limit: 50, Current: 45, ResetAt: reset},
		{RuleName: "mid", Limit: 200, Current: 150, ResetAt: reset},
	}

	headers := MostRestrictiveHeaders(infos)
	if headers["X-RateLimit-Limit"] != "50" {
		t.Errorf("expected limit=50, got %s", headers["X-RateLimit-Limit"])
	}
	if headers["X-RateLimit-Remaining"] != "5" {
		t.Errorf("expected remaining=5, got %s", headers["X-RateLimit-Remaining"])
	}
}

func TestMostRestrictiveHeaders_Empty(t *testing.T) {
	headers := MostRestrictiveHeaders(nil)
	if headers != nil {
		t.Errorf("expected nil for empty infos, got %v", headers)
	}
}

func TestBlockResponse(t *testing.T) {
	reset := time.Now().Add(30 * time.Second)
	info := RateLimitInfo{
		RuleName:  "test-rule",
		LimitType: "requests",
		Limit:     100,
		Current:   100,
		ResetAt:   reset,
	}

	resp := BlockResponse(info)
	if !resp.Block {
		t.Error("expected Block=true")
	}
	if resp.StatusCode != 429 {
		t.Errorf("expected 429, got %d", resp.StatusCode)
	}
	if resp.Headers["Content-Type"] != "application/json" {
		t.Error("expected application/json content type")
	}
	if resp.Headers["X-RateLimit-Remaining"] != "0" {
		t.Error("expected remaining=0")
	}
	if resp.Headers["Retry-After"] == "" {
		t.Error("expected Retry-After header")
	}
	if len(resp.Body) == 0 {
		t.Error("expected non-empty body")
	}
}

func TestShadowBreachHeaders(t *testing.T) {
	h := ShadowBreachHeaders("my-rule")
	if h["X-RateLimit-Shadow-Breach"] != "my-rule" {
		t.Errorf("expected my-rule, got %s", h["X-RateLimit-Shadow-Breach"])
	}
}

func TestMergeHeaders(t *testing.T) {
	dst := map[string]string{"a": "1"}
	src := map[string]string{"b": "2", "c": "3"}
	MergeHeaders(dst, src)
	if len(dst) != 3 {
		t.Errorf("expected 3 entries, got %d", len(dst))
	}
	if dst["b"] != "2" || dst["c"] != "3" {
		t.Error("merge failed")
	}
}
