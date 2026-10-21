# Prompt Compressor: evaluation of levels 1 and 2

Status as of 2026-10-21 (eac.24). This is the offline half of the evaluation
with the end-to-end checks. The paid suites the gate needs for level 2 are
planned and costed in the last section but **have not been run**.

## Recommendation

| Level | Status | Why |
|---|---|---|
| 0 (off) | available | the baseline everything is measured against |
| **1 (lossless)** | **default** | passes its gate (correctness, cost replay, smoke): 0 errors, 0 nondeterminism, never dearer than off on any session, byte-exact end to end. It saves little (0.4% of session cost) |
| **2 (reducers)** | **opt-in** until the paid suites pass | 11.4% lower session cost with no cache breaks, 35% fewer tokens in single requests, but it is lossy, and the gate's quality suites have not run |
| 3 | opt-in | the same session cost as level 2 with masking off (its default). `mask_after_turns` pays only on OpenAI Chat pricing and makes the Codex profile dearer than off |
| `dedupe_references` | off | not evaluated; it needs the same gate as level 2 |

A level-2 rollout should start in `audit` mode, as the docs page says.

## What was measured

**Offline replay** (`pc-eval replay`, `internal/eval`), on a clean archive of
community 4854efb (core tree 04d02357).

- **Bodies:** 44 single request bodies in seven vendor formats, plus 5 agent
  sessions of 30 turns each (Anthropic, OpenAI Chat, Bedrock Converse, Codex
  Responses with full history, and a chained Responses session). All synthetic
  (`testdata/corpus`).
- **Tokens:** counted by the plugin's own estimator (o200k for OpenAI
  formats).
- **Cost:** the gateway's `ChatRecordCost` at list prices:
  - Claude Sonnet 4.5: input 3, output 15, cache write 3.75, cache read 0.30 per million tokens;
  - gpt-4.1: 2 / 8, cache read 0.50;
  - gpt-5-codex: 1.25 / 10, cache read 0.125;
  - plus 400 output tokens per turn.
- **Cache model:** each turn reads from cache the prefix it shares byte for
  byte with the previous turn. A changed prefix is a cache write (Anthropic,
  Bedrock) or a miss.
- **Prefix breaks:** turns that lose cached prefix the uncompressed history
  kept. A result compressed late, as it leaves the protected newest turn, is
  counted separately as a late compression: the design pays that cost once.

**End to end** (`e2e/phase1.sh`; run log in `e2e/runs/2026-10-21-phase1.md`).
Studio and the Microgateway built from clean exports (core 105a0451, community
ee47ffb), with a mock vendor, audit, level 1 and level 2.

- **121 checks pass.** In audit mode the upstream body is byte-identical to
  the client's. In enforce mode the upstream body is byte-identical to the
  library's output for the same body, and `Tokens-Saved` equals the library's
  estimate. Recorded usage matches the vendor's to the token. Real OpenAI and
  Anthropic requests are recorded exactly.
- **The 4 failures** are one gap: native Bedrock `converse-stream` is
  compressed but not labelled or recorded (eac.83).

**Latency** (gwbench, eac.15, dev1): added edge time at p95, warm, per body
size.

| Body | Level 1 | Level 2 | Level 3 |
|---|---|---|---|
| small (5 KB) | +0.4–0.5 ms | +0.4 ms | +0.4 ms |
| typical (185 KB) | +6.3–6.6 ms | +4.3–4.8 ms | +3.2–3.8 ms |
| large (3.9 MB) | +107–157 ms | +70–110 ms | +40–78 ms |

The large-body spread covers two runs on a laptop.

The typical level-1 figure is over the 5 ms target. eac.80 is cutting the
remaining cost: today the body is parsed five times per call.

## Results

### Summary

The session cost columns are against compression off. The single-body column
is the token reduction over the 44 bodies.

| Level | Single bodies | Sessions | Anthropic | OpenAI Chat | Bedrock | Codex (full history) | Chained Responses | Prefix breaks | Cache-read share |
|---|---|---|---|---|---|---|---|---|---|
| 1 | −1.2% | **−0.4%** | −0.3% | −0.8% | −0.0% | −0.7% | −0.4% | 0 | 89.4% |
| 2 | −35.3% | **−11.4%** | −12.6% | −22.4% | −6.6% | −3.9% | −0.4% | 0 | 83.7% |
| 3 | −37.7% | **−11.4%** | −12.6% | −22.4% | −6.6% | −3.9% | −0.4% | 0 | 83.7% |
| 3, `mask_after_turns` 8 | −37.7% | −11.1% | −11.5% | −25.0% | −7.8% | **+2.0%** | −0.4% | 1 per session | 78.0% |

- **Level 2 vs level 1:** level 2 saves less on Bedrock and Codex than on
  Anthropic and OpenAI Chat, because each result is compressed late (when it
  leaves the protected newest turn) and that costs one cache rewrite. Where
  cache reads are cheapest relative to input (Codex at 0.1x), a rewrite costs
  more of what the cut saves.
- **Masking:** the masked boundary moves now and then, and each move re-caches
  the history behind it. That pays only where cache writes cost nothing extra
  and reads are not much cheaper than input (gpt-4.1). On the Codex profile
  it makes level 3 dearer than off.
- **Chained Responses** sessions send only new items, so there is little
  history to compress, and the vendor bills the stored history this
  projection cannot see.
- 0 skip violations (bodies that must pass through changed), 0
  nondeterministic outputs and 0 errors at every level.

### Level 2 by tool kind

The per-kind numbers below are tool-result tokens before and after. Single
bodies have their tool output in an older turn, which is the case level 2 is
for. The newest turn stays at level 1 by policy.

| Kind | Single bodies | Sessions (last turn) | Notes |
|---|---|---|---|
| maven | 89.5% | 57.9% | |
| go test | 88.7% | 79.8% | |
| cargo test | 75.4% | 61.3% | |
| gradle | 71.1% | 58.9% | |
| jest | 54.6% | 17.6% | |
| docker build | 50.5% | 77.7% | |
| MCP results | 45.0% | – | 83% on a 100-result search; short results are left alone |
| pytest | 40.9% | 24.5% | |
| directory listings | 36.3% | 50.8% | |
| git diff | 24.7% | 0% | the lock and generated files go (53% on a dependency bump). Diffs at the default 3 lines of context have no other slack, so they stay whole |
| ripgrep | 24.6% | 0% | hits under vendor/ and node_modules/ and the 6th+ match in a file go (72% on a repo-root search). A scoped search stays whole |
| kubectl | 23.4% | 46.8% | |
| HTML | 18.2% | 0% | script, style and SVG blocks go (60% on a framework-built page). Prose pages save under the 30% minimum cut, so they stay whole |
| terraform plan | 11.2% | 39.5% | |
| JSON API | 8.0% (34.9% at level 3) | 21.2% | arrays under 20 items are left whole |
| CSV | 0% | 0% | **by design**: export rows are distinct data. Level 3's `max_unit_tokens` is what cuts a large export |

The session generator has not changed since the first replay, so the session
costs stay comparable across runs. Its diffs, searches and pages have no lock
files, dependency hits or large scripts, which is why those kinds show 0%
there.

### Reducer misfires

None found on the corpus:

- 0 skip violations: no body that must pass through was changed, and no
  planted canary was rewritten.
- 0 nondeterministic outputs.
- 0 prefix breaks at levels 1 and 2.

The reducers that cut logs keep failure lines by construction. Quality loss
from the lines they do cut is what the paid suites below would measure.

Bugs found by this evaluation and the end-to-end checks:

| Bead | What | State |
|---|---|---|
| eac.69 | level 3 masking cost more than level 2 | fixed: masking off by default, a step rule |
| eac.70 | reducers measured 0%: newest-turn protection and corpus shapes | fixed (corpus); reducers pay for their cache rewrite (30% minimum cut) |
| eac.73 | `drop_tools` left a lone Bedrock cachePoint (400) | fixed |
| eac.74 | `drop_tools` dropped Anthropic's cache breakpoint | fixed |
| eac.81 | tools panel counts dropped tools (share over 100%) | open |
| eac.83 | native Bedrock converse-stream not labelled or recorded | open |

## Paid suites for level 2: plan and request (not run)

The gate (eac.4) makes level 2 a default only when it is not worse than off
on all of:

- BFCL (at least 1,000 cases, within 1 point);
- long-context QA (at least 500, within 2 points of F1);
- SWE-bench Verified (200 tasks × 3 seeds, within 5 points, no more than 10%
  more turns);
- cost including cache.

Level 1 does not need these suites, so the plan compares off with level 2
only.

`pc-eval plan` estimates, through the Microgateway, two arms (off and level 2):

| Suite | Runs | Claude Sonnet 4.5 | gpt-4.1 | Wall time at 4 in parallel |
|---|---|---|---|---|
| BFCL, 1,000 tasks | 2,000 | $11 | $7 | 33 min |
| Long-context QA, 500 | 1,000 | $91 | $61 | 1 h |
| SWE-bench Verified, 200 × 3 seeds | 1,200 | $1,660 | $1,392 | 60 h |
| **Total, one vendor** | | **$1,762** | **$1,460** | about 62 h |

The token figures behind these estimates are planning assumptions, not
measurements:

- BFCL: 1,500 tokens in per task;
- QA: 30,000 per task;
- SWE-bench: 1.2 M per task, 80% from cache.

A pilot replaces them with measured values.

**Two caveats before spending:**

1. **BFCL and long-context QA barely exercise level 2.**
   - The default policy compresses tool results only (`roles: [tool]`).
   - BFCL requests carry tool definitions and short calls.
   - Long-context QA carries its document in the user message.
   - Both would pass almost trivially. They are cheap, so they are worth
     running as a regression check, but they say little about the reducers.
2. **SWE-bench is the real test, and the most expensive.**
   - It is the only suite whose requests carry long tool output.
   - The suite runners are not built yet: `internal/eval/live.go` has the
     planner and the spend guard, the runners were scoped to this bead, and
     SWE-bench needs its Docker harness.

**Request to Martin (unrun; no spend without approval):**

| Stage | What | Cap |
|---|---|---|
| A | Build the runners. Pilot SWE-bench Verified with 25 tasks × 3 seeds × 2 arms on Claude Sonnet 4.5 (150 runs, about 7.5 h, estimated $207), to measure real tokens per task, turns and resolve rates. Then re-plan with the measured figures. | **$220** |
| B | BFCL 1,000 and long-context QA 500, off vs level 2, one vendor. | **$120** |
| C | The full SWE-bench gate (200 × 3 × 2) on one vendor, only if A shows level 2 within reach of the margin. Re-estimated after A; today's estimate is $1.4–1.7k. | **$1,800** |

Stages A and B together are $340. Stage C needs its own go-ahead after A.
Level 2 stays opt-in in the meantime.

## Reproducing

```bash
cd plugins/prompt-compressor
go run ./cmd/pc-eval replay -levels 0,1,2,3 -out /tmp/pc-report
echo '{"mask_after_turns": 8}' > /tmp/mask8.json
go run ./cmd/pc-eval replay -levels 0,3 -policy /tmp/mask8.json -out /tmp/pc-mask8
go run ./cmd/pc-eval plan -suite swebench -levels 2 -format anthropic -n 25
```
