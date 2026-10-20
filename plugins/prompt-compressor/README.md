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
