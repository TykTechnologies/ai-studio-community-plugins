package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Sample OpenAI SSE stream for testing
const openAISSE = `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}

data: [DONE]
`

// Sample Anthropic SSE stream for testing
const anthropicSSE = `event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","model":"claude-3-opus-20240229","usage":{"input_tokens":25,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"!"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}

event: message_stop
data: {"type":"message_stop"}
`

// Sample Google/Gemini SSE stream for testing
const googleSSE = `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"}}]}

data: {"candidates":[{"content":{"parts":[{"text":" world"}],"role":"model"}}]}

data: {"candidates":[{"content":{"parts":[{"text":"!"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"totalTokenCount":13}}
`

func TestReconstructOpenAIResponse(t *testing.T) {
	result, tokens, err := ReconstructResponseFromSSE([]byte(openAISSE), "openai")

	if err != nil {
		t.Fatalf("ReconstructResponseFromSSE failed: %v", err)
	}

	if tokens != 13 {
		t.Errorf("expected 13 tokens, got %d", tokens)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check response ID
	if id, ok := response["id"].(string); !ok || id != "chatcmpl-123" {
		t.Errorf("expected id 'chatcmpl-123', got '%v'", response["id"])
	}

	// Check content was reconstructed
	if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					if content != "Hello world!" {
						t.Errorf("expected content 'Hello world!', got '%s'", content)
					}
				} else {
					t.Error("content not found in message")
				}
			} else {
				t.Error("message not found in choice")
			}
		}
	} else {
		t.Error("choices not found in response")
	}
}

func TestReconstructAnthropicResponse(t *testing.T) {
	result, tokens, err := ReconstructResponseFromSSE([]byte(anthropicSSE), "anthropic")

	if err != nil {
		t.Fatalf("ReconstructResponseFromSSE failed: %v", err)
	}

	if tokens != 28 { // 25 input + 3 output
		t.Errorf("expected 28 tokens, got %d", tokens)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check response ID
	if id, ok := response["id"].(string); !ok || id != "msg_123" {
		t.Errorf("expected id 'msg_123', got '%v'", response["id"])
	}

	// Check content was reconstructed
	if content, ok := response["content"].([]interface{}); ok && len(content) > 0 {
		if block, ok := content[0].(map[string]interface{}); ok {
			if text, ok := block["text"].(string); ok {
				if text != "Hello world!" {
					t.Errorf("expected content 'Hello world!', got '%s'", text)
				}
			} else {
				t.Error("text not found in content block")
			}
		}
	} else {
		t.Error("content not found in response")
	}
}

func TestReconstructGoogleResponse(t *testing.T) {
	result, tokens, err := ReconstructResponseFromSSE([]byte(googleSSE), "google")

	if err != nil {
		t.Fatalf("ReconstructResponseFromSSE failed: %v", err)
	}

	if tokens != 13 {
		t.Errorf("expected 13 tokens, got %d", tokens)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Check content was reconstructed
	if candidates, ok := response["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if candidate, ok := candidates[0].(map[string]interface{}); ok {
			if content, ok := candidate["content"].(map[string]interface{}); ok {
				if parts, ok := content["parts"].([]interface{}); ok && len(parts) > 0 {
					if part, ok := parts[0].(map[string]interface{}); ok {
						if text, ok := part["text"].(string); ok {
							if text != "Hello world!" {
								t.Errorf("expected content 'Hello world!', got '%s'", text)
							}
						}
					}
				}
			}
		}
	} else {
		t.Error("candidates not found in response")
	}
}

func TestConvertOpenAIToSSE(t *testing.T) {
	jsonResponse := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello world!"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"total_tokens": 13
		}
	}`

	result, err := ConvertJSONToSSE([]byte(jsonResponse), "openai")

	if err != nil {
		t.Fatalf("ConvertJSONToSSE failed: %v", err)
	}

	// Check that it contains SSE format markers
	resultStr := string(result)
	if !strings.Contains(resultStr, "data: ") {
		t.Error("result should contain 'data: ' prefix")
	}
	if !strings.Contains(resultStr, "[DONE]") {
		t.Error("result should contain [DONE] marker")
	}
	if !strings.Contains(resultStr, "Hello") {
		t.Error("result should contain the content")
	}
}

func TestConvertAnthropicToSSE(t *testing.T) {
	jsonResponse := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-opus-20240229",
		"content": [{
			"type": "text",
			"text": "Hello world!"
		}],
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 25,
			"output_tokens": 3
		}
	}`

	result, err := ConvertJSONToSSE([]byte(jsonResponse), "anthropic")

	if err != nil {
		t.Fatalf("ConvertJSONToSSE failed: %v", err)
	}

	resultStr := string(result)

	// Check that it contains Anthropic SSE format markers
	if !strings.Contains(resultStr, "event: message_start") {
		t.Error("result should contain 'event: message_start'")
	}
	if !strings.Contains(resultStr, "event: content_block_delta") {
		t.Error("result should contain 'event: content_block_delta'")
	}
	if !strings.Contains(resultStr, "event: message_stop") {
		t.Error("result should contain 'event: message_stop'")
	}
	if !strings.Contains(resultStr, "Hello") {
		t.Error("result should contain the content")
	}
}

func TestConvertGoogleToSSE(t *testing.T) {
	jsonResponse := `{
		"candidates": [{
			"content": {
				"parts": [{"text": "Hello world!"}],
				"role": "model"
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"totalTokenCount": 13
		}
	}`

	result, err := ConvertJSONToSSE([]byte(jsonResponse), "google")

	if err != nil {
		t.Fatalf("ConvertJSONToSSE failed: %v", err)
	}

	resultStr := string(result)

	// Check that it contains SSE format
	if !strings.Contains(resultStr, "data: ") {
		t.Error("result should contain 'data: ' prefix")
	}
	if !strings.Contains(resultStr, "Hello") {
		t.Error("result should contain the content")
	}
	if !strings.Contains(resultStr, "STOP") {
		t.Error("result should contain finish reason")
	}
}

func TestIsStreamingRequest(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{
			name:     "streaming enabled",
			body:     `{"model": "gpt-4", "messages": [{"role": "user", "content": "Hi"}], "stream": true}`,
			expected: true,
		},
		{
			name:     "streaming disabled",
			body:     `{"model": "gpt-4", "messages": [{"role": "user", "content": "Hi"}], "stream": false}`,
			expected: false,
		},
		{
			name:     "streaming not specified",
			body:     `{"model": "gpt-4", "messages": [{"role": "user", "content": "Hi"}]}`,
			expected: false,
		},
		{
			name:     "invalid json",
			body:     `{invalid}`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsStreamingRequest([]byte(tt.body))
			if result != tt.expected {
				t.Errorf("IsStreamingRequest(%s) = %v, expected %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestRoundTripOpenAI(t *testing.T) {
	// Test that SSE -> JSON -> SSE produces valid output
	reconstructedJSON, _, err := ReconstructResponseFromSSE([]byte(openAISSE), "openai")
	if err != nil {
		t.Fatalf("ReconstructResponseFromSSE failed: %v", err)
	}

	sseOutput, err := ConvertJSONToSSE(reconstructedJSON, "openai")
	if err != nil {
		t.Fatalf("ConvertJSONToSSE failed: %v", err)
	}

	// The SSE output should be valid and contain the content
	if !strings.Contains(string(sseOutput), "Hello") {
		t.Error("round trip should preserve content")
	}
	if !strings.Contains(string(sseOutput), "[DONE]") {
		t.Error("round trip should end with [DONE]")
	}
}

func TestRoundTripAnthropic(t *testing.T) {
	// Test that SSE -> JSON -> SSE produces valid output
	reconstructedJSON, _, err := ReconstructResponseFromSSE([]byte(anthropicSSE), "anthropic")
	if err != nil {
		t.Fatalf("ReconstructResponseFromSSE failed: %v", err)
	}

	sseOutput, err := ConvertJSONToSSE(reconstructedJSON, "anthropic")
	if err != nil {
		t.Fatalf("ConvertJSONToSSE failed: %v", err)
	}

	// The SSE output should be valid and contain the content
	if !strings.Contains(string(sseOutput), "Hello") {
		t.Error("round trip should preserve content")
	}
	if !strings.Contains(string(sseOutput), "message_stop") {
		t.Error("round trip should end with message_stop")
	}
}

func TestVendorDetection(t *testing.T) {
	tests := []struct {
		vendor   string
		expected string // The vendor detection should handle various vendor strings
	}{
		{"openai", "openai"},
		{"OpenAI", "openai"},
		{"anthropic", "anthropic"},
		{"Anthropic", "anthropic"},
		{"google", "google"},
		{"gemini", "google"},
		{"vertex", "google"},
		{"ollama", "openai"}, // Ollama uses OpenAI format
		{"unknown", "openai"},
		{"", "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.vendor, func(t *testing.T) {
			// All vendors should be able to reconstruct the OpenAI format as a baseline
			_, _, err := ReconstructResponseFromSSE([]byte(openAISSE), tt.vendor)
			if err != nil {
				t.Errorf("vendor %s should be able to handle OpenAI format: %v", tt.vendor, err)
			}
		})
	}
}

func TestEmptySSE(t *testing.T) {
	_, _, err := ReconstructResponseFromSSE([]byte(""), "openai")
	if err == nil {
		t.Error("expected error for empty SSE data")
	}

	_, _, err = ReconstructResponseFromSSE([]byte("data: invalid\n\n"), "openai")
	if err == nil {
		t.Error("expected error for SSE with no valid JSON chunks")
	}
}
