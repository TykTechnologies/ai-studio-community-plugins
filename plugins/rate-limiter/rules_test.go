package main

import (
	"testing"

	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

func TestResolveKey_SingleDimension(t *testing.T) {
	ctx := &pb.PluginContext{AppId: 42}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"app_id"}}

	key, ok := ResolveKey(rule, ctx, req)
	if !ok {
		t.Fatal("expected ok")
	}
	if key != "app_id:42" {
		t.Errorf("expected app_id:42, got %s", key)
	}
}

func TestResolveKey_CompositeDimensions(t *testing.T) {
	ctx := &pb.PluginContext{AppId: 42, LlmSlug: "gpt-4o"}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"model", "app_id"}}

	key, ok := ResolveKey(rule, ctx, req)
	if !ok {
		t.Fatal("expected ok")
	}
	// Sorted: app_id comes before model
	if key != "app_id:42|model:gpt-4o" {
		t.Errorf("expected app_id:42|model:gpt-4o, got %s", key)
	}
}

func TestResolveKey_Global(t *testing.T) {
	ctx := &pb.PluginContext{}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"global"}}

	key, ok := ResolveKey(rule, ctx, req)
	if !ok {
		t.Fatal("expected ok for global")
	}
	if key != "global:_" {
		t.Errorf("expected global:_, got %s", key)
	}
}

func TestResolveKey_MissingDimension_AppID(t *testing.T) {
	ctx := &pb.PluginContext{AppId: 0}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"app_id"}}

	_, ok := ResolveKey(rule, ctx, req)
	if ok {
		t.Error("expected skip when app_id is 0")
	}
}

func TestResolveKey_MissingDimension_UserID(t *testing.T) {
	ctx := &pb.PluginContext{UserId: 0}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"user_id"}}

	_, ok := ResolveKey(rule, ctx, req)
	if ok {
		t.Error("expected skip when user_id is 0")
	}
}

func TestResolveKey_MissingDimension_Model(t *testing.T) {
	ctx := &pb.PluginContext{LlmSlug: ""}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"model"}}

	_, ok := ResolveKey(rule, ctx, req)
	if ok {
		t.Error("expected skip when model is empty")
	}
}

func TestResolveKey_MissingDimension_SkipsPartial(t *testing.T) {
	// app_id present but user_id missing → whole rule skipped
	ctx := &pb.PluginContext{AppId: 42, UserId: 0}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"app_id", "user_id"}}

	_, ok := ResolveKey(rule, ctx, req)
	if ok {
		t.Error("expected skip when any dimension is missing")
	}
}

func TestResolveKey_ApiKey_FromClaims(t *testing.T) {
	ctx := &pb.PluginContext{}
	req := &pb.EnrichedRequest{
		AuthClaims: map[string]string{"api_key_id": "key-abc"},
	}
	rule := Rule{Dimensions: []string{"api_key"}}

	key, ok := ResolveKey(rule, ctx, req)
	if !ok {
		t.Fatal("expected ok")
	}
	if key != "api_key:key-abc" {
		t.Errorf("expected api_key:key-abc, got %s", key)
	}
}

func TestResolveKey_ApiKey_FromAuthHeader(t *testing.T) {
	ctx := &pb.PluginContext{}
	req := &pb.EnrichedRequest{
		Request: &pb.PluginRequest{
			Headers: map[string]string{"Authorization": "Bearer sk-test-123"},
		},
	}
	rule := Rule{Dimensions: []string{"api_key"}}

	key, ok := ResolveKey(rule, ctx, req)
	if !ok {
		t.Fatal("expected ok")
	}
	if key == "" {
		t.Error("expected non-empty hashed key")
	}
}

func TestResolveKey_ApiKey_Missing(t *testing.T) {
	ctx := &pb.PluginContext{}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"api_key"}}

	_, ok := ResolveKey(rule, ctx, req)
	if ok {
		t.Error("expected skip when no api_key available")
	}
}

func TestResolveKey_LlmID(t *testing.T) {
	ctx := &pb.PluginContext{LlmId: 7}
	req := &pb.EnrichedRequest{}
	rule := Rule{Dimensions: []string{"llm_id"}}

	key, ok := ResolveKey(rule, ctx, req)
	if !ok {
		t.Fatal("expected ok")
	}
	if key != "llm_id:7" {
		t.Errorf("expected llm_id:7, got %s", key)
	}
}

func TestSortedRules(t *testing.T) {
	rules := []Rule{
		{Name: "c", Priority: 3},
		{Name: "a", Priority: 1},
		{Name: "b", Priority: 2},
	}
	sorted := SortedRules(rules)
	if sorted[0].Name != "a" || sorted[1].Name != "b" || sorted[2].Name != "c" {
		t.Errorf("expected sorted by priority, got %v", sorted)
	}
}

func TestValidateRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    Rule
		wantErr bool
	}{
		{
			name:    "valid rule",
			rule:    Rule{Name: "test", Dimensions: []string{"app_id"}, Limit: Limit{Type: "requests", Value: 100}},
			wantErr: false,
		},
		{
			name:    "empty name",
			rule:    Rule{Dimensions: []string{"app_id"}, Limit: Limit{Type: "requests", Value: 100}},
			wantErr: true,
		},
		{
			name:    "no dimensions",
			rule:    Rule{Name: "test", Limit: Limit{Type: "requests", Value: 100}},
			wantErr: true,
		},
		{
			name:    "invalid dimension",
			rule:    Rule{Name: "test", Dimensions: []string{"invalid"}, Limit: Limit{Type: "requests", Value: 100}},
			wantErr: true,
		},
		{
			name:    "invalid limit type",
			rule:    Rule{Name: "test", Dimensions: []string{"app_id"}, Limit: Limit{Type: "invalid", Value: 100}},
			wantErr: true,
		},
		{
			name:    "zero value",
			rule:    Rule{Name: "test", Dimensions: []string{"app_id"}, Limit: Limit{Type: "requests", Value: 0}},
			wantErr: true,
		},
		{
			name:    "invalid action",
			rule:    Rule{Name: "test", Dimensions: []string{"app_id"}, Limit: Limit{Type: "requests", Value: 100}, Action: "invalid"},
			wantErr: true,
		},
		{
			name:    "log action valid",
			rule:    Rule{Name: "test", Dimensions: []string{"app_id"}, Limit: Limit{Type: "tokens", Value: 50000}, Action: "log"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRule(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRule() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWindowKey(t *testing.T) {
	key := WindowKey("rule1", "app_id:42|model:gpt-4o", 1711900800)
	expected := "rl:w:rule1:app_id:42|model:gpt-4o:1711900800"
	if key != expected {
		t.Errorf("expected %s, got %s", expected, key)
	}
}

func TestConcurrentKey(t *testing.T) {
	key := ConcurrentKey("rule1", "app_id:42")
	expected := "rl:c:rule1:app_id:42"
	if key != expected {
		t.Errorf("expected %s, got %s", expected, key)
	}
}

func TestRequestStateKey(t *testing.T) {
	key := RequestStateKey("req-abc-123")
	expected := "rl:req:req-abc-123"
	if key != expected {
		t.Errorf("expected %s, got %s", expected, key)
	}
}
