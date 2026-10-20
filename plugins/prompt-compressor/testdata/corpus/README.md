# Prompt Compressor test corpus

Request bodies shaped like what real clients send to the AI Gateway, in
every vendor format the gateway proxies, carrying the tool output the
compressor works on. Every compressor test, the offline eval replay and the
cache-stability test run against these files.

## Provenance

**Everything here is synthetic.** It holds no customer data, no real
credentials and no captured traffic.

- `internal/corpus` generates the files from code (`go generate ./internal/corpus`).
- Each entry gets its own random source, seeded from its name, so the output
  is the same byte for byte on every run.
  `TestCorpusMatchesGenerator` fails if the committed files and the generator
  ever differ.
- The content describes one invented company, "Acme", and its "ledger"
  product. It spans a Go API, a Python worker, a TypeScript web app, a Rust
  parser crate and a Java reporting service.
- Hosts use `example.com` / `example.internal`, people are `@example.com`,
  IP addresses are private ranges, and the AWS account is `000000000000`.
- Signatures, `encrypted_content`, `redacted_thinking` data and image bytes
  are random bytes. They are opaque on purpose and decode to nothing.
- Tool output is modelled on the public output formats of each tool:
  - pytest 8, jest 29, `go test -v` and cargo test;
  - Maven 3 and Gradle 8;
  - BuildKit `--progress=plain`;
  - kubectl, terraform plan, git diff, ripgrep, `ls -la`, find and tree;
  - a GitHub-style issues API and MCP 2025-06-18 `tools/call` results.
- Vendor request shapes follow each vendor's public API reference as of
  2026-10:
  - OpenAI Chat Completions and Responses (including Codex CLI's
    `store:false` history, encrypted reasoning, wrapped shell output and
    `apply_patch`);
  - Anthropic Messages (cache_control, thinking, context_management,
    compaction);
  - Bedrock Converse and InvokeModel;
  - Gemini on Google AI and Vertex;
  - Ollama.

## Layout

| Path | What |
|---|---|
| `manifest.json` | Every entry and session with what tests may rely on (below) |
| `bodies/<format>/<name>.json` | One request body, exactly as the client sends it |
| `sessions/<name>.turns.gz` | A 30-turn agent session: every turn's full request body |

`format` is the request filter format the proxy resolves for the request:
`openai`, `anthropic`, `google_ai`, `vertex`, `bedrock`, `ollama`,
`openai_responses`, `openai_input`, `openai_decisions`. `path` is the
upstream path the client calls after `/llm/rest/<slug>`. It tells Bedrock
Converse from InvokeModel and separates the pass-through endpoints.

### Entry fields

- `expect`:
  - `extract`: `segments.ExtractUnits` reads the body, and the compressor
    may rewrite its tool output.
  - `skip`: the compressor must pass the body through untouched.
    `ExtractUnits` refuses it (an unsupported format, or a body not shaped
    like its format, such as Titan or Llama on the Bedrock InvokeModel path),
    and the compressor never falls back to `segments.Generic`.
- `pending`: the bead that adds the format's extractor, for entries in a
  format segments cannot read yet (none at present; OpenAI Responses was
  pending on eac.9). Until `segments.Supports` returns true for the format, the
  entry is read as a skip. After that the tests hold it to `extract`
  automatically.
- `tools`: each tool output the body carries. Fields:
  - `kind` (pytest, jest, gotest, cargo, maven, gradle, docker_build,
    kubectl, terraform_plan, git_diff, ripgrep, dir_listing, json_api, csv,
    html, mcp) and `variant`;
  - `marker`: a substring a test can find in the unit text;
  - `via`: `tool_result`, a `document` block, or `input` (prompt text of a
    pass-through body);
  - `bytes`: the output's size.
- `shapes`: structural features the body is there to exercise:
  - tool result shape: `tool_result_string`, `tool_result_blocks`,
    `tool_result_json`;
  - caching: `cache_control_block`, `cache_control_system`,
    `bedrock_cachepoint`;
  - vendor context handling: `context_management`, `compaction`;
  - opaque content: `thinking`, `redacted_thinking`, `encrypted_reasoning`,
    `thought_signature`, `image`, `base64_text_document`;
  - messages and serialisation: `developer_role`, `parallel_tool_calls`,
    `python_json` (`json.dumps` defaults: spaced separators,
    `ensure_ascii`), and others.

  `TestShapesPresent` checks each claim against the body.
- `cache_breakpoints`: the number of `cache_control` and `cachePoint`
  markers.
- `native_context_management`: the body asks the vendor to manage context
  itself, so the compressor skips it.
- `tool_pairing`:
  - `paired`: every call has exactly one result.
  - `previous_response`: the calls live in a stored response.

### Sessions

Each session has 30 turns, and turn *k* is the request the agent sends
after *k−1* tool rounds.

- `history: full`:
  - Each turn re-sends everything, so turn *k+1* is turn *k* plus new
    messages. `TestSessions` checks this, and top-level fields do not
    change between turns.
  - `anthropic_agent_30` behaves like a coding agent. It moves its last
    `cache_control` to the newest user message every turn
    (`moving_cache_control`), so consecutive turns also differ in that one
    marker. This is the case where the compressor must keep its output
    stable.
- `history: previous_response_id`:
  - Each turn sends only the new tool outputs and names the previous
    response.
  - `response_ids[k]` is the id of turn *k+1*'s response, so turn *k+2*
    names it.

Session files are gzip over one line per turn: the number of leading bytes
the turn shares with the previous turn, a tab, and the rest of the body.
Storing 30 full bodies would take about 20 MB; the prefix sharing brings
that down to a few hundred KiB. `corpus.Load` rebuilds the bodies. To read
them as files, run:

```bash
go run ./internal/corpus/cmd/corpusgen -explode /tmp/turns   # /tmp/turns/<session>/turn-NN.json
```

## Changing the corpus

Edit the generator in `internal/corpus`:

- `entries.go`: single bodies;
- `sessions.go`: sessions;
- `toolout.go`: tool output;
- `render.go`: vendor formats.

Then run `go generate ./internal/corpus` and commit the regenerated files
with the change. Never edit the JSON by hand; the generator test would fail.
