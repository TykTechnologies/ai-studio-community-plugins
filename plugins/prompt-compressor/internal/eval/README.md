# Prompt Compressor eval harness

`go run ./cmd/pc-eval` from the plugin directory.

## Offline replay (no keys, no network)

```bash
go run ./cmd/pc-eval replay -levels 0,1,2 -out /tmp/pc-report     # the plugin's test corpus
go run ./cmd/pc-eval replay -bodies ./captured -levels 0,1         # your own bodies
go run ./cmd/pc-eval replay -prices my-prices.json -output-tokens 600
go run ./cmd/pc-eval replay -policy mask8.json -levels 0,3        # {"mask_after_turns": 8}
```

`-policy` takes one policy as the plugin config holds it. The plugin's config
parser validates it, unset fields keep the defaults, and `-levels` sets the
level. `pc-eval compress` takes it too.

`-bodies` reads `<dir>/<format>/<name>.json`, one request body per file.
`<format>` is the request filter format: `openai`, `anthropic`, `bedrock`,
`google_ai`, `vertex`, `ollama` or `openai_responses`. The bodies never leave
the machine.

`report.json` and `report.md` give, for each level:

- **Token reduction per tool kind**: the tool-result units of every body,
  before and after. A second table does the same for the last turn of each
  full-history session, which holds the session's whole tool mix at the
  sizes agents produce.
- **Session cost with cache**: each 30-turn session is billed the way the
  vendor would bill it, with the gateway's own formula
  (`models.ChatRecordCost`) at the `-prices` list prices. The defaults are
  illustrative list prices.
  - The cache model uses the prefix-stability property. Each turn reads from
    cache the longest prefix it shares byte for byte with the previous turn,
    above the vendor's minimum (1024 tokens).
  - OpenAI rounds cache reads to 128-token steps.
  - Anthropic and Bedrock cache only bodies with breakpoints, and bill the new
    part as a cache write.
  - The `Change` column compares with compression off. "Cost including cache
    falls" holds when it is negative.
- **Prefix breaks**: turns where compression changed text the previous turn
  already sent, or broke the shared prefix. Any break means lost cache hits.
  - The usual cause is output that is not a pure function of the unit text
    and the policy (R1). Level 3's stepped masking of old turns also shows
    here, by design.
- **Late compressions**: results compressed further as they leave the
  protected newest turns (`protect_recent_turns`, `-protect`). Each costs the
  cache that one result, once; they are expected from level 2 up.
- **Skip violations**: changed pass-through bodies, or bodies whose context is
  vendor-managed. Any violation is a bug.
- **Nondeterministic**: bodies that compressed differently on a second run.

By default the replay runs the plugin's compress package with the default
policy at each level, and counts tokens with its per-format estimator;
`-compressor identity` and `-tokens bytes` (bytes/4) are the fallbacks.

To see what one level does to one body (and what the gateway should forward
for it, for live checks):

```bash
go run ./cmd/pc-eval compress -in body.json -format anthropic -level 2 > out.json   # report on stderr
```

The `go test ./internal/...` run is the CI smoke. It replays the corpus with
toy compressors and checks that:

- a deterministic compressor saves money after cache;
- a non-deterministic one, or one that depends on context, is caught;
- the cost formula matches the gateway's.

## Live suites (paid)

The gate (eac.4) uses BFCL (≥1,000 cases, 1-point margin), long-context QA
(≥500, 2-point F1 margin) and SWE-bench Verified (200 tasks × 3 seeds,
5-point margin). Every task runs at compression off and at each level, with
the same tasks and seeds, through the Microgateway.

The verdict uses the paired difference with a seeded bootstrap 95% CI
(`PairedDiff`, `NonInferior`); resolve rates use Wilson intervals. A level
passes when the lower CI bound is above minus the margin.

```bash
go run ./cmd/pc-eval plan -suite swebench -tasks verified-ids.txt -n 200 -seed 7 -levels 1,2
```

`plan` selects the tasks reproducibly: the same seed and task list always
give the same tasks. It prints the run count, tokens, dollars and duration,
estimated from the suite's per-task assumptions in `live.go`. Replace those
assumptions with figures from a pilot run.

A paid run needs `-run -approved-by <who> -max-usd <cap>` with a cap at or
above the estimate. Get the approval before running.

The suite runners themselves arrive with eac.24.
