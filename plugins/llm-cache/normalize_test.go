package main

import (
	"encoding/json"
	"testing"
)

func TestGenerateCacheKey(t *testing.T) {
	request := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Hello, world!",
			},
		},
		"temperature": 0.7,
	}
	requestBody, _ := json.Marshal(request)

	key, model, err := GenerateCacheKey(CacheScope{Namespace: "test-namespace"}, requestBody, true)

	if err != nil {
		t.Fatalf("GenerateCacheKey failed: %v", err)
	}

	if key == "" {
		t.Error("expected non-empty cache key")
	}

	if model != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", model)
	}

	// Key should start with expected prefix
	expectedPrefix := "cache:resp:test-namespace:"
	if len(key) < len(expectedPrefix) || key[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("key should start with '%s', got '%s'", expectedPrefix, key)
	}
}

func TestGenerateCacheKeyDeterministic(t *testing.T) {
	request := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Test message",
			},
		},
	}
	requestBody, _ := json.Marshal(request)

	key1, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, requestBody, true)
	key2, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, requestBody, true)

	if key1 != key2 {
		t.Error("same request should generate same cache key")
	}
}

func TestGenerateCacheKeyDifferentNamespaces(t *testing.T) {
	request := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Test message",
			},
		},
	}
	requestBody, _ := json.Marshal(request)

	key1, _, _ := GenerateCacheKey(CacheScope{Namespace: "namespace1"}, requestBody, true)
	key2, _, _ := GenerateCacheKey(CacheScope{Namespace: "namespace2"}, requestBody, true)

	if key1 == key2 {
		t.Error("different namespaces should generate different cache keys")
	}
}

func TestGenerateCacheKeyNormalization(t *testing.T) {
	// Request with extra whitespace
	request1 := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Hello   world!",
			},
		},
	}

	// Same request without extra whitespace
	request2 := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Hello world!",
			},
		},
	}

	body1, _ := json.Marshal(request1)
	body2, _ := json.Marshal(request2)

	// With normalization, these should produce the same key
	key1, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, body1, true)
	key2, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, body2, true)

	if key1 != key2 {
		t.Error("normalized prompts with different whitespace should generate same cache key")
	}

	// Without normalization, they should differ
	key3, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, body1, false)
	key4, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, body2, false)

	if key3 == key4 {
		t.Error("non-normalized prompts with different whitespace should generate different cache keys")
	}
}

func TestGenerateCacheKeyInvalidJSON(t *testing.T) {
	invalidBody := []byte(`{invalid json}`)

	_, _, err := GenerateCacheKey(CacheScope{Namespace: "ns"}, invalidBody, true)

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGenerateCacheKeyWithSystemPrompt(t *testing.T) {
	request := map[string]interface{}{
		"model":  "claude-3-opus",
		"system": "You are a helpful assistant.",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Hello",
			},
		},
	}
	requestBody, _ := json.Marshal(request)

	key, model, err := GenerateCacheKey(CacheScope{Namespace: "ns"}, requestBody, true)

	if err != nil {
		t.Fatalf("GenerateCacheKey failed: %v", err)
	}

	if key == "" {
		t.Error("expected non-empty cache key")
	}

	if model != "claude-3-opus" {
		t.Errorf("expected model claude-3-opus, got %s", model)
	}
}

func TestGenerateCacheKeyWithTools(t *testing.T) {
	request := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "What's the weather?",
			},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "get_weather",
					"description": "Get the weather",
				},
			},
		},
	}
	requestBody, _ := json.Marshal(request)

	key, _, err := GenerateCacheKey(CacheScope{Namespace: "ns"}, requestBody, true)

	if err != nil {
		t.Fatalf("GenerateCacheKey failed: %v", err)
	}

	if key == "" {
		t.Error("expected non-empty cache key")
	}

	// Without tools should produce different key
	request2 := map[string]interface{}{
		"model": "gpt-4",
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "What's the weather?",
			},
		},
	}
	body2, _ := json.Marshal(request2)
	key2, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, body2, true)

	if key == key2 {
		t.Error("request with tools should have different key than without tools")
	}
}

func TestGenerateCacheKeyDifferentTemperatures(t *testing.T) {
	request1 := map[string]interface{}{
		"model":       "gpt-4",
		"messages":    []interface{}{map[string]interface{}{"role": "user", "content": "Hi"}},
		"temperature": 0.5,
	}

	request2 := map[string]interface{}{
		"model":       "gpt-4",
		"messages":    []interface{}{map[string]interface{}{"role": "user", "content": "Hi"}},
		"temperature": 0.9,
	}

	body1, _ := json.Marshal(request1)
	body2, _ := json.Marshal(request2)

	key1, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, body1, true)
	key2, _, _ := GenerateCacheKey(CacheScope{Namespace: "ns"}, body2, true)

	if key1 == key2 {
		t.Error("different temperatures should generate different cache keys")
	}
}

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello world  ", "hello world"},
		{"hello   world", "hello world"},
		{"hello\n\nworld", "hello world"},
		{"hello\t\tworld", "hello world"},
		{"  hello   \n   world  ", "hello world"},
		{"simple", "simple"},
		{"", ""},
	}

	for _, tt := range tests {
		result := normalizeText(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeText(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestExtractTokensFromResponse(t *testing.T) {
	// OpenAI-style response
	openaiResponse := map[string]interface{}{
		"usage": map[string]interface{}{
			"prompt_tokens":     100.0,
			"completion_tokens": 50.0,
			"total_tokens":      150.0,
		},
	}
	body, _ := json.Marshal(openaiResponse)
	tokens := ExtractTokensFromResponse(body)
	if tokens != 150 {
		t.Errorf("expected 150 tokens (OpenAI total), got %d", tokens)
	}

	// Anthropic-style response
	anthropicResponse := map[string]interface{}{
		"usage": map[string]interface{}{
			"input_tokens":  80.0,
			"output_tokens": 40.0,
		},
	}
	body, _ = json.Marshal(anthropicResponse)
	tokens = ExtractTokensFromResponse(body)
	if tokens != 120 {
		t.Errorf("expected 120 tokens (Anthropic), got %d", tokens)
	}

	// No usage field
	noUsageResponse := map[string]interface{}{
		"response": "Hello",
	}
	body, _ = json.Marshal(noUsageResponse)
	tokens = ExtractTokensFromResponse(body)
	if tokens != 0 {
		t.Errorf("expected 0 tokens when no usage field, got %d", tokens)
	}

	// Invalid JSON
	tokens = ExtractTokensFromResponse([]byte(`{invalid}`))
	if tokens != 0 {
		t.Errorf("expected 0 tokens for invalid JSON, got %d", tokens)
	}
}

func keyOf(t *testing.T, scope CacheScope, body string) string {
	t.Helper()
	key, _, err := GenerateCacheKey(scope, []byte(body), true)
	if err != nil {
		t.Fatalf("GenerateCacheKey(%s): %v", body, err)
	}
	return key
}

func TestGenerateCacheKeyScope(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	base := CacheScope{Namespace: "ns", LLMID: 1, Vendor: "openai", Path: "/v1/chat/completions"}
	variants := map[string]CacheScope{
		"llm":    {Namespace: "ns", LLMID: 2, Vendor: "openai", Path: "/v1/chat/completions"},
		"vendor": {Namespace: "ns", LLMID: 1, Vendor: "anthropic", Path: "/v1/chat/completions"},
		"path":   {Namespace: "ns", LLMID: 1, Vendor: "openai", Path: "/v1/completions"},
	}
	for name, scope := range variants {
		if keyOf(t, base, body) == keyOf(t, scope, body) {
			t.Errorf("a different %s gave the same key", name)
		}
	}
}

func TestGenerateCacheKeyIgnoresTransportFields(t *testing.T) {
	scope := CacheScope{Namespace: "ns"}
	plain := keyOf(t, scope, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	streamed := keyOf(t, scope, `{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true,"stream_options":{"include_usage":true}}`)
	if plain != streamed {
		t.Error("stream and stream_options should not change the key")
	}
}

func TestGenerateCacheKeyToolOrderAndShape(t *testing.T) {
	scope := CacheScope{Namespace: "ns"}
	openAIAB := keyOf(t, scope, `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"a"}},{"type":"function","function":{"name":"b"}}]}`)
	openAIBA := keyOf(t, scope, `{"model":"m","messages":[],"tools":[{"type":"function","function":{"name":"b"}},{"type":"function","function":{"name":"a"}}]}`)
	if openAIAB != openAIBA {
		t.Error("tool order should not change the key")
	}
	// Anthropic tools have no "function" object; two different ones must
	// not look alike.
	anthA := keyOf(t, scope, `{"model":"m","messages":[],"tools":[{"name":"a","input_schema":{"type":"object"}}]}`)
	anthB := keyOf(t, scope, `{"model":"m","messages":[],"tools":[{"name":"b","input_schema":{"type":"object"}}]}`)
	if anthA == anthB {
		t.Error("different Anthropic tools gave the same key")
	}
}

func TestGenerateCacheKeyToolResultsMatter(t *testing.T) {
	scope := CacheScope{Namespace: "ns"}
	conv := func(result string) string {
		return `{"model":"m","messages":[{"role":"user","content":"weather?"},` +
			`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"w","input":{}}]},` +
			`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"` + result + `"}]}]}`
	}
	if keyOf(t, scope, conv("sunny")) == keyOf(t, scope, conv("rainy")) {
		t.Error("different tool results gave the same key")
	}
}

func TestGenerateCacheKeyGeminiModelFromPath(t *testing.T) {
	_, model, err := GenerateCacheKey(CacheScope{Namespace: "ns", Path: "/v1beta/models/gemini-a:generateContent"}, []byte(`{"contents":[]}`), true)
	if err != nil || model != "gemini-a" {
		t.Fatalf("model = %q, %v; want gemini-a", model, err)
	}
}

func TestAPIPath(t *testing.T) {
	cases := map[string]string{
		"/llm/call/claude/v1/messages":                                "/v1/messages",
		"/llm/rest/gem/v1beta/models/g:streamGenerateContent":         "/v1beta/models/g:generateContent",
		"/llm/call/gem/v1beta/models/g:streamGenerateContent?alt=sse": "/v1beta/models/g:generateContent",
		"/v1/chat/completions":                                        "/v1/chat/completions",
	}
	for in, want := range cases {
		if got := apiPath(in); got != want {
			t.Errorf("apiPath(%q) = %q, want %q", in, got, want)
		}
	}
}
