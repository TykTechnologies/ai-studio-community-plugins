# LLM Response Cache Plugin

A community plugin for Tyk AI Studio that caches LLM responses to reduce costs and latency.

## Features

- **Deterministic Cache Keys**: SHA-256 hashed keys over the LLM, its API and the whole request
- **Prompt Normalization**: Normalizes whitespace and JSON ordering to improve cache hit rates
- **Namespace Isolation**: Isolates cache entries by API key, app ID, or organization ID
- **LRU Eviction**: Automatically evicts least recently used entries when cache is full
- **TTL Expiration**: Configurable time-to-live for cached responses
- **Bypass Support**: Skip caching via header (`X-Cache: bypass`) or query param (`?cache=bypass`)
- **Admin Dashboard**: Real-time metrics and cache management via the admin UI

## How It Works

1. **Request Phase (PostAuth)**
   - Generate deterministic cache key from request content
   - Check in-memory cache for existing response
   - On HIT: Return cached response immediately (blocks upstream call)
   - On MISS: Store cache key for response phase

2. **Response Phase (OnBeforeWrite / OnStreamComplete)**
   - Retrieve pending cache operation by request ID
   - Store the LLM's JSON response in cache with TTL; a streamed response is
     rebuilt into JSON first
   - Add cache status headers to response

3. **Replay**
   - A JSON request gets the stored JSON; a streaming request gets it converted
     into the vendor's own stream (OpenAI, Anthropic and Gemini formats)
   - Headers describing the body (`Content-Type`, `Content-Length`) are set for
     what is actually sent, never copied from the stored response
   - Only plain-text answers in a single choice are converted between JSON and
     a stream. Tool calls, thinking blocks and multiple choices are replayed
     only in the form they were stored in; otherwise the request goes upstream
   - Vendors whose streams the cache cannot produce (for example Ollama's
     NDJSON) are only served JSON from the cache; their streaming requests go
     upstream uncached

## Cache Key Components

The cache key is generated from:
- **Namespace**: API key hash, app ID, or org ID (configurable)
- **LLM**: the LLM's ID and vendor. Two LLMs never share an entry, even with
  the same model name and messages
- **API**: the vendor API path (for example `/v1/messages`,
  `/v1/chat/completions` or Gemini's `models/{model}:generateContent`, which
  also names the model)
- **Request**: every field of the request body (messages or Gemini `contents`,
  system prompt, tools and tool results, temperature, `max_tokens`,
  `response_format`, `n`, stop sequences and so on), except `stream` and
  `stream_options`, which only choose how the answer is delivered

With `normalize_prompts`, prompt text is compared with its whitespace
collapsed and tool definitions regardless of their order.

Requests reach the cache on the gateway's `/llm/` endpoints in the LLM's
native API. That includes Studio's OpenAI-compatible endpoints (`/ai/{slug}/v1`
and the unified `/v1` router), whose drivers call `/llm/call/{slug}/` in the
native API and stream from it.

## Configuration

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable/disable the cache |
| `ttl_seconds` | integer | `3600` | Time-to-live for cached responses (60-86400) |
| `max_entry_size_kb` | integer | `2048` | Maximum size of a single cache entry |
| `max_cache_size_mb` | integer | `256` | Maximum total cache size before LRU eviction |
| `namespaces` | array | `["api_key"]` | Fields for cache isolation (`api_key`, `app_id`, `org_id`) |
| `normalize_prompts` | boolean | `true` | Normalize whitespace and JSON ordering |
| `expose_cache_key_header` | boolean | `false` | Include `X-Cache-Key` in responses |

## Response Headers

| Header | Description |
|--------|-------------|
| `X-Cache-Status` | `HIT`, `MISS`, or `BYPASS` |
| `X-Cache-Key` | Cache key hash (if `expose_cache_key_header` enabled) |
| `X-Cache-Age` | Seconds since entry was cached (on HIT) |
| `X-Cache-TTL` | Seconds until entry expires (on HIT) |

## Bypass Cache

To bypass the cache for a specific request:

```bash
# Via header
curl -H "X-Cache: bypass" ...

# Via query parameter
curl "https://api.example.com/v1/chat?cache=bypass" ...
```

## Building

```bash
cd community/plugins/llm-cache
go build -o llm-cache .
```

## Installation

1. Build the plugin binary
2. Configure the plugin in AI Studio
3. Access the dashboard at `/admin/llm-cache/dashboard`

## Metrics

The admin dashboard displays:
- **Hit Rate**: Percentage of requests served from cache
- **Cache Hits/Misses**: Total counts
- **Bypass Count**: Requests that bypassed the cache
- **Tokens Saved**: Estimated tokens saved by cache hits
- **Active Entries**: Current number of cached responses
- **Cache Size**: Current memory usage and capacity
- **Eviction Count**: Number of entries evicted due to size limits

## Limitations

- **In-memory only**: Cache is not persisted across restarts
- **Single instance**: Cache is not shared between gateway instances
- **Streaming**: Streamed answers with tool calls or thinking blocks are not cached

## Future Enhancements (Enterprise)

- Redis/distributed cache backend
- Streaming response re-injection
- Per-route TTL configuration
- Cache warming/preloading
- Analytics and cost tracking

## License

This plugin is part of the Tyk AI Studio community plugins.
