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

	key, model, err := GenerateCacheKey("test-namespace", requestBody, true)

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

	key1, _, _ := GenerateCacheKey("ns", requestBody, true)
	key2, _, _ := GenerateCacheKey("ns", requestBody, true)

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

	key1, _, _ := GenerateCacheKey("namespace1", requestBody, true)
	key2, _, _ := GenerateCacheKey("namespace2", requestBody, true)

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
	key1, _, _ := GenerateCacheKey("ns", body1, true)
	key2, _, _ := GenerateCacheKey("ns", body2, true)

	if key1 != key2 {
		t.Error("normalized prompts with different whitespace should generate same cache key")
	}

	// Without normalization, they should differ
	key3, _, _ := GenerateCacheKey("ns", body1, false)
	key4, _, _ := GenerateCacheKey("ns", body2, false)

	if key3 == key4 {
		t.Error("non-normalized prompts with different whitespace should generate different cache keys")
	}
}

func TestGenerateCacheKeyInvalidJSON(t *testing.T) {
	invalidBody := []byte(`{invalid json}`)

	_, _, err := GenerateCacheKey("ns", invalidBody, true)

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

	key, model, err := GenerateCacheKey("ns", requestBody, true)

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

	key, _, err := GenerateCacheKey("ns", requestBody, true)

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
	key2, _, _ := GenerateCacheKey("ns", body2, true)

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

	key1, _, _ := GenerateCacheKey("ns", body1, true)
	key2, _, _ := GenerateCacheKey("ns", body2, true)

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

func TestNormalizeMessage(t *testing.T) {
	// Simple string content
	msg := map[string]interface{}{
		"role":    "user",
		"content": "  Hello   world  ",
	}
	normalized := normalizeMessage(msg, true)
	if normalized.Role != "user" {
		t.Errorf("expected role 'user', got '%s'", normalized.Role)
	}
	if normalized.Content != "Hello world" {
		t.Errorf("expected normalized content 'Hello world', got '%s'", normalized.Content)
	}

	// Without normalization
	normalized = normalizeMessage(msg, false)
	if normalized.Content != "  Hello   world  " {
		t.Errorf("without normalization, content should be unchanged")
	}
}

func TestNormalizeMessageWithArrayContent(t *testing.T) {
	// Array content (like Anthropic vision messages)
	msg := map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "What's in this image?",
			},
		},
	}

	normalized := normalizeMessage(msg, true)
	if normalized.Content != "What's in this image?" {
		t.Errorf("expected extracted text content, got '%s'", normalized.Content)
	}
}

func TestNormalizeTools(t *testing.T) {
	tools := []interface{}{
		map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "tool_b",
				"description": "Second tool",
			},
		},
		map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "tool_a",
				"description": "First tool",
			},
		},
	}

	normalized := normalizeTools(tools)

	if len(normalized) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(normalized))
	}

	// Should be sorted by name
	if getToolName(normalized[0]) != "tool_a" {
		t.Error("tools should be sorted by name, expected tool_a first")
	}
	if getToolName(normalized[1]) != "tool_b" {
		t.Error("tools should be sorted by name, expected tool_b second")
	}
}

func TestGetToolName(t *testing.T) {
	tool := NormalizedTool{
		Type: "function",
		Function: map[string]interface{}{
			"name": "my_tool",
		},
	}

	name := getToolName(tool)
	if name != "my_tool" {
		t.Errorf("expected 'my_tool', got '%s'", name)
	}

	// Tool without function
	emptyTool := NormalizedTool{Type: "function"}
	name = getToolName(emptyTool)
	if name != "" {
		t.Errorf("expected empty string for tool without function, got '%s'", name)
	}
}

func TestCanonicalizeJSON(t *testing.T) {
	// Test with nested map
	input := map[string]interface{}{
		"b": "value_b",
		"a": "value_a",
		"nested": map[string]interface{}{
			"z": "value_z",
			"y": "value_y",
		},
	}

	result := canonicalizeJSON(input)

	// Result should be a map
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected map result")
	}

	// Nested should also be canonicalized
	nested, ok := resultMap["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("nested should be a map")
	}

	if nested["y"] != "value_y" {
		t.Error("nested values should be preserved")
	}
}

func TestCanonicalizeJSONArray(t *testing.T) {
	input := []interface{}{
		map[string]interface{}{"name": "item1"},
		map[string]interface{}{"name": "item2"},
	}

	result := canonicalizeJSON(input)

	resultArray, ok := result.([]interface{})
	if !ok {
		t.Fatal("expected array result")
	}

	if len(resultArray) != 2 {
		t.Errorf("expected 2 items, got %d", len(resultArray))
	}
}

func TestHashComponentsConsistency(t *testing.T) {
	components := CacheKeyComponents{
		Namespace: "test",
		Model:     "gpt-4",
		Messages: []NormalizedMessage{
			{Role: "user", Content: "Hello"},
		},
		SystemPrompt: "Be helpful",
		Temperature:  0.7,
	}

	hash1, err := hashComponents(components)
	if err != nil {
		t.Fatalf("hashComponents failed: %v", err)
	}

	hash2, err := hashComponents(components)
	if err != nil {
		t.Fatalf("hashComponents failed: %v", err)
	}

	if hash1 != hash2 {
		t.Error("same components should produce same hash")
	}
}

func TestHashComponentsDifferent(t *testing.T) {
	components1 := CacheKeyComponents{
		Namespace: "test",
		Model:     "gpt-4",
		Messages: []NormalizedMessage{
			{Role: "user", Content: "Hello"},
		},
	}

	components2 := CacheKeyComponents{
		Namespace: "test",
		Model:     "gpt-4",
		Messages: []NormalizedMessage{
			{Role: "user", Content: "Goodbye"},
		},
	}

	hash1, _ := hashComponents(components1)
	hash2, _ := hashComponents(components2)

	if hash1 == hash2 {
		t.Error("different components should produce different hashes")
	}
}
