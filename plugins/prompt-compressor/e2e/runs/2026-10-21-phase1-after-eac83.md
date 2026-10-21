# Phase 1 end-to-end re-run after eac.83, 2026-10-21

The re-run of `e2e/phase1.sh` after the eac.83 fix. Native Bedrock streams
now run the response hooks.

- Stack: pcx, from clean exports of core efba0fdd (eac.83, eac.80),
  enterprise 813ca438 and community 44866e0.
- Script: `e2e/phase1.sh` as committed with this file.

## Result

**All 137 checks pass:** 136 in the log below, and the real-vendor check on a later read (see below).

- **Bedrock converse-stream** now carries `X-Tyk-Compression` (`skipped` in
  audit and at level 1, `applied` with Tokens-Saved 3,139 at level 2, equal
  to the library's estimate). It is recorded too: each App records 43 of 43
  requests, with input and cache-read tokens equal to the mock's.
- **The real-vendor row** printed FAIL in the log below, because only 2 of
  the 4 requests had been recorded when the script read the totals 75 s after
  sending. They all arrived with the edge's next flush: App 25 recorded 4
  requests, 33,272 input and 11,520 cache-read tokens, the vendors' figures
  exactly. The script now polls until the records arrive (up to
  `FLUSH_WAIT`, 240 s) instead of sleeping a fixed 75 s.
- **An earlier attempt of this run** failed 8 rows only because the mock
  vendor had been restarted on a directory that still held an earlier run's
  requests. The script now refuses a non-empty mock directory.

---

# Prompt Compressor Phase 1 end-to-end run 20261021165444

Studio http://localhost:18080, edge http://localhost:8081, mock http://host.docker.internal:18999. LLMs: openai 7 (pc-e2e-openai), anthropic 8 (pc-e2e-anthropic), bedrock 9 (pc-e2e-bedrock).
Apps: audit 22, enforce level 1 23, enforce level 2 24 (bound to policy e2e-l2).
Built from: core efba0fdd (eac.83, eac.80), enterprise 813ca438, community 44866e0 (plugin, corpus), e2e/phase1.sh from the worktree (empty mock dir check).

## 1. Audit mode (forwards the client's body byte for byte)

| Request | Route | HTTP | Stream | X-Tyk-Compression | Tokens-Saved | Policy | Expect | Result |
|---|---|---|---|---|---|---|---|---|
| anthropic_cargo_string | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | audit | 1 | ae3359f2aa56 | audit | ok |
| anthropic_jest_blocks_image | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | audit | 725 | ae3359f2aa56 | audit | ok |
| anthropic_cache_control | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| anthropic_thinking_gotest | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| anthropic_document_csv | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | audit | 1428 | ae3359f2aa56 | audit | ok |
| anthropic_context_management | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | audit | 4 | ae3359f2aa56 | audit | ok |
| anthropic_compaction | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| anthropic_mcp_result | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| anthropic_parallel_maven_kubectl | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | audit | 3 | ae3359f2aa56 | audit | ok |
| openai_pytest_developer | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| openai_parallel_k8s_diff | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | skipped | - | ae3359f2aa56 | audit | ok |
| openai_terraform_rg | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| openai_json_api_mcp | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| openai_gradle_docker | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | audit | 4 | ae3359f2aa56 | audit | ok |
| openai_csv_html | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| openai_jest_cargo_image | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | audit | 1 | ae3359f2aa56 | audit | ok |
| openai_gotest_ls | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | audit | 9 | ae3359f2aa56 | audit | ok |
| openai_maven_large | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | audit | 4 | ae3359f2aa56 | audit | ok |
| openai_diff_lockfiles | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| anthropic_rg_vendor | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| anthropic_html_app | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| openai_mcp_search_100 | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| bedrock_converse_cachepoint | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | audit | 3 | ae3359f2aa56 | audit | ok |
| bedrock_converse_json_result | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| bedrock_converse_terraform_pytest | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| bedrock_converse_docker_html_csv | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | audit | 2 | ae3359f2aa56 | audit | ok |
| bedrock_invoke_anthropic | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/invoke` | 200 | no | audit | - | ae3359f2aa56 | audit | ok |
| bedrock_invoke_mcp_jest | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/invoke` | 200 | no | audit | - | ae3359f2aa56 | audit | ok |
| bedrock_invoke_titan | `/llm/rest/pc-e2e-bedrock/model/amazon.titan-text-premier-v1:0/invoke` | 200 | no | skipped | - | ae3359f2aa56 | untouched | ok |
| bedrock_invoke_llama | `/llm/rest/pc-e2e-bedrock/model/meta.llama3-1-70b-instruct-v1:0/invoke` | 200 | no | skipped | - | ae3359f2aa56 | untouched | ok |
| responses_codex_shell | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | audit | 10 | ae3359f2aa56 | audit | ok |
| responses_string_input | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| responses_function_outputs | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | audit | 999 | ae3359f2aa56 | audit | ok |
| responses_compaction | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | audit | 4 | ae3359f2aa56 | audit | ok |
| responses_forged_items | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | skipped | - | ae3359f2aa56 | audit | ok |
| openai_input_embeddings | `/llm/rest/pc-e2e-openai/v1/embeddings` | 200 | no | skipped | - | ae3359f2aa56 | untouched | ok |
| openai_input_completions | `/llm/rest/pc-e2e-openai/v1/completions` | 200 | no | skipped | - | ae3359f2aa56 | untouched | ok |
| openai_pytest_developer (stream) | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | skipped | - | ae3359f2aa56 | audit | ok |
| openai_gradle_docker (stream) | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | audit | 4 | ae3359f2aa56 | audit | ok |
| anthropic_cargo_string (stream) | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | audit | 1 | ae3359f2aa56 | audit | ok |
| anthropic_mcp_result (stream) | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | skipped | - | ae3359f2aa56 | audit | ok |
| responses_function_outputs (stream) | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | audit | 999 | ae3359f2aa56 | audit | ok |
| bedrock_converse_terraform_pytest (stream) | `/llm/stream/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse-stream` | 200 | yes | skipped | - | ae3359f2aa56 | audit | ok |

## 2. Enforce, level 1 (forwards what pc-eval compress gives for the same body)

| Request | Route | HTTP | Stream | X-Tyk-Compression | Tokens-Saved | Policy | Expect | Result |
|---|---|---|---|---|---|---|---|---|
| anthropic_cargo_string | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 1 | ae3359f2aa56 | enforce:1 | ok |
| anthropic_jest_blocks_image | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 725 | ae3359f2aa56 | enforce:1 | ok |
| anthropic_cache_control | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| anthropic_thinking_gotest | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| anthropic_document_csv | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 1428 | ae3359f2aa56 | enforce:1 | ok |
| anthropic_context_management | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 4 | ae3359f2aa56 | enforce:1 | ok |
| anthropic_compaction | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| anthropic_mcp_result | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| anthropic_parallel_maven_kubectl | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | applied | 3 | ae3359f2aa56 | enforce:1 | ok |
| openai_pytest_developer | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| openai_parallel_k8s_diff | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| openai_terraform_rg | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| openai_json_api_mcp | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| openai_gradle_docker | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 4 | ae3359f2aa56 | enforce:1 | ok |
| openai_csv_html | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| openai_jest_cargo_image | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 1 | ae3359f2aa56 | enforce:1 | ok |
| openai_gotest_ls | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 9 | ae3359f2aa56 | enforce:1 | ok |
| openai_maven_large | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 4 | ae3359f2aa56 | enforce:1 | ok |
| openai_diff_lockfiles | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| anthropic_rg_vendor | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| anthropic_html_app | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| openai_mcp_search_100 | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| bedrock_converse_cachepoint | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | applied | 3 | ae3359f2aa56 | enforce:1 | ok |
| bedrock_converse_json_result | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| bedrock_converse_terraform_pytest | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| bedrock_converse_docker_html_csv | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | applied | 2 | ae3359f2aa56 | enforce:1 | ok |
| bedrock_invoke_anthropic | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/invoke` | 200 | no | applied | - | ae3359f2aa56 | enforce:1 | ok |
| bedrock_invoke_mcp_jest | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/invoke` | 200 | no | applied | - | ae3359f2aa56 | enforce:1 | ok |
| bedrock_invoke_titan | `/llm/rest/pc-e2e-bedrock/model/amazon.titan-text-premier-v1:0/invoke` | 200 | no | skipped | - | ae3359f2aa56 | untouched | ok |
| bedrock_invoke_llama | `/llm/rest/pc-e2e-bedrock/model/meta.llama3-1-70b-instruct-v1:0/invoke` | 200 | no | skipped | - | ae3359f2aa56 | untouched | ok |
| responses_codex_shell | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | applied | 10 | ae3359f2aa56 | enforce:1 | ok |
| responses_string_input | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| responses_function_outputs | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | applied | 999 | ae3359f2aa56 | enforce:1 | ok |
| responses_compaction | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | applied | 4 | ae3359f2aa56 | enforce:1 | ok |
| responses_forged_items | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| openai_input_embeddings | `/llm/rest/pc-e2e-openai/v1/embeddings` | 200 | no | skipped | - | ae3359f2aa56 | untouched | ok |
| openai_input_completions | `/llm/rest/pc-e2e-openai/v1/completions` | 200 | no | skipped | - | ae3359f2aa56 | untouched | ok |
| openai_pytest_developer (stream) | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| openai_gradle_docker (stream) | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | applied | 4 | ae3359f2aa56 | enforce:1 | ok |
| anthropic_cargo_string (stream) | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | applied | 1 | ae3359f2aa56 | enforce:1 | ok |
| anthropic_mcp_result (stream) | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | skipped | - | ae3359f2aa56 | enforce:1 | ok |
| responses_function_outputs (stream) | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | applied | 999 | ae3359f2aa56 | enforce:1 | ok |
| bedrock_converse_terraform_pytest (stream) | `/llm/stream/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse-stream` | 200 | yes | skipped | - | ae3359f2aa56 | enforce:1 | ok |

## 3. Enforce, level 2 (App bound to policy e2e-l2)

| Request | Route | HTTP | Stream | X-Tyk-Compression | Tokens-Saved | Policy | Expect | Result |
|---|---|---|---|---|---|---|---|---|
| anthropic_cargo_string | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 3411 | 513bcff397d5 | enforce:2 | ok |
| anthropic_jest_blocks_image | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 2520 | 513bcff397d5 | enforce:2 | ok |
| anthropic_cache_control | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 6176 | 513bcff397d5 | enforce:2 | ok |
| anthropic_thinking_gotest | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 2600 | 513bcff397d5 | enforce:2 | ok |
| anthropic_document_csv | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 1428 | 513bcff397d5 | enforce:2 | ok |
| anthropic_context_management | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 4 | 513bcff397d5 | enforce:2 | ok |
| anthropic_compaction | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | skipped | - | 513bcff397d5 | enforce:2 | ok |
| anthropic_mcp_result | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 2000 | 513bcff397d5 | enforce:2 | ok |
| anthropic_parallel_maven_kubectl | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | applied | 11028 | 513bcff397d5 | enforce:2 | ok |
| openai_pytest_developer | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 2217 | 513bcff397d5 | enforce:2 | ok |
| openai_parallel_k8s_diff | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | applied | 2097 | 513bcff397d5 | enforce:2 | ok |
| openai_terraform_rg | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | 513bcff397d5 | enforce:2 | ok |
| openai_json_api_mcp | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | 513bcff397d5 | enforce:2 | ok |
| openai_gradle_docker | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 13461 | 513bcff397d5 | enforce:2 | ok |
| openai_csv_html | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | skipped | - | 513bcff397d5 | enforce:2 | ok |
| openai_jest_cargo_image | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 4832 | 513bcff397d5 | enforce:2 | ok |
| openai_gotest_ls | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 6571 | 513bcff397d5 | enforce:2 | ok |
| openai_maven_large | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 13937 | 513bcff397d5 | enforce:2 | ok |
| openai_diff_lockfiles | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 3054 | 513bcff397d5 | enforce:2 | ok |
| anthropic_rg_vendor | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 2468 | 513bcff397d5 | enforce:2 | ok |
| anthropic_html_app | `/llm/rest/pc-e2e-anthropic/v1/messages` | 200 | no | applied | 5156 | 513bcff397d5 | enforce:2 | ok |
| openai_mcp_search_100 | `/llm/rest/pc-e2e-openai/v1/chat/completions` | 200 | no | applied | 13537 | 513bcff397d5 | enforce:2 | ok |
| bedrock_converse_cachepoint | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | applied | 6143 | 513bcff397d5 | enforce:2 | ok |
| bedrock_converse_json_result | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | skipped | - | 513bcff397d5 | enforce:2 | ok |
| bedrock_converse_terraform_pytest | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | applied | 3139 | 513bcff397d5 | enforce:2 | ok |
| bedrock_converse_docker_html_csv | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse` | 200 | no | applied | 3062 | 513bcff397d5 | enforce:2 | ok |
| bedrock_invoke_anthropic | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/invoke` | 200 | no | applied | 5928 | 513bcff397d5 | enforce:2 | ok |
| bedrock_invoke_mcp_jest | `/llm/rest/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/invoke` | 200 | no | applied | 2012 | 513bcff397d5 | enforce:2 | ok |
| bedrock_invoke_titan | `/llm/rest/pc-e2e-bedrock/model/amazon.titan-text-premier-v1:0/invoke` | 200 | no | skipped | - | 513bcff397d5 | untouched | ok |
| bedrock_invoke_llama | `/llm/rest/pc-e2e-bedrock/model/meta.llama3-1-70b-instruct-v1:0/invoke` | 200 | no | skipped | - | 513bcff397d5 | untouched | ok |
| responses_codex_shell | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | applied | 5990 | 513bcff397d5 | enforce:2 | ok |
| responses_string_input | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | skipped | - | 513bcff397d5 | enforce:2 | ok |
| responses_function_outputs | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | applied | 999 | 513bcff397d5 | enforce:2 | ok |
| responses_compaction | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | applied | 4 | 513bcff397d5 | enforce:2 | ok |
| responses_forged_items | `/llm/rest/pc-e2e-openai/v1/responses` | 200 | no | applied | 664 | 513bcff397d5 | enforce:2 | ok |
| openai_input_embeddings | `/llm/rest/pc-e2e-openai/v1/embeddings` | 200 | no | skipped | - | 513bcff397d5 | untouched | ok |
| openai_input_completions | `/llm/rest/pc-e2e-openai/v1/completions` | 200 | no | skipped | - | 513bcff397d5 | untouched | ok |
| openai_pytest_developer (stream) | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | applied | 2217 | 513bcff397d5 | enforce:2 | ok |
| openai_gradle_docker (stream) | `/llm/stream/pc-e2e-openai/v1/chat/completions` | 200 | yes | applied | 13461 | 513bcff397d5 | enforce:2 | ok |
| anthropic_cargo_string (stream) | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | applied | 3411 | 513bcff397d5 | enforce:2 | ok |
| anthropic_mcp_result (stream) | `/llm/stream/pc-e2e-anthropic/v1/messages` | 200 | yes | applied | 2000 | 513bcff397d5 | enforce:2 | ok |
| responses_function_outputs (stream) | `/llm/stream/pc-e2e-openai/v1/responses` | 200 | yes | applied | 999 | 513bcff397d5 | enforce:2 | ok |
| bedrock_converse_terraform_pytest (stream) | `/llm/stream/pc-e2e-bedrock/model/anthropic.claude-sonnet-4-5-20250929-v1:0/converse-stream` | 200 | yes | applied | 3139 | 513bcff397d5 | enforce:2 | ok |

## 4. Recorded usage against the vendor's (mock) usage

The mock reports input = upstream bytes / 4 and cached = input / 2 for bodies with a cache marker (or OpenAI bodies over 4096 bytes). Records reach Studio with the edge's next flush.

| App | Requests sent | recorded | Input tokens (mock) | recorded | Cache-read (mock) | recorded | Usage missing | Result |
|---|---|---|---|---|---|---|---|---|
| audit | 43 | 43 | 354959 | 354959 | 105671 | 105671 | 0 | ok |
| enforce-l1 | 43 | 43 | 350153 | 350153 | 104638 | 104638 | 0 | ok |
| enforce-l2 | 43 | 43 | 229129 | 229129 | 64345 | 64345 | 0 | ok |

## 5. /ai/ and /anthropic/ routes

Same body from the level-0 App (baseline) and the level-2 App; the upstream body is the vendor's own format.

| Route | LLM | HTTP (L0, L2) | X-Tyk-Compression (L2) | Upstream bytes L0 | L2 | Expect | Result |
|---|---|---|---|---|---|---|---|
| `/ai/pc-e2e-openai/v1/chat/completions` | openai | 200, 200 | applied | 47457 | 6020 | compressed | ok |
| `/ai/pc-e2e-anthropic/v1/chat/completions` | anthropic | 200, 200 | applied | 47400 | 5963 | compressed | ok |
| `/ai/pc-e2e-bedrock/v1/chat/completions` | bedrock | 200, 200 | - | 47272 | 47272 | untouched | ok |
| `/anthropic/pc-e2e-bedrock/v1/messages` | bedrock | 200, 200 | - | 49295 | 49295 | untouched | ok |

The /anthropic/ bridge serves Bedrock LLMs only (an Anthropic LLM gets 400 by design), so it has one row.

## 6. Advanced LLM Cache attached before the compressor

| Send | X-Cache-Status | X-Tyk-Compression | Upstream requests |
|---|---|---|---|
| 1 | MISS | applied | 1 |
| 2 | HIT | applied | 0 |

## 7. Real vendors (default policy, level 1, enforce)

| Request | HTTP | X-Tyk-Compression | Tokens-Saved | Vendor input tokens | Vendor cache-read |
|---|---|---|---|---|---|
| openai | 200 | skipped |  | 6317 | 5760 |
| openai-stream | 200 | skipped |  | 6317 | 5760 |
| anthropic | 200 | applied | 1 | 10319 | 0 |
| anthropic-stream | 200 | applied | 1 | 10319 | 0 |

Vendor usage over 4 requests: input 33272, cache-read 11520. Recorded: {"requests":2,"input_tokens":12634,"cache_read_tokens":11520,"usage_missing":0}. **FAIL**

**1 failing rows.**
