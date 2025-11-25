# LLM Firewall Plugin

A phrase-based prompt filtering plugin for Tyk AI Studio's Microgateway. This plugin detects disallowed phrases in incoming prompts and blocks requests that contain policy violations before they reach the LLM.

## Features

- **Phrase-based filtering** - Block requests containing specific phrases or patterns
- **Regex support** - Use regular expressions for complex pattern matching
- **Model-specific rules** - Apply different rules to different LLM models using glob patterns
- **Multi-vendor support** - Works with OpenAI, Anthropic, Google AI, Vertex, and Ollama
- **Configurable hook phase** - Run checks before or after authentication
- **Privacy-preserving** - Logs matched patterns only, not request content
- **Fail-open design** - Allows requests through if parsing fails (configurable)

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

To use PostAuth, set `"hook_phase": "post_auth"` in your configuration.

## Supported LLM Vendors

The plugin automatically detects and parses request formats for:

| Vendor | Request Format |
|--------|----------------|
| OpenAI | `messages` array with `role` and `content` |
| Anthropic | `system` field + `messages` array (supports content blocks) |
| Google AI | `systemInstruction` + `contents` array with `parts` |
| Vertex | Same as Google AI |
| Ollama | Same as OpenAI |

Unknown vendors fall back to OpenAI format parsing.

## Error Handling

The plugin follows a **fail-open** design:

- If request body cannot be parsed → Request is **allowed**
- If no rules match the model → Request is **allowed**
- If invalid regex in config → Pattern is **skipped** (logged at startup)
- If vendor unknown → Falls back to OpenAI parsing

This ensures that configuration errors or edge cases don't accidentally block legitimate traffic.

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

## License

This plugin is part of Tyk AI Studio and is released under the same license terms.
