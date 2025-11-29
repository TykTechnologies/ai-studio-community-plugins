package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// SSE Event Types
const (
	// OpenAI
	SSEDataPrefix = "data: "
	SSEDone       = "[DONE]"

	// Anthropic event types
	AnthropicMessageStart       = "message_start"
	AnthropicContentBlockStart  = "content_block_start"
	AnthropicContentBlockDelta  = "content_block_delta"
	AnthropicContentBlockStop   = "content_block_stop"
	AnthropicMessageDelta       = "message_delta"
	AnthropicMessageStop        = "message_stop"
)

// ReconstructResponseFromSSE converts an SSE stream back into a complete JSON response.
// This is used to cache streaming responses in a format that can be returned as either
// JSON (for REST requests) or re-converted to SSE (for streaming requests).
func ReconstructResponseFromSSE(sseData []byte, vendor string) ([]byte, int, error) {
	vendor = strings.ToLower(vendor)

	switch {
	case strings.Contains(vendor, "anthropic"):
		return reconstructAnthropicResponse(sseData)
	case strings.Contains(vendor, "google"), strings.Contains(vendor, "gemini"), strings.Contains(vendor, "vertex"):
		return reconstructGoogleResponse(sseData)
	default:
		// Default to OpenAI format (also used by Ollama, etc.)
		return reconstructOpenAIResponse(sseData)
	}
}

// reconstructOpenAIResponse reconstructs an OpenAI-format response from SSE chunks.
// OpenAI SSE format:
//
//	data: {"id":"chatcmpl-xxx","choices":[{"delta":{"content":"Hello"}}]}
//	data: {"id":"chatcmpl-xxx","choices":[{"delta":{"content":" world"}}]}
//	data: {"id":"chatcmpl-xxx","choices":[{"finish_reason":"stop"}],"usage":{"total_tokens":10}}
//	data: [DONE]
func reconstructOpenAIResponse(sseData []byte) ([]byte, int, error) {
	var contentBuilder strings.Builder
	var lastChunk map[string]interface{}
	var tokenUsage int
	var responseID string
	var model string
	var finishReason string
	var toolCalls []interface{}

	scanner := bufio.NewScanner(bytes.NewReader(sseData))
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and non-data lines
		if !strings.HasPrefix(line, SSEDataPrefix) {
			continue
		}

		data := strings.TrimPrefix(line, SSEDataPrefix)
		data = strings.TrimSpace(data)

		// Skip [DONE] marker
		if data == SSEDone {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // Skip malformed chunks
		}

		lastChunk = chunk

		// Extract response ID
		if id, ok := chunk["id"].(string); ok && responseID == "" {
			responseID = id
		}

		// Extract model
		if m, ok := chunk["model"].(string); ok && model == "" {
			model = m
		}

		// Extract content from choices
		if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				// Extract delta content
				if delta, ok := choice["delta"].(map[string]interface{}); ok {
					if content, ok := delta["content"].(string); ok {
						contentBuilder.WriteString(content)
					}
					// Extract tool calls from delta
					if tc, ok := delta["tool_calls"].([]interface{}); ok {
						toolCalls = append(toolCalls, tc...)
					}
				}
				// Extract finish reason
				if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
					finishReason = fr
				}
			}
		}

		// Extract token usage (usually in the last chunk)
		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			if total, ok := usage["total_tokens"].(float64); ok {
				tokenUsage = int(total)
			} else {
				// Calculate from parts
				prompt := 0
				completion := 0
				if pt, ok := usage["prompt_tokens"].(float64); ok {
					prompt = int(pt)
				}
				if ct, ok := usage["completion_tokens"].(float64); ok {
					completion = int(ct)
				}
				tokenUsage = prompt + completion
			}
		}
	}

	if lastChunk == nil {
		return nil, 0, fmt.Errorf("no valid SSE chunks found")
	}

	// Build the reconstructed response
	content := contentBuilder.String()

	// Build message object
	message := map[string]interface{}{
		"role":    "assistant",
		"content": content,
	}

	// Add tool calls if present
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	// Build choice object
	choice := map[string]interface{}{
		"index":         0,
		"message":       message,
		"finish_reason": finishReason,
	}

	// Build the complete response
	response := map[string]interface{}{
		"id":      responseID,
		"object":  "chat.completion",
		"model":   model,
		"choices": []interface{}{choice},
	}

	// Add usage if available
	if tokenUsage > 0 {
		response["usage"] = map[string]interface{}{
			"total_tokens": tokenUsage,
		}
	}

	result, err := json.Marshal(response)
	return result, tokenUsage, err
}

// reconstructAnthropicResponse reconstructs an Anthropic-format response from SSE chunks.
// Anthropic SSE format:
//
//	event: message_start
//	data: {"type":"message_start","message":{"id":"msg_xxx","usage":{"input_tokens":20}}}
//
//	event: content_block_delta
//	data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}
//
//	event: message_delta
//	data: {"type":"message_delta","usage":{"output_tokens":5}}
//
//	event: message_stop
//	data: {"type":"message_stop"}
func reconstructAnthropicResponse(sseData []byte) ([]byte, int, error) {
	var contentBuilder strings.Builder
	var messageID string
	var model string
	var stopReason string
	var inputTokens, outputTokens int
	var contentBlocks []interface{}
	var currentBlockIndex int
	var currentBlockType string

	// Track content blocks for proper reconstruction
	blockContents := make(map[int]strings.Builder)

	scanner := bufio.NewScanner(bytes.NewReader(sseData))
	var currentEventType string

	for scanner.Scan() {
		line := scanner.Text()

		// Handle event type line
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Skip non-data lines
		if !strings.HasPrefix(line, SSEDataPrefix) {
			continue
		}

		data := strings.TrimPrefix(line, SSEDataPrefix)
		data = strings.TrimSpace(data)

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		eventType := currentEventType
		if eventType == "" {
			if t, ok := chunk["type"].(string); ok {
				eventType = t
			}
		}

		switch eventType {
		case AnthropicMessageStart:
			if msg, ok := chunk["message"].(map[string]interface{}); ok {
				if id, ok := msg["id"].(string); ok {
					messageID = id
				}
				if m, ok := msg["model"].(string); ok {
					model = m
				}
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					if it, ok := usage["input_tokens"].(float64); ok {
						inputTokens = int(it)
					}
				}
			}

		case AnthropicContentBlockStart:
			if idx, ok := chunk["index"].(float64); ok {
				currentBlockIndex = int(idx)
			}
			if cb, ok := chunk["content_block"].(map[string]interface{}); ok {
				if t, ok := cb["type"].(string); ok {
					currentBlockType = t
				}
			}

		case AnthropicContentBlockDelta:
			if delta, ok := chunk["delta"].(map[string]interface{}); ok {
				if text, ok := delta["text"].(string); ok {
					contentBuilder.WriteString(text)
					builder := blockContents[currentBlockIndex]
					builder.WriteString(text)
					blockContents[currentBlockIndex] = builder
				}
			}

		case AnthropicContentBlockStop:
			// Finalize the content block
			if builder, exists := blockContents[currentBlockIndex]; exists {
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": currentBlockType,
					"text": builder.String(),
				})
			}

		case AnthropicMessageDelta:
			if delta, ok := chunk["delta"].(map[string]interface{}); ok {
				if sr, ok := delta["stop_reason"].(string); ok {
					stopReason = sr
				}
			}
			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				if ot, ok := usage["output_tokens"].(float64); ok {
					outputTokens = int(ot)
				}
			}

		case AnthropicMessageStop:
			// End of message
		}
	}

	// If no content blocks were created, create one from accumulated content
	if len(contentBlocks) == 0 && contentBuilder.Len() > 0 {
		contentBlocks = []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": contentBuilder.String(),
			},
		}
	}

	// Build the complete response
	response := map[string]interface{}{
		"id":           messageID,
		"type":         "message",
		"role":         "assistant",
		"model":        model,
		"content":      contentBlocks,
		"stop_reason":  stopReason,
		"stop_sequence": nil,
		"usage": map[string]interface{}{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}

	tokenUsage := inputTokens + outputTokens
	result, err := json.Marshal(response)
	return result, tokenUsage, err
}

// reconstructGoogleResponse reconstructs a Google/Gemini/Vertex-format response from SSE chunks.
// Google SSE format:
//
//	data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}
//	data: {"candidates":[{"content":{"parts":[{"text":" world"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}
func reconstructGoogleResponse(sseData []byte) ([]byte, int, error) {
	var contentBuilder strings.Builder
	var lastChunk map[string]interface{}
	var tokenUsage int
	var finishReason string

	scanner := bufio.NewScanner(bytes.NewReader(sseData))
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, SSEDataPrefix) {
			continue
		}

		data := strings.TrimPrefix(line, SSEDataPrefix)
		data = strings.TrimSpace(data)

		if data == SSEDone {
			continue
		}

		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		lastChunk = chunk

		// Extract content from candidates
		if candidates, ok := chunk["candidates"].([]interface{}); ok && len(candidates) > 0 {
			if candidate, ok := candidates[0].(map[string]interface{}); ok {
				// Extract finish reason
				if fr, ok := candidate["finishReason"].(string); ok && fr != "" {
					finishReason = fr
				}

				// Extract content parts
				if content, ok := candidate["content"].(map[string]interface{}); ok {
					if parts, ok := content["parts"].([]interface{}); ok {
						for _, part := range parts {
							if p, ok := part.(map[string]interface{}); ok {
								if text, ok := p["text"].(string); ok {
									contentBuilder.WriteString(text)
								}
							}
						}
					}
				}
			}
		}

		// Extract token usage
		if usage, ok := chunk["usageMetadata"].(map[string]interface{}); ok {
			prompt := 0
			candidates := 0
			if pt, ok := usage["promptTokenCount"].(float64); ok {
				prompt = int(pt)
			}
			if ct, ok := usage["candidatesTokenCount"].(float64); ok {
				candidates = int(ct)
			}
			tokenUsage = prompt + candidates
		}
	}

	if lastChunk == nil {
		return nil, 0, fmt.Errorf("no valid SSE chunks found")
	}

	// Build the reconstructed response in Google format
	response := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{
							"text": contentBuilder.String(),
						},
					},
					"role": "model",
				},
				"finishReason": finishReason,
			},
		},
	}

	if tokenUsage > 0 {
		response["usageMetadata"] = map[string]interface{}{
			"totalTokenCount": tokenUsage,
		}
	}

	result, err := json.Marshal(response)
	return result, tokenUsage, err
}

// ConvertJSONToSSE converts a cached JSON response back to SSE format for streaming.
// This is called when a streaming request hits the cache.
func ConvertJSONToSSE(jsonData []byte, vendor string) ([]byte, error) {
	vendor = strings.ToLower(vendor)

	switch {
	case strings.Contains(vendor, "anthropic"):
		return convertAnthropicToSSE(jsonData)
	case strings.Contains(vendor, "google"), strings.Contains(vendor, "gemini"), strings.Contains(vendor, "vertex"):
		return convertGoogleToSSE(jsonData)
	default:
		return convertOpenAIToSSE(jsonData)
	}
}

// convertOpenAIToSSE converts a cached OpenAI JSON response to SSE format
func convertOpenAIToSSE(jsonData []byte) ([]byte, error) {
	var response map[string]interface{}
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	responseID, _ := response["id"].(string)
	model, _ := response["model"].(string)

	// Get the content from the response
	content := ""
	finishReason := "stop"
	if choices, ok := response["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if c, ok := msg["content"].(string); ok {
					content = c
				}
			}
			if fr, ok := choice["finish_reason"].(string); ok {
				finishReason = fr
			}
		}
	}

	// Generate SSE chunks - simulate streaming by chunking the content
	// For simplicity, we'll send it as a few chunks
	chunkSize := 50 // characters per chunk
	for i := 0; i < len(content); i += chunkSize {
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}

		chunk := map[string]interface{}{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"model":   model,
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"delta": map[string]interface{}{
						"content": content[i:end],
					},
				},
			},
		}

		chunkJSON, _ := json.Marshal(chunk)
		buf.WriteString(SSEDataPrefix)
		buf.Write(chunkJSON)
		buf.WriteString("\n\n")
	}

	// Send final chunk with finish_reason
	finalChunk := map[string]interface{}{
		"id":      responseID,
		"object":  "chat.completion.chunk",
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": finishReason,
			},
		},
	}

	// Add usage if available
	if usage, ok := response["usage"]; ok {
		finalChunk["usage"] = usage
	}

	chunkJSON, _ := json.Marshal(finalChunk)
	buf.WriteString(SSEDataPrefix)
	buf.Write(chunkJSON)
	buf.WriteString("\n\n")

	// Send [DONE] marker
	buf.WriteString(SSEDataPrefix)
	buf.WriteString(SSEDone)
	buf.WriteString("\n\n")

	return buf.Bytes(), nil
}

// convertAnthropicToSSE converts a cached Anthropic JSON response to SSE format
func convertAnthropicToSSE(jsonData []byte) ([]byte, error) {
	var response map[string]interface{}
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	messageID, _ := response["id"].(string)
	model, _ := response["model"].(string)
	stopReason, _ := response["stop_reason"].(string)

	// Get usage
	var inputTokens, outputTokens int
	if usage, ok := response["usage"].(map[string]interface{}); ok {
		if it, ok := usage["input_tokens"].(float64); ok {
			inputTokens = int(it)
		}
		if ot, ok := usage["output_tokens"].(float64); ok {
			outputTokens = int(ot)
		}
	}

	// message_start event
	messageStart := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":    messageID,
			"type":  "message",
			"role":  "assistant",
			"model": model,
			"usage": map[string]interface{}{
				"input_tokens":  inputTokens,
				"output_tokens": 0,
			},
		},
	}
	buf.WriteString("event: message_start\n")
	msgStartJSON, _ := json.Marshal(messageStart)
	buf.WriteString(SSEDataPrefix)
	buf.Write(msgStartJSON)
	buf.WriteString("\n\n")

	// Get content from response
	content := ""
	if contentBlocks, ok := response["content"].([]interface{}); ok && len(contentBlocks) > 0 {
		if block, ok := contentBlocks[0].(map[string]interface{}); ok {
			if text, ok := block["text"].(string); ok {
				content = text
			}
		}
	}

	// content_block_start event
	blockStart := map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	}
	buf.WriteString("event: content_block_start\n")
	blockStartJSON, _ := json.Marshal(blockStart)
	buf.WriteString(SSEDataPrefix)
	buf.Write(blockStartJSON)
	buf.WriteString("\n\n")

	// content_block_delta events - chunk the content
	chunkSize := 50
	for i := 0; i < len(content); i += chunkSize {
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}

		delta := map[string]interface{}{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": content[i:end],
			},
		}
		buf.WriteString("event: content_block_delta\n")
		deltaJSON, _ := json.Marshal(delta)
		buf.WriteString(SSEDataPrefix)
		buf.Write(deltaJSON)
		buf.WriteString("\n\n")
	}

	// content_block_stop event
	blockStop := map[string]interface{}{
		"type":  "content_block_stop",
		"index": 0,
	}
	buf.WriteString("event: content_block_stop\n")
	blockStopJSON, _ := json.Marshal(blockStop)
	buf.WriteString(SSEDataPrefix)
	buf.Write(blockStopJSON)
	buf.WriteString("\n\n")

	// message_delta event
	messageDelta := map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason": stopReason,
		},
		"usage": map[string]interface{}{
			"output_tokens": outputTokens,
		},
	}
	buf.WriteString("event: message_delta\n")
	msgDeltaJSON, _ := json.Marshal(messageDelta)
	buf.WriteString(SSEDataPrefix)
	buf.Write(msgDeltaJSON)
	buf.WriteString("\n\n")

	// message_stop event
	messageStop := map[string]interface{}{
		"type": "message_stop",
	}
	buf.WriteString("event: message_stop\n")
	msgStopJSON, _ := json.Marshal(messageStop)
	buf.WriteString(SSEDataPrefix)
	buf.Write(msgStopJSON)
	buf.WriteString("\n\n")

	return buf.Bytes(), nil
}

// convertGoogleToSSE converts a cached Google/Gemini JSON response to SSE format
func convertGoogleToSSE(jsonData []byte) ([]byte, error) {
	var response map[string]interface{}
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, err
	}

	var buf bytes.Buffer

	// Get content and finish reason from response
	content := ""
	finishReason := "STOP"
	if candidates, ok := response["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if candidate, ok := candidates[0].(map[string]interface{}); ok {
			if fr, ok := candidate["finishReason"].(string); ok {
				finishReason = fr
			}
			if c, ok := candidate["content"].(map[string]interface{}); ok {
				if parts, ok := c["parts"].([]interface{}); ok && len(parts) > 0 {
					if part, ok := parts[0].(map[string]interface{}); ok {
						if text, ok := part["text"].(string); ok {
							content = text
						}
					}
				}
			}
		}
	}

	// Get usage metadata
	var tokenUsage map[string]interface{}
	if usage, ok := response["usageMetadata"].(map[string]interface{}); ok {
		tokenUsage = usage
	}

	// Generate SSE chunks
	chunkSize := 50
	for i := 0; i < len(content); i += chunkSize {
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}

		isLast := end >= len(content)

		chunk := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"parts": []interface{}{
							map[string]interface{}{
								"text": content[i:end],
							},
						},
						"role": "model",
					},
				},
			},
		}

		// Add finish reason and usage on last chunk
		if isLast {
			if candidates, ok := chunk["candidates"].([]interface{}); ok {
				if candidate, ok := candidates[0].(map[string]interface{}); ok {
					candidate["finishReason"] = finishReason
				}
			}
			if tokenUsage != nil {
				chunk["usageMetadata"] = tokenUsage
			}
		}

		chunkJSON, _ := json.Marshal(chunk)
		buf.WriteString(SSEDataPrefix)
		buf.Write(chunkJSON)
		buf.WriteString("\n\n")
	}

	return buf.Bytes(), nil
}

// IsStreamingRequest checks if the request body indicates a streaming request
func IsStreamingRequest(body []byte) bool {
	var request map[string]interface{}
	if err := json.Unmarshal(body, &request); err != nil {
		return false
	}

	// Check for "stream": true
	if stream, ok := request["stream"].(bool); ok {
		return stream
	}

	return false
}
