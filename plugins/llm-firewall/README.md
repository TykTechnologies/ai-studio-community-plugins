# LLM Firewall Plugin

A phrase-based prompt filtering plugin for Tyk AI Studio's Microgateway. This plugin detects disallowed phrases in incoming prompts and blocks requests that contain policy violations before they reach the LLM.

## Features

- **Phrase-based filtering** - Block requests containing specific phrases or patterns
- **Regex support** - Use regular expressions for complex pattern matching
- **Model-specific rules** - Apply different rules to different LLM models using glob patterns
- **Multi-vendor support** - Works with OpenAI (chat and Responses API), Anthropic, Google AI, Vertex, and Ollama
- **Configurable hook phase** - Run checks before or after authentication
- **Privacy-preserving** - Logs matched patterns only, not request content
- **Fail-open by default** - Allows requests through if their content cannot be inspected; `fail_closed` blocks them instead

## Installation

### Building from Source

```bash
cd community/plugins/llm-firewall
go build -o llm-firewall .
```

### Deploying to Microgateway

1. Place the `llm-firewall` binary in your plugins directory
2. Configure the plugin in your microgateway configuration or via the API
3. Restart the gateway (if using file-based configuration)

## Configuration

The plugin is configured via JSON. Configuration can be passed through the plugin config when registering the plugin with the microgateway.

### Configuration Schema

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `hook_phase` | string | `"pre_auth"` | When to run the check: `"pre_auth"` or `"post_auth"` |
| `block_message` | string | `"Request blocked by content policy"` | Error message returned to clients |
| `case_sensitive` | boolean | `false` | Whether pattern matching is case-sensitive |
| `fail_closed` | boolean | `false` | Block requests whose content cannot be inspected (see [Error Handling](#error-handling)) |
| `rules` | array | `[]` | List of firewall rules (see below) |

### Rule Structure

Each rule applies to models matching a glob pattern:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `model_pattern` | string | required | Glob pattern for model names (e.g., `"claude-*"`, `"gpt-4*"`, `"*"`) |
| `phrases` | array | required | List of phrases/patterns to block |
| `enabled` | boolean | `true` | Whether this rule is active |

### Phrase Structure

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pattern` | string | required | The phrase or regex pattern to block |
| `is_regex` | boolean | `false` | Whether the pattern is a regular expression |
| `description` | string | `""` | Optional description for documentation/logging |

## Example Configurations

### Basic Configuration

Block common jailbreak attempts across all models:

```json
{
  "hook_phase": "pre_auth",
  "block_message": "Your request contains content that violates our usage policy",
  "case_sensitive": false,
  "rules": [
    {
      "model_pattern": "*",
      "enabled": true,
      "phrases": [
        {
          "pattern": "jailbreak",
          "is_regex": false,
          "description": "Block jailbreak keyword"
        },
        {
          "pattern": "ignore (all )?previous instructions",
          "is_regex": true,
          "description": "Block prompt injection attempts"
        },
        {
          "pattern": "disregard your programming",
          "is_regex": false,
          "description": "Block instruction override attempts"
        }
      ]
    }
  ]
}
```

### Model-Specific Rules

Apply different rules to different model families:

```json
{
  "hook_phase": "pre_auth",
  "block_message": "Request blocked by content policy",
  "case_sensitive": false,
  "rules": [
    {
      "model_pattern": "*",
      "enabled": true,
      "phrases": [
        {
          "pattern": "jailbreak",
          "is_regex": false
        }
      ]
    },
    {
      "model_pattern": "claude-*",
      "enabled": true,
      "phrases": [
        {
          "pattern": "pretend you are",
          "is_regex": false,
          "description": "Block role-playing injection for Claude"
        },
        {
          "pattern": "you are now",
          "is_regex": false
        }
      ]
    },
    {
      "model_pattern": "gpt-4*",
      "enabled": true,
      "phrases": [
        {
          "pattern": "DAN mode",
          "is_regex": false,
          "description": "Block DAN jailbreak for GPT-4"
        },
        {
          "pattern": "Developer Mode",
          "is_regex": false
        }
      ]
    }
  ]
}
```

### Advanced Regex Patterns

Use regular expressions for complex matching:

```json
{
  "hook_phase": "pre_auth",
  "block_message": "Request blocked by security policy",
  "case_sensitive": false,
  "rules": [
    {
      "model_pattern": "*",
      "enabled": true,
      "phrases": [
        {
          "pattern": "ignore\\s+(all\\s+)?(previous|prior|above)\\s+(instructions|prompts|rules)",
          "is_regex": true,
          "description": "Block various prompt injection patterns"
        },
        {
          "pattern": "(sudo|admin|root)\\s+mode",
          "is_regex": true,
          "description": "Block privilege escalation attempts"
        },
        {
          "pattern": "\\b(hack|exploit|bypass)\\s+(the\\s+)?(system|filter|safety)",
          "is_regex": true,
          "description": "Block explicit bypass attempts"
        }
      ]
    }
  ]
}
```

## Model Pattern Syntax

The `model_pattern` field uses glob-style patterns:

| Pattern | Matches |
|---------|---------|
| `*` | All models |
| `claude-*` | `claude-3-opus`, `claude-3-sonnet`, `claude-3.5-sonnet`, etc. |
| `gpt-4*` | `gpt-4`, `gpt-4-turbo`, `gpt-4o`, `gpt-4o-mini`, etc. |
| `gpt-4o-*` | `gpt-4o-mini`, `gpt-4o-2024-08-06`, etc. |
| `gemini-*` | `gemini-pro`, `gemini-1.5-pro`, etc. |
| `anthropic.claude-*` | Bedrock model IDs such as `anthropic.claude-3-haiku-20240307-v1:0` |
| `*claude*` | Bedrock model IDs, inference profiles (`us.anthropic.claude-...`) and their ARNs |

The model is read the vendor's way: the body's `model`, the `models/{model}:` path segment for Google AI and Vertex, and for Bedrock (since 1.2.0) the `/model/{modelId}/` path segment, unescaped, so an escaped ARN or inference profile ID matches as written. A request that names no model is matched by its LLM's slug.

## Hook Phases

### PreAuth (Default)

- Runs **before** authentication
- Blocks malicious requests early, saving resources
- Cannot access authenticated user context
- Best for: General content filtering, DDoS protection

### PostAuth

- Runs **after** authentication
- Has access to authenticated user/app context
- Useful for user-specific rules
- Best for: User-specific policies, audit logging with user context

To use PostAuth, set `"hook_phase": "post_auth"` in your configuration. The plugin checks in that one phase only (since 1.2.0; before, it checked every request in both phases whatever `hook_phase` said). An unknown value falls back to `pre_auth`.

In `post_auth` the firewall sees the body as the `post_auth` plugins before it in the LLM's plugin order left it. Put it before any plugin that rewrites the prompt, such as the Prompt Compressor, so it screens what the client sent. In `pre_auth` it always does.

## Supported LLM Vendors

The plugin inspects every string under the fields that carry prompt text, whatever the vendor or API shape:

| Field | Used by |
|-------|---------|
| `messages` | OpenAI and Ollama chat, Anthropic Messages, Bedrock Converse |
| `system` | Anthropic, Bedrock Converse |
| `systemInstruction`, `system_instruction` | Google AI and Vertex (both spellings the REST API accepts) |
| `contents` | Google AI and Vertex |
| `input`, `instructions` | OpenAI Responses API |
| `prompt` | Legacy completions |
| `tools`, `functions`, `tool_config`, `toolConfig` | Tool definitions and settings (descriptions the model reads), including Bedrock Converse `toolConfig` |

Inside these fields it reads all nested text: content blocks of any type (`text`, `input_text`, …), tool results, tool call arguments, tool messages, documents, and tool descriptions. Everything inside caller-chosen payloads (tool call `input`/`arguments`, function `args`/`response`, tool `parameters`) is read in full. Elsewhere it skips inline media (`image_url`, `inline_data`/`inlineData`, `file_data`, `input_audio`) and message or block metadata (`role`, `type`, IDs, MIME types, `detail`, `cache_control`). The data of a base64 media source is skipped, except for `text/*` documents, which are decoded (up to 1 MB) and inspected. Likewise the bytes of a Bedrock Converse image, video or document are skipped, except for documents in a text format (`txt`, `md`, `csv`, `html`), which are decoded (up to 1 MB) and inspected along with the document's `name` and `context` (since 1.2.0; before, the base64 was matched as it was). Each message, or other element of these fields, is matched as one text, with its strings joined by spaces.

## Error Handling

By default the plugin follows a **fail-open** design:

- If the request body cannot be parsed, or no text content is found in it → Request is **allowed** (with `fail_closed: true`: **blocked** with the normal block response, when an enabled rule applies to the model, and logged with the reason)
- If no rules match the model → Request is **allowed** (also with `fail_closed`)
- If invalid regex in config → Pattern is **skipped** (logged at startup)

Fail-open ensures that configuration errors or edge cases don't accidentally block legitimate traffic. Use `fail_closed` when an uninspected prompt is worse than a blocked one, for example when clients could send content in a format the plugin does not parse.

## Logging

When a request is blocked, the plugin logs:

- Model name
- Matched pattern (not the actual content - for privacy)
- Pattern description (if provided)
- Request ID

Example log output:
```
WARN: Request blocked by firewall model=claude-3-opus matched_pattern="ignore previous instructions" description="Block prompt injection" request_id=abc123
```

## Response Format

Blocked requests receive a `403 Forbidden` response:

```json
{
  "error": {
    "message": "Request blocked by content policy",
    "type": "content_policy_violation"
  }
}
```

The message text is configurable via `block_message`.

## Best Practices

1. **Start with broad rules** - Use `"model_pattern": "*"` for common patterns
2. **Add model-specific rules** - Layer on vendor-specific jailbreak patterns
3. **Use descriptions** - Document why each pattern exists for maintainability
4. **Test regex patterns** - Validate regex patterns before deployment
5. **Monitor logs** - Review blocked requests to tune rules
6. **Keep rules focused** - Avoid overly broad patterns that cause false positives

## Limitations

- **Request-only** - Does not analyze LLM responses
- **Text content only** - Does not analyze images or other media
- **No semantic analysis** - Pattern matching only, not intent detection
- **Single match exit** - Stops checking after first match (for performance)

## Plugin Order

An LLM's plugins run in one order, the order of the LLM's plugin list. `pre_auth` plugins run before any `post_auth` plugin, and each `post_auth` plugin gets the body the previous one returned. A `post_auth` block (a cache hit, a 429, a firewall block) answers at once: later plugins, request filters and every response hook are skipped for that request. On `/ai/`, `/v1/`, routers and `/anthropic/` the chain runs on the gateway's internal `/llm/call/` hop, once per failover attempt, on the vendor's native body.

Recommended `post_auth` order: LLM Firewall (when it runs in `post_auth`), response caches, Rate Limiter, Prompt Compressor, LLM Load Balancer.

- **The firewall screens the client's body.** In `pre_auth`, the default, it runs before every `post_auth` plugin, so prompt compression can neither hide a phrase (by dropping lines, cutting long output or masking turns) nor create one. In `post_auth`, put it before the Prompt Compressor.
- Request filters and guardrails run after the `post_auth` chain and screen the body as it is forwarded. The compressor's lightest level strips terminal escape codes, which can join a phrase that they split in the forwarded body; the firewall does not see that form.
- Microgateway edges managed by AI Studio follow the order set on the LLM from AI Studio 2.3. Older edges run an LLM's plugins in the order the plugins were installed.

## License

This plugin is part of Tyk AI Studio and is released under the same license terms.
