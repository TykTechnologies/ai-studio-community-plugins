package main

import (
	"testing"
)

func TestExtractTokenUsage_OpenAI(t *testing.T) {
	body := []byte(`{
		"id": "chatcmpl-abc",
		"choices": [{"message": {"content": "Hello"}}],
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 50,
			"total_tokens": 150
		}
	}`)
	u := ExtractTokenUsage(body)
	if u.PromptTokens != 100 {
		t.Errorf("expected prompt=100, got %d", u.PromptTokens)
	}
	if u.CompletionTokens != 50 {
		t.Errorf("expected completion=50, got %d", u.CompletionTokens)
	}
	if u.TotalTokens != 150 {
		t.Errorf("expected total=150, got %d", u.TotalTokens)
	}
}

func TestExtractTokenUsage_Anthropic(t *testing.T) {
	body := []byte(`{
		"id": "msg_abc",
		"type": "message",
		"content": [{"type": "text", "text": "Hello"}],
		"usage": {
			"input_tokens": 200,
			"output_tokens": 80
		}
	}`)
	u := ExtractTokenUsage(body)
	if u.PromptTokens != 200 {
		t.Errorf("expected prompt=200, got %d", u.PromptTokens)
	}
	if u.CompletionTokens != 80 {
		t.Errorf("expected completion=80, got %d", u.CompletionTokens)
	}
	if u.TotalTokens != 280 {
		t.Errorf("expected total=280, got %d", u.TotalTokens)
	}
}

func TestExtractTokenUsage_AnthropicWithCache(t *testing.T) {
	body := []byte(`{
		"usage": {
			"input_tokens": 150,
			"output_tokens": 60,
			"cache_creation_input_tokens": 100,
			"cache_read_input_tokens": 50
		}
	}`)
	u := ExtractTokenUsage(body)
	if u.CacheWriteTokens != 100 {
		t.Errorf("expected cache_write=100, got %d", u.CacheWriteTokens)
	}
	if u.CacheReadTokens != 50 {
		t.Errorf("expected cache_read=50, got %d", u.CacheReadTokens)
	}
	if u.TotalTokens != 210 {
		t.Errorf("expected total=210, got %d", u.TotalTokens)
	}
}

func TestExtractTokenUsage_EmptyBody(t *testing.T) {
	u := ExtractTokenUsage(nil)
	if u.TotalTokens != 0 {
		t.Errorf("expected 0, got %d", u.TotalTokens)
	}
	u = ExtractTokenUsage([]byte{})
	if u.TotalTokens != 0 {
		t.Errorf("expected 0, got %d", u.TotalTokens)
	}
}

func TestExtractTokenUsage_InvalidJSON(t *testing.T) {
	u := ExtractTokenUsage([]byte("not json"))
	if u.TotalTokens != 0 {
		t.Errorf("expected 0, got %d", u.TotalTokens)
	}
}

func TestExtractTokenUsage_NoUsage(t *testing.T) {
	body := []byte(`{"id": "test", "content": "hello"}`)
	u := ExtractTokenUsage(body)
	if u.TotalTokens != 0 {
		t.Errorf("expected 0, got %d", u.TotalTokens)
	}
}

func TestExtractTokenUsage_TotalFallback(t *testing.T) {
	// No total_tokens field — should compute from prompt + completion
	body := []byte(`{"usage": {"prompt_tokens": 30, "completion_tokens": 70}}`)
	u := ExtractTokenUsage(body)
	if u.TotalTokens != 100 {
		t.Errorf("expected computed total=100, got %d", u.TotalTokens)
	}
}
