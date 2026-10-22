# Rate Limiter Plugin

Sliding-window rate limiting for LLM requests in Tyk AI Studio and the Microgateway. Rules limit requests, tokens or concurrent requests per window. Each rule is keyed by any combination of dimensions: `app_id`, `user_id`, `model`, `llm_id`, `api_key` and `global`.

Rules are managed on the plugin's **Rate Limit Rules** page, under **Rate Limiting** in the AI Studio sidebar. They are stored in the plugin's KV store and copied into the plugin config, so edge gateways receive them with their configuration snapshot.

## Configuration

| Key | Default | Description |
|-----|---------|-------------|
| `storage_backend` | `kv` | `kv` (plugin KV store, counters local to each instance) or `redis` (counters shared across instances) |
| `redis_url` | | Redis URL when `storage_backend` is `redis`. If Redis is unreachable at start-up, the plugin falls back to `kv`. Keep the password out of the URL: the config form shows the URL in clear. |
| `redis_password` | | Redis password. Overrides a password in `redis_url`. The config form masks it. Accepts a reference instead of the value (see below). |
| `window_size_seconds` | `60` | Length of the sliding window |
| `fail_open` | `true` | What happens when the limiter cannot read its counters (see below) |

### Redis password references

`redis_password` can be a reference instead of the value:

- `$SECRET/NAME` names a secret in AI Studio's secret store. AI Studio 2.3 and later resolve it before the plugin starts, on AI Studio and on edge gateways, where the value travels encrypted like an LLM API key. Edges older than 2.3 receive the reference, not the value. Saving a `$SECRET/` reference needs permission to read secrets.
- `$ENV/NAME` is read from the plugin's own environment. On AI Studio 2.3 and later, pass the variable to plugins with `PLUGIN_ENV_PASSTHROUGH`.

A reference that cannot be resolved (an older Studio or edge, a missing secret, or the `refused-credential-reference` an edge gets in its place) is never sent to Redis as the password. Redis set-up fails, and the plugin falls back to `kv` and logs why. Limits are then counted per instance instead of shared, so on edges older than 2.3 give `redis_password` as `$ENV/NAME` (with the variable passed through) or as the value.

## Token rules

A token rule checks the window's count before the request and adds the response's actual token usage after it, so the request that crosses the limit completes and the next one is blocked. Usage is read in each vendor's shape with the Plugin SDK's `ResponseUsage`: OpenAI `prompt_tokens`/`completion_tokens`, Anthropic `input_tokens`/`output_tokens`, Google `usageMetadata` and Bedrock Converse `inputTokens`/`outputTokens` (counted since 1.2.0). The total is the vendor's `total_tokens` when it reports one, else input plus output; Anthropic and Bedrock prompt-cache reads and writes are not added. A buffered response carries `X-RateLimit-Tokens-Used`.

Since 1.2.0, streamed responses count too: when a stream ends, its usage is read from the whole stream (OpenAI streams report usage only with `stream_options.include_usage`), and its concurrent slots are released. Before 1.2.0 streams added no tokens, and a stream's concurrent slot was held until its counter expired five minutes later. A stream that ends with an upstream error releases its slots as well.

## Concurrent rules

A concurrent rule takes a slot when a request passes it and gives the slot back when the response (or the stream) ends, also when it ends with an upstream error. A request that is answered before it reaches the upstream gets no response phase: a block by a later plugin (a cache hit, for example), a request filter or guardrail block, or a stream the gateway ends early. Its slot is given back once the request is ten minutes old. When the rate limiter blocks a request itself, the slots the request took under earlier rules are given back at once. Before 1.2.0 such slots were never given back while traffic kept the counter alive, so the limit crept down.

## Plugin Order

An LLM's plugins run in one order, the order of the LLM's plugin list. `pre_auth` plugins run before any `post_auth` plugin, and each `post_auth` plugin gets the body the previous one returned. A `post_auth` block (a cache hit, a 429, a firewall block) answers at once: later plugins, request filters and every response hook are skipped for that request. On `/ai/`, `/v1/`, routers and `/anthropic/` the chain runs on the gateway's internal `/llm/call/` hop, once per failover attempt, on the vendor's native body.

Recommended `post_auth` order: LLM Firewall (when it runs in `post_auth`), response caches, Rate Limiter, Prompt Compressor, LLM Load Balancer.

- **Put the rate limiter before the Prompt Compressor.** It does not read the body; its keys are the App, user, LLM and API key. Token rules count the usage the vendor reports, once per request, which is the usage of the compressed body whatever the order, so compression is reflected either way. Before the compressor, a request refused with 429 is never compressed.
- Cached tokens count where the vendor's total includes them (OpenAI, Google, Bedrock) and not where there is no total (Anthropic: input plus output).
- **Cache hits are not rate limited** when a cache comes before the rate limiter: a hit answers before the rate limiter runs.
- **Known issue:** Microgateway edges managed by AI Studio run an LLM's plugins in the order the plugins were installed, not the order set on the LLM.

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
