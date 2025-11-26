# LLM Response Cache Plugin

A community plugin for Tyk AI Studio that caches LLM responses to reduce costs and latency.

## Features

- **Deterministic Cache Keys**: SHA-256 hashed keys based on model, messages, tools, and temperature
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

2. **Response Phase (OnBeforeWrite)**
   - Retrieve pending cache operation by request ID
   - Store LLM response in cache with TTL
   - Add cache status headers to response

## Cache Key Components

The cache key is generated from:
- **Namespace**: API key hash, app ID, or org ID (configurable)
- **Model**: The LLM model identifier
- **Messages**: All message content (normalized)
- **System Prompt**: System instructions
- **Tools**: Tool definitions (sorted alphabetically)
- **Temperature**: Temperature value (different temps = different keys)

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
- **Streaming**: Streaming responses are cached as complete responses and returned non-streamed on HIT

## Future Enhancements (Enterprise)

- Redis/distributed cache backend
- Streaming response re-injection
- Per-route TTL configuration
- Cache warming/preloading
- Analytics and cost tracking

## License

This plugin is part of the Tyk AI Studio community plugins.
