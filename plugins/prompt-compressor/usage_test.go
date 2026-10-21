package main

import (
	"encoding/binary"
	"hash/crc32"
	"testing"
)

// Recorded response shapes, trimmed to what the parser reads.
func TestParseUsageBuffered(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want usage
	}{
		{"anthropic", `{"id":"msg_1","type":"message","content":[{"type":"text","text":"hi"}],
			"usage":{"input_tokens":12,"cache_creation_input_tokens":100,"cache_read_input_tokens":3000,"output_tokens":7}}`,
			usage{InputTokens: 3112, CacheReadTokens: 3000, CacheWriteTokens: 100, OutputTokens: 7}},
		{"openai chat", `{"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":2048,"completion_tokens":20,"total_tokens":2068,
			"prompt_tokens_details":{"cached_tokens":1920,"audio_tokens":0}}}`,
			usage{InputTokens: 2048, CacheReadTokens: 1920, OutputTokens: 20}},
		{"openai chat without cache details", `{"usage":{"prompt_tokens":10,"completion_tokens":2}}`,
			usage{InputTokens: 10, OutputTokens: 2}},
		{"openai responses", `{"id":"resp_1","object":"response","output":[],"usage":{"input_tokens":5000,
			"input_tokens_details":{"cached_tokens":4096},"output_tokens":300,"output_tokens_details":{"reasoning_tokens":200},"total_tokens":5300}}`,
			usage{InputTokens: 5000, CacheReadTokens: 4096, OutputTokens: 300}},
		{"google", `{"candidates":[],"usageMetadata":{"promptTokenCount":900,"candidatesTokenCount":50,"cachedContentTokenCount":800,"totalTokenCount":950}}`,
			usage{InputTokens: 900, CacheReadTokens: 800, OutputTokens: 50}},
		{"bedrock converse", `{"output":{"message":{}},"stopReason":"end_turn","usage":{"inputTokens":40,"outputTokens":9,
			"cacheReadInputTokens":1000,"cacheWriteInputTokens":60,"totalTokens":1109}}`,
			usage{InputTokens: 1100, CacheReadTokens: 1000, CacheWriteTokens: 60, OutputTokens: 9}},
	} {
		got, ok := parseUsage([]byte(tc.body))
		if !ok || got != tc.want {
			t.Errorf("%s: %+v, %v; want %+v", tc.name, got, ok, tc.want)
		}
	}
	for _, body := range []string{`{"id":"x","choices":[]}`, `not json`, `{"usage":{}}`, ``} {
		if _, ok := parseUsage([]byte(body)); ok {
			t.Errorf("%q: usage found", body)
		}
	}
}

func TestParseUsageStream(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stream string
		want   usage
	}{
		{"anthropic", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":20,\"cache_read_input_tokens\":4000,\"cache_creation_input_tokens\":0,\"output_tokens\":1}}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":55}}\n\n",
			usage{InputTokens: 4020, CacheReadTokens: 4000, OutputTokens: 55}},
		{"openai chat with include_usage", "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":300,\"completion_tokens\":4,\"prompt_tokens_details\":{\"cached_tokens\":256}}}\n\ndata: [DONE]\n\n",
			usage{InputTokens: 300, CacheReadTokens: 256, OutputTokens: 4}},
		{"openai responses", "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"usage\":null}}\n\n" +
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":7000,\"input_tokens_details\":{\"cached_tokens\":6144},\"output_tokens\":90}}}\n\n",
			usage{InputTokens: 7000, CacheReadTokens: 6144, OutputTokens: 90}},
		{"google", "data: {\"candidates\":[],\"usageMetadata\":{\"promptTokenCount\":500,\"cachedContentTokenCount\":400}}\r\n\r\n" +
			"data: {\"candidates\":[],\"usageMetadata\":{\"promptTokenCount\":500,\"candidatesTokenCount\":30,\"cachedContentTokenCount\":400}}\r\n\r\n",
			usage{InputTokens: 500, CacheReadTokens: 400, OutputTokens: 30}},
		{"bedrock converse-stream", string(eventStreamFrame(`{"contentBlockDelta":{"delta":{"text":"hi"}}}`)) +
			string(eventStreamFrame(`{"metadata":{"usage":{"inputTokens":30,"outputTokens":8,"cacheReadInputTokens":2000,"cacheWriteInputTokens":0},"metrics":{"latencyMs":120}}}`)),
			usage{InputTokens: 2030, CacheReadTokens: 2000, OutputTokens: 8}},
	} {
		got, ok := parseStreamUsage([]byte(tc.stream))
		if !ok || got != tc.want {
			t.Errorf("%s: %+v, %v; want %+v", tc.name, got, ok, tc.want)
		}
	}
	// An OpenAI stream without stream_options.include_usage has none.
	if _, ok := parseStreamUsage([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")); ok {
		t.Error("usage found in a stream without it")
	}
}

// eventStreamFrame builds an AWS event-stream frame with no headers, as
// Bedrock's converse-stream sends them.
func eventStreamFrame(payload string) []byte {
	total := 12 + len(payload) + 4
	b := make([]byte, 12, total)
	binary.BigEndian.PutUint32(b[0:], uint32(total))
	binary.BigEndian.PutUint32(b[4:], 0)
	binary.BigEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b[:8]))
	b = append(b, payload...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
}
