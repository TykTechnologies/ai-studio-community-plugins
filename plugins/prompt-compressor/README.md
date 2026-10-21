# Prompt Compressor Plugin

A community plugin for Tyk AI Studio that shrinks LLM request bodies on the
Microgateway before they reach the vendor, starting with tool outputs.

- **Cache-safe:** the output is a pure function of the text and the policy
  version, so a compressed prefix stays byte-identical turn after turn and the
  provider's prompt cache keeps hitting.
- **Measured:** it reports tokens and money saved next to the cache-read share.
- **Fails open:** any error passes the request through unchanged.

> **Status:** levels 1 to 3 and the external engines on the Microgateway in
> `audit` and `enforce` modes, with savings records, the dashboard, the
> Policies page and the preview tool in Studio. User docs:
> `docs/site/docs/prompt-compressor.md` in the AI Studio repo.

## Hooks

| Hook | Runtime | What it does |
|------|---------|--------------|
| `post_auth` | Microgateway | Compresses the request body (`enforce`) or measures what it would save (`audit`) |
| `on_response` | Microgateway | Labels the response (`X-Tyk-Compression`: applied, audit, bypassed, skipped, rejected or failed_open; `X-Tyk-Compression-Tokens-Saved`, an estimate; `X-Tyk-Compression-Policy`, the policy's short hash), reads the usage including cache-read and cache-write tokens (buffered bodies, server-sent events and Bedrock event streams), and records the request. An OpenAI stream carries usage only with `stream_options.include_usage`; without it the record says the usage is missing |
| `studio_ui` | Studio | Admin pages: policies, preview, savings dashboard |

`post_auth` runs only on the gateway's `/llm/` endpoints. Studio's
OpenAI-compatible endpoints (`/ai/{slug}/v1`, the unified `/v1` and
`/router/`) reach it through their internal `/llm/call/` hop for every vendor
except Bedrock, which they call directly.

### Coverage

| Traffic | Compressed? |
|---------|-------------|
| `/llm/rest`, `/llm/stream`, `/llm/call` (vendor-native bodies) | Yes |
| `/ai/`, unified `/v1`, `/router/` to a non-Bedrock LLM | Yes, on the internal `/llm/call/` hop (vendor-native body) |
| `/ai/` and `/anthropic/` to a Bedrock LLM | **No**: these call AWS directly and run no `post_auth`, until the core `pre_upstream` hook exists |
| Studio Chat | No: Chat does not go through the gateway |

The request format comes from the LLM's vendor, the path and the LLM's
endpoint (`pkg/requestformat`); the plugin reads the endpoint with `llms.read`
and falls back to the vendor's own format without it.

### Request handling

- `mode: off` does nothing; `audit` compresses, records the result and
  forwards the original body; `enforce` forwards the compressed body.
- `X-Tyk-Compression: off` skips compression for one request when
  `allow_client_bypass` is on (it is off by default). No header can choose
  a policy or raise a level.
- The hook never fails a request: it returns no error, recovers panics
  (counted as `failed_open`), and passes the request through on anything it
  cannot do. It returns no request headers, so multi-value headers reach the
  vendor as the client sent them. It never logs headers or bodies.
- What it learned is kept by request ID for the response hook (300 s, at
  most 8,192 requests).

## Configuration

The plugin gets one config for every LLM it is attached to. Named policies
hold the compression settings; bindings pick a policy per LLM or App.

```json
{
  "mode": "audit",
  "default_policy": "safe",
  "policies": {
    "safe":   {"level": 1},
    "agents": {"level": 2, "min_unit_tokens": 400,
               "tool_overrides": {"Bash": {"max_unit_tokens": 4000},
                                  "read_contract": {"exclude": true}},
               "protect_patterns": ["[A-Z]{2,6}-[0-9]+"]}
  },
  "bindings": {"llm:claude-prod": "agents", "app:42": "off"}
}
```

- **mode:** `off`, `audit` (the default: compute and record, forward the
  original) or `enforce`.
- **allow_client_bypass** (default false) honours `X-Tyk-Compression: off`.
  Turn it on for clients that must sometimes send a body exactly as
  written. It is not a security control: bypass can only turn compression
  off, so the vendor gets exactly the body the client sent, as without the
  plugin. The header can never pick a policy or raise a level.
- **max_in_flight_bytes** (default 16 MiB, at most 256 MiB, never below
  `max_body_bytes`) bounds the body bytes the plugin parses and compresses
  at once, all requests together. Compressing a body allocates about 10-20
  times its size and concurrency multiplies that, so at the default the
  worst case stays near 250 MiB. A request that would go over is forwarded
  unchanged at once with reason `over_budget`, never queued; like any
  uncompressed turn between compressed ones it can cost a prompt-cache
  rewrite, so a dashboard showing `over_budget` means the budget is too
  small for the edge's traffic. It is not on the Policies page:
  `policies:write` cannot raise it.
- **Policies** start from the defaults (level 1, tool outputs only,
  `min_unit_tokens` 300, `protect_recent_turns` 1, `cache_mode`
  deterministic) and list only what they change. `off` is a reserved policy
  name that turns compression off; `default` is the defaults when no policy of
  that name is defined.
- **Bindings** are keyed `llm:<slug>`, `llm_id:<id>` or `app:<id>`. Don't use
  the per-LLM config override: the gateway applies it only when the plugin is
  on exactly one LLM.
- **Precedence:** the App metadata key `prompt_compressor.policy` (needs
  `apps.read`; cached 30 s per App), then `app:`, `llm_id:`, `llm:`, then
  `default_policy`. A policy name that doesn't exist in App metadata falls
  back to the bindings. While an App's metadata is unknown the App's
  requests are forwarded unchanged, since the App may have opted out: reason
  `app_scope_missing` when the host refuses the lookup (`apps.read` isn't
  approved; the status RPC and the dashboard say so too), else
  `app_policy_unknown` (the lookup is pending or the host is unreachable).
  A request waits at most 25 ms for the lookup. Once known, the metadata is
  kept while a refresh fails. A client bypass is recorded with reason
  `client_bypass`.
- **Engine credentials** are top-level keys, `engine_<name>_url` and
  `engine_<name>_token`, so `$SECRET/` references work (the edge resolves
  them; a refused reference reads as no URL or no token). The URL must be
  http or https and must not carry credentials (`user:pass@`; put them in
  the token); errors and logs name the setting, never its value, and log
  only the engine's host. The token, when set, goes as `Authorization:
  Bearer`, and only over https or to a loopback host (`localhost`,
  `127.0.0.1`, `::1`): an engine with a token behind plain http to any
  other host is never called, so its units fail (see `failure_mode`). An
  unauthenticated sidecar needs no token.
- **Egress:** the plugin checks every address it dials for an engine,
  after name resolution, so DNS rebinding cannot get around it: link-local
  addresses (169.254.0.0/16 and fe80::/10, cloud metadata among them),
  fd00:ec2::254 and unspecified addresses are refused. Loopback and private
  addresses are allowed, for sidecars. Proxy settings are ignored and
  redirects are not followed.
- **Engines:** a policy's `engine` (`name`, `kind`, `timeout_ms`) sends the
  units it selected, after levels 1–3, to an external engine:
  - `headroom`: `POST <url>/v1/compress` with `{"messages":[{"role","content"}],
    "model"}`, answering `{"messages":[{"content"}]}`;
  - `llmlingua2`: `POST <url>/compressPrompt` with `{"context":[text],
    "rate":0.5,"force_tokens":[...]}`, answering `{"compressed_prompt"}`.
  One call per leaf, up to 8 at a time, each within `timeout_ms`. System
  prompts, tool definitions, the newest (protected) turns, masked and
  deduplicated results are never sent, nor is anything on an LLM where
  level 3 is capped. Protect-pattern matches go with each call (as
  LLMLingua-2 force tokens) and an answer that drops one, or is not
  shorter, is not used. Redirects are not followed. Every answer is
  memoised by the text sent, the policy hash and the engine, in process, so
  a unit already sent compresses to the same bytes on every later turn even
  when the engine is down or not deterministic. The memo is also kept in
  the edge's KV (7 days), so it survives a restart, but its compressed
  text, which is mostly the client's tool output, only where the host keeps
  the LLM's request bodies (`ANALYTICS_STORE_REQUESTS` on an edge, and the
  LLM not set to `dont_log_bodies`; a host too old to say counts as not).
  Elsewhere KV keeps only the passthrough decisions, which hold no text,
  and after a restart a unit is sent to the engine again: with an engine
  that is not deterministic its bytes may change once, a one-off
  prompt-cache miss. KV is per edge: two edges may answer differently for
  the same unit unless the engine is deterministic.
- **failure_mode:** `open` (the default) forwards a unit the engine failed
  on at levels 1–3 and memoises that, so a later success cannot rewrite
  what the vendor has cached; `closed` refuses the request with a 502
  (`failed_closed`), in enforce mode only. Both are counted.
- **Policy hash:** every record carries the SHA-256 of the policy's effective
  settings and the strategy version, so a reviewer can tell which rules
  changed a request. Key order and settings left at their defaults don't
  change it; engine credentials are not part of it.

An invalid config is logged and turns compression off; it never stops the
plugin loading. The full schema is in `config.schema.json`.

### Levels

- **Level 1 (lossless):** JSON documents are compacted on the bytes (key
  order, duplicate keys, number spelling and escapes kept) and their string
  values get the text strategies; text loses ANSI escape codes, carriage-return
  progress frames (rendered as a terminal shows them), trailing whitespace and
  runs of blank lines.
- **Level 2:** level 1, then a reducer when the text has a shape it knows.
  Test runners (pytest, jest, go test, cargo test, Maven surefire, Gradle)
  drop passing tests, progress, downloads and up-to-date tasks and keep
  every failure with its message and location and the totals. Logs and
  listings: terraform plan (Refreshing lines and `(known after apply)`
  attributes go), kubectl (healthy pods in a table; labels, annotations,
  volumes, mounts and Normal events in describe output), git diff (context
  more than 3 lines from a change, and the changes to lock and generated
  files such as package-lock.json, go.sum or *.min.js), grep and ripgrep
  (more than 5 matches in one file, and matches under vendor/,
  node_modules/ and the like), HTML pages (script and style blocks, link
  and meta tags), directory listings (deeper than 4 levels, more than 30 entries
  in one directory, anything under node_modules, .git, vendor, dist, build,
  target and the like), build logs (all but the last 20 lines, step headers
  and errors), stack traces (runs of framework frames) and repeated lines
  (folded with a count). Every reducer keeps the lines it keeps verbatim,
  and every line a `protect_patterns` regex matches; the log and listing
  reducers also keep every line that mentions an error, failure, panic,
  exception or fatal (grep matches excepted: they are code). Text no
  reducer recognises (prose, code) gets level 1 only. A reducer's cut is
  used only if it removes at least 30% of the text: the newest turn's tool
  output goes at level 1 and is reduced on the next request, which rewrites
  it in the vendor's prompt cache, and a smaller cut does not pay for that
  (replaying the corpus sessions, 30% gave the lowest cost with the cache).
- **Level 2, JSON:** an array of 20 items or more, at any depth of a JSON
  tool output (also JSON inside a string), keeps its first and last 5 items,
  every item whose members say it failed (`error`/`errors`/`exception` set,
  `status`/`state`/`phase`/`result` naming a failure such as `Failed` or
  `CrashLoopBackOff`, an HTTP `status` of 400 or more, a non-zero
  `exit_code`, `success: false`), every item holding a numeric outlier
  (modified z-score over 3.5) and every item a `protect_patterns` regex
  matches. Each run of dropped items becomes one marker string, so the
  output is still JSON; kept items are their input bytes, member order
  included, and arrays inside a kept item stay whole.
- **Size budgets:** `tool_overrides.<tool>.max_unit_tokens` (UTF-8 bytes / 4)
  makes level 2 cut that tool's long JSON arrays harder until the output
  fits: arrays of 13 items or more, then 5 items kept at each end, then 3, 2
  and 1 (the floor). A budget the output already fits changes nothing, and
  text reducers have no harder setting.
- **Level 3:** level 2, then cuts for what is still over the size budget
  (the tool's `max_unit_tokens`, else the policy's, default 6000). Text keeps
  its first lines (a third of the budget) and its last lines, plus every
  line a `protect_patterns` regex matches, with a marker between; a text
  with no short lines is cut by bytes. JSON has its arrays cut to the floor
  above, then, if still over, becomes its field names and value types
  (`"string"`, `"number"`, `"boolean"`, one item per array and a count),
  still JSON. With `mask_after_turns` set (0, the default, does not mask),
  tool results older than that many turns become a placeholder
  (`[tyk-compressed: shell output, 12,345 tokens, masked after 8 turns]`);
  the masking boundary moves only when it would mask at least
  `mask_step_tokens` more and at least twice the tokens the move makes the
  vendor re-cache, so each move, which rewrites the cached prefix once,
  buys a large saving. Even so, replaying the corpus sessions, masking
  saved money on OpenAI chat (cached input at half price) and Bedrock but
  cost more than it saved on Anthropic and GPT-5 (cache reads at a tenth),
  so turn it on per policy where cached input is not much cheaper than
  input. Masking replaces the whole result and ignores
  `protect_patterns`. Results held in several leaves (structured JSON tool
  results) are not masked. Level 3 is capped at 2 on LLMs with request
  filters, and on LLMs the plugin has not looked up yet, unless
  `lossy_with_request_filters` is set (see below); the edge reports request
  filters from Studio 2.3 on, and older hosts are treated as unknown, so
  level 3 stays capped there.
- `protect_recent_turns` keeps the newest turns at level 1.
- **drop_tools** (any level): tool definitions to remove from requests, by
  name or by a prefix ending in `*` (`["legacy_*", "browser_screenshot"]`), for
  OpenAI chat, Responses, Anthropic, Google `functionDeclarations` and
  Bedrock `toolConfig.tools`. A tool the conversation already called, or
  that `tool_choice` (Google `allowedFunctionNames`, Bedrock `toolChoice`)
  names, is always kept, and an array the list would empty is left whole.
  The kept definitions, and every other byte of the body, are sent as they
  were. Set it per App with a binding or the App metadata override; the
  dashboard's never-called tools are the candidates. Changing the list is a
  one-off prompt-cache miss for the Apps it applies to. Removed definitions
  count as the `drop_tools` strategy.

  **drop_tools saves tokens; it is not an access control.** The client
  decides whether a tool survives: a tool named by a tool call in the
  history (which the client writes) or by `tool_choice` is kept,
  `X-Tyk-Compression: off` skips the policy while `allow_client_bypass` is
  on (it is off by default), and the request passes through whole in
  `audit` mode, while an App's metadata is unknown, over `max_body_bytes`
  or `max_in_flight_bytes`, or on any error. To keep a tool away from a
  model, don't give the App access to it, or block the call with a filter
  or a guardrail on tool calls.
- Tool results the vendor produced (Anthropic web search, web fetch, code
  execution and MCP connector results; Google code execution; the Responses
  built-in tools) are never rewritten, nor are thinking blocks, signatures
  or tool call ids. The model's own text is rewritten only when a policy's
  `roles` includes `assistant`.

A reducer's cut leaves an inline marker, in a fixed format:

```
[tyk-compressed: pytest output, 18,400 → 1,150 tokens; 3 failures kept]
[tyk-compressed: 12 lines]
```

The first cut in an output carries the reducer, the size before and after
(UTF-8 bytes / 4, so it depends only on the text) and, for test runners, the
failures kept; each later cut carries the number of lines it removed, or
what it removed: `[tyk-compressed: previous line repeated 39 more times]`,
`[tyk-compressed: 35 more matches in internal/store/ledger.go]`,
`[tyk-compressed: 6 framework frames]`, `[tyk-compressed: 20 healthy rows]`.
In JSON the markers are array items and count items:
`"[tyk-compressed: json output, 12,559 → 5,346 tokens; 13 items cut]"`, then
`"[tyk-compressed: 4 items]"`.

### Policies page

Admin > Prompt Compressor > Policies edits the mode, the policies and their
bindings, and saves them through the host's plugin config update. Edge
gateways show as pending and apply the change after the next configuration
push (Edge Gateways page); they do not pull it. It validates before it
saves and warns that a change makes every affected session miss the prompt
cache once. LLMs are bound by ID (`llm_id:<id>`); `llm:<slug>` bindings
written by hand are shown with the LLM they match.

- Reading needs the plugin's **Compression policies read** permission,
  saving **Compression policies write** (`policies:read` / `policies:write`
  in the manifest's `rbac` block).
- The page owns only the policy settings (`mode`, `default_policy`,
  `policies`, `bindings`, `allow_client_bypass`, `failure_mode`,
  `lossy_with_request_filters`, `max_body_bytes`). Engine credentials and
  any other key are stored exactly as they were, and a save that changes,
  adds or removes one is refused: `policies:write` must not be enough to
  point an engine URL elsewhere while keeping its token. Edit them in the
  plugin's own configuration form, which needs plugin edit rights (and
  secrets read for `$SECRET/` references). Stored token values are masked on
  the page, and engine URLs show only their scheme, host and path: a query
  string, fragment or userinfo is masked, since it can carry a key.
  `$SECRET/` references are shown as they are.
- Level 3 and engines are skipped on LLMs with request filters unless
  `lossy_with_request_filters` is set. The page marks the LLM bindings and
  names the LLMs where that happens, from the host's LLM list
  (`has_request_filters`, which says only whether an LLM has request
  filters, not which). On a host too old to report it, the page shows a
  general note on every level-3 policy instead, and computes LLM slugs the
  way the gateway does.

## Savings records

Each edge merges its completed requests into hourly totals per App, LLM,
policy version and outcome. Every minute, and on shutdown, it queues them
for Studio with `SendToControl` (metadata `metric_type:
prompt_compressor_savings`). A batch the edge cannot queue is kept for the
next flush.

- **Counts and ids only.** Records carry token, byte and unit counts,
  strategy names, compression time and the vendor's usage counts. No prompt
  or tool text leaves the edge. The model name is used to look up its price
  at the edge and is not sent. Token counts are estimates: for OpenAI-family
  models the tokenizer runs on at most 8 KB of the request's own tool text
  and the rest is counted at that bytes-per-token ratio (encoding every
  leaf allocated hundreds of times the body); other vendors use a fixed
  characters-per-token ratio. Counts never change what is compressed.
  `max_body_bytes` (default 4 MiB) is capped at 16 MiB: compressing a body
  allocates about 10-20 times its size, and a larger body is never parsed;
  `max_in_flight_bytes` bounds the total across requests. An external
  engine's answer is read up to twice the text sent plus 64 KiB.
- **Tool definitions.** Every request's tool definitions are measured,
  whatever happens to its body: how many, their size in tokens (UTF-8
  bytes / 4 of each definition's JSON), a hash of the tool list (a new list
  breaks the prompt cache), and the tool names the model called, read from
  the buffered response or the stream (OpenAI chat and Responses,
  Anthropic, Google, Bedrock Converse). Only names and counts leave the
  edge, never schemas, descriptions or arguments; a name not shaped like
  a tool name (letters, digits, `_ . : -`, up to 64) and names past 64 per
  row count as "other". The dashboard shows each App's tool-definition
  tokens and their share of the prompt, the tools never called in the
  range, and what dropping them would have saved at the input and
  cache-read prices.
- **Prices.** Saved tokens are priced at the model's input price (an upper
  bound) and at its cache-read price, so the dashboard can show both next to
  the cache-read share.
- **Studio** merges the batches into hourly rollups in plugin KV, per edge
  and hour. Redelivered batches change nothing. Data older than 90 days is
  removed.
- **Bounds.** Studio refuses hours more than an hour ahead, keeps at most
  5,000 rows (and a 4 MB budget) per edge and hour, 64 strategy names and 8
  currencies per row, and 500 policy markers. Anything past a bound is added
  to an overflow row or an "other" entry, so totals stay right.

## Service scopes

| Scope | Why |
|-------|-----|
| `kv.readwrite` | Savings rollups and memoised results |
| `control.send` | Savings records from the edges to Studio |
| `pricing.read` | Model prices, including the cache-read price, to cost the savings |
| `apps.read` | The per-App policy override in App metadata; the App picker. Without it nothing sent through an App is compressed |
| `llms.read` | The LLM picker on the Policies page; the LLM's endpoint, which decides the request format |

## Building

```bash
cd community/plugins/prompt-compressor
go build -o prompt-compressor
make test   # go vet and go test -race
```

## Registering on the dev stack

1. Start the full stack from the AI Studio repo root: `make dev-full`. The
   plugin watcher builds every plugin module into `/app/bin/plugins/`.
2. Open the Admin UI at http://localhost:3000 and go to Admin > Plugins >
   Register Plugin.
3. Use the command `file:///app/bin/plugins/prompt-compressor` and approve the
   requested scopes.
4. Attach the plugin to an LLM, so the Microgateway loads it.

The watcher rebuilds the plugin when a `.go` file changes. Reload it with
`curl -X POST http://localhost:8080/api/v1/plugins/{id}/reload`.
