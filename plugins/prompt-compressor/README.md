# Prompt Compressor Plugin

A community plugin for Tyk AI Studio that shrinks LLM request bodies on the
Microgateway before they reach the vendor, starting with tool outputs.

- **Cache-safe:** the output is a pure function of the text and the policy
  version, so a compressed prefix stays byte-identical turn after turn and the
  provider's prompt cache keeps hitting.
- **Measured:** it reports tokens and money saved next to the cache-read share.
- **Fails open:** any error passes the request through unchanged.

> **Status:** scaffold. This version loads on Studio and the Microgateway and
> passes every request through untouched. Compression, the config model and
> the dashboard land in later versions.

## Hooks

| Hook | Runtime | What it does |
|------|---------|--------------|
| `post_auth` | Microgateway | Compresses the request body (pass-through for now) |
| `on_response` | Microgateway | Reads usage, including cache-read tokens, and records the savings |
| `studio_ui` | Studio | Admin pages: policies, preview, savings dashboard |

`post_auth` runs only on the gateway's `/llm/` endpoints. Studio's
OpenAI-compatible endpoints (`/ai/{slug}/v1`, the unified `/v1` and
`/router/`) reach it through their internal `/llm/call/` hop for every vendor
except Bedrock, which they call directly.

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
  `default_policy`. A policy name that doesn't exist in App metadata, or a
  failed App lookup, falls back to the bindings.
- **Engine credentials** are top-level keys, `engine_<name>_url` and
  `engine_<name>_token`, so `$SECRET/` references work. External engines are
  not supported yet.
- **Policy hash:** every record carries the SHA-256 of the policy's effective
  settings and the strategy version, so a reviewer can tell which rules
  changed a request. Key order and settings left at their defaults don't
  change it; engine credentials are not part of it.

An invalid config is logged and turns compression off; it never stops the
plugin loading. The `validateConfig` RPC checks a config before it is saved.
The full schema is in `config.schema.json`.

## Service scopes

| Scope | Why |
|-------|-----|
| `kv.readwrite` | Savings rollups and memoised results |
| `control.send` | Savings records from the edges to Studio |
| `pricing.read` | Model prices, including the cache-read price, to cost the savings |
| `apps.read` | The per-App policy override in App metadata |

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
