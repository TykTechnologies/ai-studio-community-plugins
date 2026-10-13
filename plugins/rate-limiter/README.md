# Rate Limiter Plugin

Sliding-window rate limiting for LLM requests in Tyk AI Studio and the Microgateway. Rules limit requests, tokens or concurrent requests per window. Each rule is keyed by any combination of dimensions: `app_id`, `user_id`, `model`, `llm_id`, `api_key` and `global`.

Rules are managed on the plugin's **Rate Limit Rules** page, under **Rate Limiting** in the AI Studio sidebar. They are stored in the plugin's KV store and copied into the plugin config, so edge gateways receive them with their configuration snapshot.

## Configuration

| Key | Default | Description |
|-----|---------|-------------|
| `storage_backend` | `kv` | `kv` (plugin KV store, counters local to each instance) or `redis` (counters shared across instances) |
| `redis_url` | | Redis URL when `storage_backend` is `redis`. If Redis is unreachable at start-up, the plugin falls back to `kv`. |
| `window_size_seconds` | `60` | Length of the sliding window |
| `fail_open` | `true` | What happens when the limiter cannot read its counters (see below) |

## Failure handling

The limiter fails open or closed according to `fail_open`. Both of these count as a storage failure:

- the store cannot be reached, or
- a stored counter cannot be decoded (corrupt data). The error is logged with the counter's key. A corrupt counter is left in place until its TTL expires; it is never silently treated as zero.

With `fail_open: true` the request is allowed and the response carries `X-RateLimit-Error: storage_unavailable`. With `fail_open: false` the request is rejected with `503 {"error":"rate limiter storage unavailable"}`.

The Redis backend decodes counters in Lua scripts, and a corrupt value fails the script, so both backends behave the same way.

## Editing rules

Every rule change is checked against the rule set's version. If two administrators save changes based on the same version, the first save wins and the second gets a version-conflict error, so it can reload and retry. Saves are serialised within the plugin process, because the KV store has no compare-and-swap.

`getRuleStats` returns a rule's limit and the time the current window resets. It does not report a usage figure: usage is counted per dimension key, wherever requests are enforced (each gateway's KV or a shared Redis), so there is no single number per rule.

## Shutdown

On shutdown the plugin closes its Redis connection pool.
