// Command mockupstream is the vendor stand-in for the Phase 1 end-to-end run
// (e2e/phase1.sh). It answers OpenAI Chat Completions, OpenAI Responses,
// Anthropic Messages and Bedrock Converse, buffered and streaming, and
// records every request body so the run can compare what the gateway
// forwarded with what the client sent.
//
// Usage it reports is a pure function of the body it received, so a record's
// input tokens can be checked against it:
//
//	input  = len(body) / 4
//	cached = input / 2 when the body carries a cache marker (cache_control,
//	         cachePoint) or, for OpenAI formats, is over 4096 bytes; else 0
//
// A newest message asking ECHO_CALL(<tool>) gets a response calling that tool.
//
//	go run ./e2e/mockupstream -dir /tmp/pc-e2e-requests
//
// It listens on 127.0.0.1:18999 by default: it saves every body it gets.
// Docker Desktop containers reach that as host.docker.internal; on a Linux
// Docker host, where host.docker.internal is the bridge gateway, pass
// -addr with that gateway's address.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

var callRe = regexp.MustCompile(`ECHO_CALL\(([A-Za-z0-9_]+)\)`)

type usage struct{ input, cached, output int }

func usageFor(body []byte, openaiFormat bool) usage {
	u := usage{input: len(body) / 4, output: 12}
	if bytes.Contains(body, []byte(`"cache_control"`)) || bytes.Contains(body, []byte(`"cachePoint"`)) || (openaiFormat && len(body) > 4096) {
		u.cached = u.input / 2
	}
	return u
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18999", "listen address")
	dir := flag.String("dir", "mock-requests", "where to record requests")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatal(err)
	}
	var n atomic.Int64
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])
		i := n.Add(1)
		base := filepath.Join(*dir, fmt.Sprintf("%04d", i))
		meta, _ := json.MarshalIndent(map[string]any{
			"n": i, "time": time.Now().UTC().Format(time.RFC3339Nano), "method": r.Method, "path": r.URL.Path,
			"body_bytes": len(body), "body_sha256": hash,
		}, "", "  ")
		_ = os.WriteFile(base+".json", meta, 0o644)
		_ = os.WriteFile(base+".body", body, 0o644)
		log.Printf("#%d %s %s %d bytes", i, r.Method, r.URL.Path, len(body))

		w.Header().Set("X-Echo-Body-SHA256", hash)
		w.Header().Set("X-Echo-Request-N", fmt.Sprint(i))
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(body, &req)
		call := ""
		if m := callRe.FindAllSubmatch(body, -1); len(m) > 0 {
			call = string(m[len(m)-1][1])
		}
		text := "echo sha256=" + hash[:16]
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/converse-stream"):
			bedrockStream(w, text, call, usageFor(body, false))
		case strings.HasSuffix(p, "/converse"):
			writeJSON(w, bedrockMessage(text, call, usageFor(body, false)))
		case strings.HasSuffix(p, "/invoke"): // Bedrock InvokeModel, Anthropic body
			writeJSON(w, anthropicMessage(i, text, call, usageFor(body, false)))
		case strings.HasSuffix(p, "/messages"):
			if req.Stream {
				anthropicStream(w, i, text, call, usageFor(body, false))
			} else {
				writeJSON(w, anthropicMessage(i, text, call, usageFor(body, false)))
			}
		case strings.HasSuffix(p, "/responses"):
			if req.Stream {
				responsesStream(w, i, text, usageFor(body, true))
			} else {
				writeJSON(w, responsesObject(i, text, usageFor(body, true)))
			}
		default: // chat completions
			var so struct {
				StreamOptions struct {
					IncludeUsage bool `json:"include_usage"`
				} `json:"stream_options"`
			}
			_ = json.Unmarshal(body, &so)
			if req.Stream {
				chatStream(w, i, text, so.StreamOptions.IncludeUsage, usageFor(body, true))
			} else {
				writeJSON(w, chatCompletion(i, text, call, usageFor(body, true)))
			}
		}
	})
	log.Printf("mockupstream on %s recording to %s", *addr, *dir)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func sse(w http.ResponseWriter, event string, v any) {
	b, _ := json.Marshal(v)
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
}

// ---- OpenAI Chat Completions

func chatUsage(u usage) map[string]any {
	return map[string]any{"prompt_tokens": u.input, "completion_tokens": u.output, "total_tokens": u.input + u.output,
		"prompt_tokens_details": map[string]any{"cached_tokens": u.cached}}
}

func chatCompletion(i int64, text, call string, u usage) map[string]any {
	msg := map[string]any{"role": "assistant", "content": text}
	finish := "stop"
	if call != "" {
		msg = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
			"id": fmt.Sprintf("call_echo%d", i), "type": "function", "function": map[string]any{"name": call, "arguments": `{"path":"README.md"}`}}}}
		finish = "tool_calls"
	}
	return map[string]any{"id": fmt.Sprintf("chatcmpl-echo%d", i), "object": "chat.completion", "created": time.Now().Unix(), "model": "echo",
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}}, "usage": chatUsage(u)}
}

func chatStream(w http.ResponseWriter, i int64, text string, includeUsage bool, u usage) {
	sseHeaders(w)
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"id": fmt.Sprintf("chatcmpl-echo%d", i), "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "echo",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
	}
	sse(w, "", chunk(map[string]any{"role": "assistant", "content": ""}, nil))
	for _, part := range []string{text[:8], text[8:]} {
		sse(w, "", chunk(map[string]any{"content": part}, nil))
	}
	sse(w, "", chunk(map[string]any{}, "stop"))
	if includeUsage {
		sse(w, "", map[string]any{"id": fmt.Sprintf("chatcmpl-echo%d", i), "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": "echo",
			"choices": []any{}, "usage": chatUsage(u)})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// ---- OpenAI Responses

func responsesUsage(u usage) map[string]any {
	return map[string]any{"input_tokens": u.input, "input_tokens_details": map[string]any{"cached_tokens": u.cached},
		"output_tokens": u.output, "output_tokens_details": map[string]any{"reasoning_tokens": 0}, "total_tokens": u.input + u.output}
}

func responsesObject(i int64, text string, u usage) map[string]any {
	return map[string]any{"id": fmt.Sprintf("resp_echo%d", i), "object": "response", "created_at": time.Now().Unix(), "status": "completed", "model": "echo",
		"output": []any{map[string]any{"type": "message", "id": fmt.Sprintf("msg_echo%d", i), "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}},
		"usage": responsesUsage(u)}
}

func responsesStream(w http.ResponseWriter, i int64, text string, u usage) {
	sseHeaders(w)
	pending := responsesObject(i, "", u)
	pending["status"], pending["output"], pending["usage"] = "in_progress", []any{}, nil
	sse(w, "response.created", map[string]any{"type": "response.created", "sequence_number": 0, "response": pending})
	sse(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "sequence_number": 1, "item_id": fmt.Sprintf("msg_echo%d", i), "output_index": 0, "content_index": 0, "delta": text})
	sse(w, "response.completed", map[string]any{"type": "response.completed", "sequence_number": 2, "response": responsesObject(i, text, u)})
}

// ---- Anthropic Messages

func anthropicUsage(u usage) map[string]any {
	return map[string]any{"input_tokens": u.input - u.cached, "cache_read_input_tokens": u.cached, "cache_creation_input_tokens": 0, "output_tokens": u.output}
}

func anthropicMessage(i int64, text, call string, u usage) map[string]any {
	content := []any{map[string]any{"type": "text", "text": text}}
	stop := "end_turn"
	if call != "" {
		content = []any{map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_echo%d", i), "name": call, "input": map[string]any{"path": "README.md"}}}
		stop = "tool_use"
	}
	return map[string]any{"id": fmt.Sprintf("msg_echo%d", i), "type": "message", "role": "assistant", "model": "echo",
		"content": content, "stop_reason": stop, "usage": anthropicUsage(u)}
}

func anthropicStream(w http.ResponseWriter, i int64, text, call string, u usage) {
	sseHeaders(w)
	start := anthropicMessage(i, "", "", u)
	start["content"], start["stop_reason"] = []any{}, nil
	su := anthropicUsage(u)
	su["output_tokens"] = 1
	start["usage"] = su
	sse(w, "message_start", map[string]any{"type": "message_start", "message": start})
	stop := "end_turn"
	if call != "" {
		sse(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_echo%d", i), "name": call, "input": map[string]any{}}})
		sse(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"path":"README.md"}`}})
		stop = "tool_use"
	} else {
		sse(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		sse(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	}
	sse(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	sse(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": u.output}})
	sse(w, "message_stop", map[string]any{"type": "message_stop"})
}

// ---- Bedrock Converse

func bedrockUsage(u usage) map[string]any {
	return map[string]any{"inputTokens": u.input - u.cached, "outputTokens": u.output, "totalTokens": u.input + u.output, "cacheReadInputTokens": u.cached, "cacheWriteInputTokens": 0}
}

func bedrockMessage(text, call string, u usage) map[string]any {
	content := []any{map[string]any{"text": text}}
	stop := "end_turn"
	if call != "" {
		content = []any{map[string]any{"toolUse": map[string]any{"toolUseId": "tooluse_echo", "name": call, "input": map[string]any{"path": "README.md"}}}}
		stop = "tool_use"
	}
	return map[string]any{"output": map[string]any{"message": map[string]any{"role": "assistant", "content": content}},
		"stopReason": stop, "usage": bedrockUsage(u), "metrics": map[string]any{"latencyMs": 1}}
}

// bedrockStream writes a converse-stream response as AWS event stream
// frames: prelude (total and headers length, CRC), headers, JSON payload,
// message CRC.
func bedrockStream(w http.ResponseWriter, text, call string, u usage) {
	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	frame := func(event string, v any) {
		payload, _ := json.Marshal(v)
		var h bytes.Buffer
		for _, kv := range [][2]string{{":event-type", event}, {":content-type", "application/json"}, {":message-type", "event"}} {
			h.WriteByte(byte(len(kv[0])))
			h.WriteString(kv[0])
			h.WriteByte(7) // string
			_ = binary.Write(&h, binary.BigEndian, uint16(len(kv[1])))
			h.WriteString(kv[1])
		}
		total := 12 + h.Len() + len(payload) + 4
		var m bytes.Buffer
		_ = binary.Write(&m, binary.BigEndian, uint32(total))
		_ = binary.Write(&m, binary.BigEndian, uint32(h.Len()))
		_ = binary.Write(&m, binary.BigEndian, crc32.ChecksumIEEE(m.Bytes()))
		m.Write(h.Bytes())
		m.Write(payload)
		_ = binary.Write(&m, binary.BigEndian, crc32.ChecksumIEEE(m.Bytes()))
		_, _ = w.Write(m.Bytes())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	frame("messageStart", map[string]any{"role": "assistant"})
	stop := "end_turn"
	if call != "" {
		frame("contentBlockStart", map[string]any{"contentBlockIndex": 0, "start": map[string]any{"toolUse": map[string]any{"toolUseId": "tooluse_echo", "name": call}}})
		frame("contentBlockDelta", map[string]any{"contentBlockIndex": 0, "delta": map[string]any{"toolUse": map[string]any{"input": `{"path":"README.md"}`}}})
		stop = "tool_use"
	} else {
		frame("contentBlockDelta", map[string]any{"contentBlockIndex": 0, "delta": map[string]any{"text": text}})
	}
	frame("contentBlockStop", map[string]any{"contentBlockIndex": 0})
	frame("messageStop", map[string]any{"stopReason": stop})
	frame("metadata", map[string]any{"usage": bedrockUsage(u), "metrics": map[string]any{"latencyMs": 1}})
}
