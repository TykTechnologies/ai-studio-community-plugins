#!/usr/bin/env bash
# Prompt Compressor Phase 1 end-to-end run (eac.15) against a running AI
# Studio + Microgateway, with e2e/mockupstream as the vendor. Writes a run
# log (Markdown) and the raw evidence to $OUT.
#
# Needs: curl, jq, go; the plugin registered in Studio, scopes approved and
# loaded on the edge; mockupstream running where the gateway can reach it.
#
#   go run ./e2e/mockupstream -dir /tmp/pc-mock &   # 127.0.0.1:18999
#   STUDIO=http://localhost:8080 EDGE=http://localhost:8081 API_KEY=... \
#   PLUGIN_ID=1 MOCK_URL=http://host.docker.internal:18999 MOCK_DIR=/tmp/pc-mock \
#   OUT=/tmp/pc-e2e e2e/phase1.sh
#
# Optional: OPENAI_API_KEY / ANTHROPIC_API_KEY for one real-vendor request
# each (buffered and streaming, a few hundred tokens), CACHE_PLUGIN_ID to
# check the order with Advanced LLM Cache attached before the compressor.
#
# The plugin's config is replaced for the run and restored at the end.
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
PLUGIN_DIR=$(cd "$HERE/.." && pwd)
: "${STUDIO:?}" "${EDGE:?}" "${API_KEY:?}" "${PLUGIN_ID:?}" "${MOCK_URL:?}" "${MOCK_DIR:?}" "${OUT:?}"
SETTLE=${SETTLE:-20}
# The mock numbers requests from 1 per process, and streams and the /ai/
# routes are matched by count: start it on an empty -dir for every run.
if [ -n "$(find "$MOCK_DIR" -name '*.body' 2>/dev/null | head -1)" ]; then
  echo "MOCK_DIR $MOCK_DIR already holds requests: restart mockupstream with an empty -dir"; exit 1
fi
RUN=${RUN:-$(date -u +%Y%m%d%H%M%S)}
mkdir -p "$OUT/req" "$OUT/resp"
LOG=$OUT/run-log.md
CORPUS=$PLUGIN_DIR/testdata/corpus

api() { # method path [json]
  if [ $# -ge 3 ]; then
    curl -sS -X "$1" -H "Authorization: Bearer $API_KEY" -H 'Content-Type: application/json' -d "$3" "$STUDIO$2"
  else
    curl -sS -X "$1" -H "Authorization: Bearer $API_KEY" "$STUDIO$2"
  fi
}
log() { printf '%s\n' "$*" >> "$LOG"; }
PCE=$OUT/pc-eval
(cd "$PLUGIN_DIR" && go build -o "$PCE" ./cmd/pc-eval) || { echo "build pc-eval failed"; exit 1; }

# ---- setup: LLMs (mock), Apps, config
ensure_llm() { # name vendor model endpoint [metadata-json] [plugin-ids] [api-key]
  local id attrs
  id=$(api GET '/api/v1/llms?all=true' | jq -r --arg n "$1" '.data[] | select(.attributes.name==$n) | .id' | head -1)
  attrs=$(jq -nc --arg n "$1" --arg v "$2" --arg m "$3" --arg e "$4" --argjson md "${5:-null}" --arg k "${7:-not-a-real-key}" \
    '{name:$n, vendor:$v, api_endpoint:$e, api_key:$k, default_model:$m, active:true, privacy_score:50} + (if $md then {metadata:$md} else {} end)')
  if [ -z "$id" ]; then
    id=$(api POST /api/v1/llms "{\"data\":{\"type\":\"llms\",\"attributes\":$attrs}}" | jq -r '.data.id')
  else
    api PATCH "/api/v1/llms/$id" "{\"data\":{\"type\":\"llms\",\"id\":\"$id\",\"attributes\":$attrs}}" > /dev/null
  fi
  api PUT "/api/v1/llms/$id/plugins" "{\"plugin_ids\":[${6:-$PLUGIN_ID}]}" > /dev/null
  echo "$id"
}
slug() { api GET "/api/v1/llms/$1" | jq -r '.data.attributes.slug // .data.attributes.name'; }
L_OA=$(ensure_llm pc-e2e-openai openai gpt-4.1 "$MOCK_URL/v1")
L_AN=$(ensure_llm pc-e2e-anthropic anthropic claude-sonnet-4-5 "$MOCK_URL/v1")
L_BR=$(ensure_llm pc-e2e-bedrock bedrock anthropic.claude-sonnet-4-5-20250929-v1:0 "$MOCK_URL" \
  '{"aws_region":"us-east-1","aws_access_key_id":"AKIAEXAMPLEE2E000000","aws_secret_access_key":"not-a-real-secret"}')
S_OA=$(slug "$L_OA"); S_AN=$(slug "$L_AN"); S_BR=$(slug "$L_BR")
UID_=$(api GET /common/me | jq -r '.id // .data.id')
mkapp() { # name [llm-ids-json] -> "id secret"
  local id cid attrs
  attrs=$(jq -nc --arg n "$1" --argjson u "$UID_" --argjson l "${2:-[$L_OA,$L_AN,$L_BR]}" \
    '{name:$n, description:"Prompt Compressor e2e", user_id:$u, llm_ids:$l, is_active:true}')
  id=$(api POST /api/v1/apps "{\"data\":{\"type\":\"apps\",\"attributes\":$attrs}}" | jq -r '.data.id')
  api POST "/api/v1/apps/$id/activate-credential" > /dev/null
  cid=$(api GET "/api/v1/apps/$id" | jq -r '.data.attributes.credential_id')
  echo "$id $(api GET "/api/v1/credentials/$cid" | jq -r '.data.attributes.secret')"
}
read -r A_AUD K_AUD <<<"$(mkapp "pc-e2e-$RUN-audit")"
read -r A_E1 K_E1 <<<"$(mkapp "pc-e2e-$RUN-enforce-l1")"
read -r A_E2 K_E2 <<<"$(mkapp "pc-e2e-$RUN-enforce-l2")"
ORIG_CONFIG=$(api GET "/api/v1/plugins/$PLUGIN_ID" | jq -c '.data.attributes.config // {}')
restore() { api PATCH "/api/v1/plugins/$PLUGIN_ID" "{\"config\":$ORIG_CONFIG}" > /dev/null; api POST /api/v1/edges/reload-all > /dev/null; }
trap restore EXIT
setmode() {
  local cfg
  # In enforce runs the audit App is the level-0 baseline for section 5.
  cfg=$(jq -nc --arg m "$1" --arg b "app:$A_E2" --arg a "app:$A_AUD" '{mode:$m, allow_client_bypass:true, policies:{"e2e-l2":{level:2}, "e2e-l0":{level:0}},
    bindings: ({($b):"e2e-l2"} + (if $m == "enforce" then {($a):"e2e-l0"} else {} end))}')
  api PATCH "/api/v1/plugins/$PLUGIN_ID" "{\"config\":$cfg}" > /dev/null
  api POST /api/v1/edges/reload-all > /dev/null
  sleep "$SETTLE"
}

{
  echo "# Prompt Compressor Phase 1 end-to-end run $RUN"
  echo
  echo "Studio $STUDIO, edge $EDGE, mock $MOCK_URL. LLMs: openai $L_OA ($S_OA), anthropic $L_AN ($S_AN), bedrock $L_BR ($S_BR)."
  echo "Apps: audit $A_AUD, enforce level 1 $A_E1, enforce level 2 $A_E2 (bound to policy e2e-l2)."
  echo "Built from: ${RUN_SHAS:-$(cd "$PLUGIN_DIR" && git rev-parse --short HEAD 2>/dev/null || echo "unknown (set RUN_SHAS)")}."
  echo
} > "$LOG"

# ---- one request: route, upstream body, headers
mock_count() { find "$MOCK_DIR" -name '*.body' | wc -l | tr -d ' '; }
N=0
FAILS=0
send() { # label key route format body-file expect(audit|enforce:<level>|untouched|any)
  local label=$1 key=$2 route=$3 format=$4 bodyf=$5 expect=$6 h r code n up want verdict stream
  N=$((N + 1))
  h=$OUT/resp/$N.h; r=$OUT/resp/$N.body
  local before after
  before=$(mock_count)
  code=$(curl -sS -o "$r" -D "$h" -w '%{http_code}' -X POST "$EDGE$route" -H "Authorization: Bearer $key" \
    -H 'Content-Type: application/json' -H 'anthropic-version: 2023-06-01' --data-binary "@$bodyf")
  after=$(mock_count)
  n=$(grep -i '^x-echo-request-n:' "$h" | cut -d' ' -f2 | tr -d '\r')
  # Native Bedrock streams relay no upstream headers: the one new mock
  # request is this one.
  [ -z "$n" ] && [ "$after" = $((before + 1)) ] && n=$after
  up=$MOCK_DIR/$(printf %04d "${n:-0}").body
  local hc hs hp
  hc=$(grep -i '^x-tyk-compression:' "$h" | cut -d' ' -f2- | tr -d '\r')
  hs=$(grep -i '^x-tyk-compression-tokens-saved:' "$h" | cut -d' ' -f2- | tr -d '\r')
  hp=$(grep -i '^x-tyk-compression-policy:' "$h" | cut -d' ' -f2- | tr -d '\r')
  stream=no; grep -qi '^content-type: text/event-stream\|^content-type: application/vnd.amazon.eventstream' "$h" && stream=yes
  verdict=ok
  if [ -z "$n" ] || [ ! -f "$up" ]; then
    verdict="FAIL (no upstream request)"
  else
    case $expect in
      audit|untouched)
        cmp -s "$bodyf" "$up" || verdict="FAIL (upstream differs from client)";;
      enforce:*)
        want=$OUT/req/$N.expected
        "$PCE" compress -format "$format" -level "${expect#enforce:}" -in "$bodyf" -out "$want" 2> "$want.rep"
        if ! cmp -s "$want" "$up"; then verdict="FAIL (upstream differs from pc-eval compress)"
        else
          local es; es=$(jq -r '.tokens_before - .tokens_after' "$want.rep")
          [ "$es" -gt 0 ] && [ "${hs:-0}" != "$es" ] && verdict="FAIL (Tokens-Saved $hs, library $es)"
        fi;;
    esac
  fi
  case $verdict in FAIL*) FAILS=$((FAILS + 1));; esac
  printf '| %s | `%s` | %s | %s | %s | %s | %s | %s | %s |\n' "$label" "$route" "$code" "$stream" "${hc:--}" "${hs:--}" "${hp:--}" "$expect" "$verdict" >> "$LOG"
  printf '%s\t%s\t%s\t%s\t%s\n' "$N" "${n:-0}" "$format" "$key" "$label" >> "$OUT/requests.tsv"
}
table() { log ""; log "| Request | Route | HTTP | Stream | X-Tyk-Compression | Tokens-Saved | Policy | Expect | Result |"; log "|---|---|---|---|---|---|---|---|---|"; }

# Corpus bodies per format, with the gateway route for their path; bodies
# that stream go to /llm/stream.
corpus_pass() { # key expect
  local key=$1 expect=$2 e f fmt path s route
  while read -r e; do
    f=$CORPUS/$(jq -r .file <<<"$e"); fmt=$(jq -r .format <<<"$e"); path=$(jq -r .path <<<"$e")
    case $fmt in openai|openai_input|openai_responses) s=$S_OA;; anthropic) s=$S_AN;; bedrock) s=$S_BR;; esac
    route=/llm/rest/$s$path
    jq -e '.stream == true' "$f" > /dev/null 2>&1 && route=/llm/stream/$s$path
    local ex=$expect
    [ "$(jq -r .expect <<<"$e")" = skip ] && ex=untouched
    # The gateway resolves previous_response_id against the responses it
    # served and answers 404 for one it never issued.
    [ "$(jq -r .name <<<"$e")" = responses_previous_response_id ] && continue
    send "$(jq -r .name <<<"$e")" "$key" "$route" "$fmt" "$f" "$ex"
  done < <(jq -c '.entries[] | select(.format=="openai" or .format=="anthropic" or .format=="bedrock" or .format=="openai_responses" or .format=="openai_input") | {name, file, format, path, expect}' "$CORPUS/manifest.json")
}
# Streaming variants of corpus bodies (stream:true added, include_usage for
# OpenAI chat; Bedrock Converse on converse-stream).
stream_pass() { # key expect
  local key=$1 expect=$2 name fmt path route f out
  for name in openai_pytest_developer openai_gradle_docker anthropic_cargo_string anthropic_mcp_result responses_function_outputs bedrock_converse_terraform_pytest; do
    f=$(ls "$CORPUS"/bodies/*/"$name".json); fmt=$(jq -r --arg n "$name" '.entries[] | select(.name==$n) | .format' "$CORPUS/manifest.json")
    path=$(jq -r --arg n "$name" '.entries[] | select(.name==$n) | .path' "$CORPUS/manifest.json")
    out=$OUT/req/stream-$name-$N.json
    case $fmt in
      openai) jq -c '. + {stream:true, stream_options:{include_usage:true}}' "$f" > "$out"; route=/llm/stream/$S_OA$path;;
      openai_responses) jq -c '. + {stream:true}' "$f" > "$out"; route=/llm/stream/$S_OA$path;;
      anthropic) jq -c '. + {stream:true}' "$f" > "$out"; route=/llm/stream/$S_AN$path;;
      bedrock) cp "$f" "$out"; route=/llm/stream/$S_BR${path%/converse}/converse-stream;;
    esac
    send "$name (stream)" "$key" "$route" "$fmt" "$out" "$expect"
  done
}

log "## 1. Audit mode (forwards the client's body byte for byte)"
setmode audit; table
corpus_pass "$K_AUD" audit
stream_pass "$K_AUD" audit

log ""
log "## 2. Enforce, level 1 (forwards what pc-eval compress gives for the same body)"
setmode enforce; table
corpus_pass "$K_E1" enforce:1
stream_pass "$K_E1" enforce:1

log ""
log "## 3. Enforce, level 2 (App bound to policy e2e-l2)"
table
corpus_pass "$K_E2" enforce:2
stream_pass "$K_E2" enforce:2

# ---- usage: what the plugin recorded against what the mock reported
log ""
log "## 4. Recorded usage against the vendor's (mock) usage"
log ""
log "The mock reports input = upstream bytes / 4 and cached = input / 2 for bodies with a cache marker (or OpenAI bodies over 4096 bytes). Records reach Studio with the edge's next flush."
sleep "${FLUSH_WAIT:-75}"
log ""
log "| App | Requests sent | recorded | Input tokens (mock) | recorded | Cache-read (mock) | recorded | Usage missing | Result |"
log "|---|---|---|---|---|---|---|---|---|"
for pair in "audit $A_AUD $K_AUD" "enforce-l1 $A_E1 $K_E1" "enforce-l2 $A_E2 $K_E2"; do
  read -r name app key <<<"$pair"
  sent=0; inp=0; cached=0
  while IFS=$'\t' read -r _ n fmt k _; do
    [ "$k" = "$key" ] || continue
    b=$MOCK_DIR/$(printf %04d "$n").body; [ -f "$b" ] || continue
    sent=$((sent + 1)); sz=$(wc -c < "$b" | tr -d ' '); i=$((sz / 4)); inp=$((inp + i))
    if grep -q '"cache_control"\|"cachePoint"' "$b" || { case $fmt in openai*) [ "$sz" -gt 4096 ];; *) false;; esac; }; then cached=$((cached + i / 2)); fi
  done < "$OUT/requests.tsv"
  got=$(api POST "/api/v1/plugins/$PLUGIN_ID/rpc/getSavings" "{\"range_hours\":24,\"app_id\":$app}" | jq -c '(.data // .).totals')
  echo "$got" > "$OUT/savings-$name.json"
  rr=$(jq -r .requests <<<"$got"); ri=$(jq -r .input_tokens <<<"$got"); rc=$(jq -r .cache_read_tokens <<<"$got"); um=$(jq -r .usage_missing <<<"$got")
  verdict=ok; { [ "$rr" = "$sent" ] && [ "$ri" = "$inp" ] && [ "$rc" = "$cached" ] && [ "$um" = 0 ]; } || { verdict="FAIL"; FAILS=$((FAILS + 1)); }
  log "| $name | $sent | $rr | $inp | $ri | $cached | $rc | $um | $verdict |"
done

# ---- OpenAI-compatible and Anthropic-compatible routes. /ai/ reaches
# post_auth on the internal /llm/call hop for non-Bedrock LLMs; /ai/ and
# /anthropic/ to Bedrock call the vendor directly and must pass through
# uncompressed. Each body goes once from the level-0 App (baseline) and once
# from the level-2 App: smaller upstream means compressed.
log ""
log "## 5. /ai/ and /anthropic/ routes"
log ""
log "Same body from the level-0 App (baseline) and the level-2 App; the upstream body is the vendor's own format."
log ""
log "| Route | LLM | HTTP (L0, L2) | X-Tyk-Compression (L2) | Upstream bytes L0 | L2 | Expect | Result |"
log "|---|---|---|---|---|---|---|---|"
route_case() { # route label expect(compressed|untouched) body
  local route=$1 label=$2 expect=$3 bodyf=$4 c0 c2 h n s0 s2 hc verdict
  h=$OUT/resp/route-$label-$RANDOM.h
  c0=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$EDGE$route" -H "Authorization: Bearer $K_AUD" -H 'Content-Type: application/json' -H 'anthropic-version: 2023-06-01' --data-binary "@$bodyf")
  n=$(mock_count); s0=$(wc -c < "$MOCK_DIR/$(printf %04d "$n").body" | tr -d ' ')
  c2=$(curl -sS -o /dev/null -D "$h" -w '%{http_code}' -X POST "$EDGE$route" -H "Authorization: Bearer $K_E2" -H 'Content-Type: application/json' -H 'anthropic-version: 2023-06-01' --data-binary "@$bodyf")
  n=$(mock_count); s2=$(wc -c < "$MOCK_DIR/$(printf %04d "$n").body" | tr -d ' ')
  hc=$(grep -i '^x-tyk-compression:' "$h" | cut -d' ' -f2- | tr -d '\r')
  verdict=ok
  if [ "$c0" != 200 ] || [ "$c2" != 200 ]; then verdict="FAIL (HTTP)"
  elif [ "$expect" = compressed ] && [ "$s2" -ge "$s0" ]; then verdict="FAIL (not compressed)"
  elif [ "$expect" = untouched ] && [ "$s2" != "$s0" ]; then verdict="FAIL (changed on an uncovered path)"
  fi
  case $verdict in FAIL*) FAILS=$((FAILS + 1));; esac
  log "| \`$route\` | $label | $c0, $c2 | ${hc:--} | $s0 | $s2 | $expect | $verdict |"
}
AIB=$OUT/req/ai-body.json
jq -c '{model, messages, max_tokens: 64}' "$CORPUS/bodies/openai/openai_maven_large.json" > "$AIB"
ANB=$OUT/req/anthropic-route.json
jq -c '. + {stream: false}' "$CORPUS/bodies/anthropic/anthropic_parallel_maven_kubectl.json" > "$ANB"
route_case "/ai/$S_OA/v1/chat/completions" openai compressed "$AIB"
route_case "/ai/$S_AN/v1/chat/completions" anthropic compressed "$AIB"
route_case "/ai/$S_BR/v1/chat/completions" bedrock untouched "$AIB"
route_case "/anthropic/$S_BR/v1/messages" bedrock untouched "$ANB"
log ""
log "The /anthropic/ bridge serves Bedrock LLMs only (an Anthropic LLM gets 400 by design), so it has one row."

# ---- optional: Advanced LLM Cache before the compressor
if [ -n "${CACHE_PLUGIN_ID:-}" ]; then
  log ""
  log "## 6. Advanced LLM Cache attached before the compressor"
  L_CA=$(ensure_llm pc-e2e-openai-cached openai gpt-4.1 "$MOCK_URL/v1" null "$CACHE_PLUGIN_ID,$PLUGIN_ID")
  api PATCH "/api/v1/apps/$A_E2" "$(api GET "/api/v1/apps/$A_E2" | jq -c --argjson c "$L_CA" '{data:{type:"apps",id:(.data.id|tostring),attributes:(.data.attributes | {name, description, user_id, is_active, llm_ids: ((.llm_ids // []) + [$c])})}}')" > /dev/null
  api POST /api/v1/edges/reload-all > /dev/null; sleep "$SETTLE"
  CB=$CORPUS/bodies/openai/openai_gradle_docker.json
  log ""
  log "| Send | X-Cache-Status | X-Tyk-Compression | Upstream requests |"
  log "|---|---|---|---|"
  for i in 1 2; do
    before=$(mock_count)
    h=$OUT/resp/cache-$i.h
    curl -sS -o /dev/null -D "$h" -X POST "$EDGE/llm/rest/$(slug "$L_CA")/v1/chat/completions" -H "Authorization: Bearer $K_E2" -H 'Content-Type: application/json' --data-binary "@$CB"
    after=$(mock_count)
    log "| $i | $(grep -i '^x-cache-status:' "$h" | cut -d' ' -f2- | tr -d '\r') | $(grep -i '^x-tyk-compression:' "$h" | cut -d' ' -f2- | tr -d '\r') | $((after - before)) |"
  done
fi

# ---- optional: one real-vendor request per vendor, buffered and streaming
# (policy: the default, level 1 enforce), recorded usage against the usage
# the vendor returned to the client.
real_usage() { # response-file vendor -> input cached (the plugin's normalisation)
  local f=$1
  if grep -q '^data: ' "$f"; then
    sed -n 's/^data: //p' "$f" | grep -v '^\[DONE\]' | jq -s -r --arg v "$2" '
      if $v == "anthropic" then (map(select(.type=="message_start"))[0].message.usage | [(.input_tokens + (.cache_read_input_tokens // 0) + (.cache_creation_input_tokens // 0)), (.cache_read_input_tokens // 0)])
      else (map(select(.usage != null))[0].usage | [.prompt_tokens, (.prompt_tokens_details.cached_tokens // 0)]) end | @tsv'
  else
    jq -r --arg v "$2" '.usage | if $v == "anthropic" then [(.input_tokens + (.cache_read_input_tokens // 0) + (.cache_creation_input_tokens // 0)), (.cache_read_input_tokens // 0)]
      else [.prompt_tokens, (.prompt_tokens_details.cached_tokens // 0)] end | @tsv' "$f"
  fi
}
if [ -n "${OPENAI_API_KEY:-}${ANTHROPIC_API_KEY:-}" ]; then
  log ""
  log "## 7. Real vendors (default policy, level 1, enforce)"
  ids="[]"
  if [ -n "${OPENAI_API_KEY:-}" ]; then
    L_ROA=$(ensure_llm pc-e2e-openai-real openai "${OPENAI_MODEL:-gpt-4.1}" https://api.openai.com/v1 null "$PLUGIN_ID" "$OPENAI_API_KEY"); ids=$(jq -c --argjson i "$L_ROA" '. + [$i]' <<<"$ids")
  fi
  if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
    L_RAN=$(ensure_llm pc-e2e-anthropic-real anthropic "${ANTHROPIC_MODEL:-claude-sonnet-4-5}" https://api.anthropic.com/v1 null "$PLUGIN_ID" "$ANTHROPIC_API_KEY"); ids=$(jq -c --argjson i "$L_RAN" '. + [$i]' <<<"$ids")
  fi
  read -r A_RE K_RE <<<"$(mkapp "pc-e2e-$RUN-real" "$ids")"
  api POST /api/v1/edges/reload-all > /dev/null; sleep "$SETTLE"
  log ""
  log "| Request | HTTP | X-Tyk-Compression | Tokens-Saved | Vendor input tokens | Vendor cache-read |"
  log "|---|---|---|---|---|---|"
  vin=0; vcr=0; vn=0
  real() { # label route vendor body
    local h=$OUT/resp/real-$1.h r=$OUT/resp/real-$1.body code u
    code=$(curl -sS -o "$r" -D "$h" -w '%{http_code}' -X POST "$EDGE$2" -H "Authorization: Bearer $K_RE" -H 'Content-Type: application/json' -H 'anthropic-version: 2023-06-01' --data-binary "@$4")
    u=$(real_usage "$r" "$3" 2>/dev/null)
    [ "$code" = 200 ] && [ -n "$u" ] && { vn=$((vn + 1)); vin=$((vin + $(cut -f1 <<<"$u"))); vcr=$((vcr + $(cut -f2 <<<"$u"))); }
    [ "$code" = 200 ] || FAILS=$((FAILS + 1))
    log "| $1 | $code | $(grep -i '^x-tyk-compression:' "$h" | cut -d' ' -f2- | tr -d '\r') | $(grep -i '^x-tyk-compression-tokens-saved:' "$h" | cut -d' ' -f2- | tr -d '\r') | $(cut -f1 <<<"$u") | $(cut -f2 <<<"$u") |"
  }
  if [ -n "${L_ROA:-}" ]; then
    jq -c --arg m "${OPENAI_MODEL:-gpt-4.1}" '.model = $m | del(.max_tokens, .temperature, .top_p) | .max_completion_tokens = 256' "$CORPUS/bodies/openai/openai_pytest_developer.json" > "$OUT/req/real-openai.json"
    jq -c '. + {stream: true, stream_options: {include_usage: true}}' "$OUT/req/real-openai.json" > "$OUT/req/real-openai-stream.json"
    real openai "/llm/rest/$(slug "$L_ROA")/v1/chat/completions" openai "$OUT/req/real-openai.json"
    real openai-stream "/llm/stream/$(slug "$L_ROA")/v1/chat/completions" openai "$OUT/req/real-openai-stream.json"
  fi
  if [ -n "${L_RAN:-}" ]; then
    jq -c --arg m "${ANTHROPIC_MODEL:-claude-sonnet-4-5}" '.model = $m | .max_tokens = 64 | del(.stream, .temperature, .top_p)' "$CORPUS/bodies/anthropic/anthropic_cargo_string.json" > "$OUT/req/real-anthropic.json"
    jq -c '. + {stream: true}' "$OUT/req/real-anthropic.json" > "$OUT/req/real-anthropic-stream.json"
    real anthropic "/llm/rest/$(slug "$L_RAN")/v1/messages" anthropic "$OUT/req/real-anthropic.json"
    real anthropic-stream "/llm/stream/$(slug "$L_RAN")/v1/messages" anthropic "$OUT/req/real-anthropic-stream.json"
  fi
  sleep "${FLUSH_WAIT:-75}"
  got=$(api POST "/api/v1/plugins/$PLUGIN_ID/rpc/getSavings" "{\"range_hours\":24,\"app_id\":$A_RE}" | jq -c '(.data // .).totals | {requests, input_tokens, cache_read_tokens, usage_missing}')
  verdict=ok; { [ "$(jq -r .requests <<<"$got")" = "$vn" ] && [ "$(jq -r .input_tokens <<<"$got")" = "$vin" ] && [ "$(jq -r .cache_read_tokens <<<"$got")" = "$vcr" ]; } || { verdict=FAIL; FAILS=$((FAILS + 1)); }
  log ""
  log "Vendor usage over $vn requests: input $vin, cache-read $vcr. Recorded: $got. **$verdict**"
fi

log ""
log "**$FAILS failing rows.**"
echo "$LOG ($FAILS failing rows)"
