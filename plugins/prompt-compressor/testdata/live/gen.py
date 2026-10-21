"""Generate the synthetic live-suite fixtures (eac.23) into OUT.

testdata/live/<suite>/tasks.jsonl and oracle.json for bfcl, lcqa and
swebench, in the shapes agreed with the runner: everything Acme/example,
seeded, deterministic. Some tasks keep their evidence where level 2 keeps
it (failure lines, array edges), some put it where level 2 cuts it, so a
dry run shows the pipeline can see a regression.
"""
import json
import os
import random
import sys

OUT = sys.argv[1]
R = random.Random(20261021)

SYSTEM = "You are an engineering assistant for Acme's ledger service. Use the tools when you need data."


def oa_tool(name, desc, props, req):
    return {"type": "function", "function": {"name": name, "description": desc,
            "parameters": {"type": "object", "properties": props, "required": req}}}


def an_tool(name, desc, props, req):
    return {"name": name, "description": desc, "input_schema": {"type": "object", "properties": props, "required": req}}


S = {"type": "string"}
TOOLS = [
    ("search_invoices", "Search invoices by customer.", {"customer": S}, ["customer"]),
    ("refund_invoice", "Refund one invoice in full.", {"invoice_id": S, "reason": S}, ["invoice_id", "reason"]),
    ("get_customer", "Look up a customer.", {"customer": S}, ["customer"]),
    ("read_file", "Read a file from the repository.", {"path": S}, ["path"]),
    ("bash", "Run a shell command in the repository.", {"command": S}, ["command"]),
]


def tools(fmt):
    mk = oa_tool if fmt == "openai" else an_tool
    return [mk(*t) for t in TOOLS]


def invoices(n, special_at, special):
    rows = []
    for i in range(n):
        inv = {"id": "inv_%05d" % R.randint(10000, 99999), "customer": "cus_acme_%03d" % R.randint(1, 400),
               "amount_minor": R.randint(1000, 900000), "currency": R.choice(["EUR", "GBP", "USD"]),
               "status": R.choice(["paid", "paid", "paid", "open"]), "created_at": "2026-09-%02dT10:%02d:00Z" % (R.randint(1, 30), R.randint(0, 59))}
        rows.append(inv)
    rows[special_at] = special
    return json.dumps({"invoices": rows, "has_more": False}, separators=(",", ":"))


def history(fmt, task_id, user, calls, final_user):
    """calls: [(name, args, output)], each its own assistant turn."""
    if fmt == "openai":
        msgs = [{"role": "system", "content": SYSTEM}, {"role": "user", "content": "[task:%s] %s" % (task_id, user)}]
        for k, (name, args, out) in enumerate(calls):
            cid = "call_%s_%d" % (task_id.replace("-", ""), k)
            msgs.append({"role": "assistant", "content": None, "tool_calls": [{"id": cid, "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]})
            msgs.append({"role": "tool", "tool_call_id": cid, "content": out})
        msgs.append({"role": "user", "content": final_user})
        return {"model": "gpt-4.1", "messages": msgs, "tools": tools(fmt)}
    msgs = [{"role": "user", "content": "[task:%s] %s" % (task_id, user)}]
    pending = None
    for k, (name, args, out) in enumerate(calls):
        cid = "toolu_%s_%d" % (task_id.replace("-", ""), k)
        msgs.append({"role": "assistant", "content": [{"type": "tool_use", "id": cid, "name": name, "input": args}]})
        msgs.append({"role": "user", "content": [{"type": "tool_result", "tool_use_id": cid, "content": out}]})
    msgs[-1]["content"].append({"type": "text", "text": final_user})
    return {"model": "claude-sonnet-4-5", "max_tokens": 1024, "system": SYSTEM, "messages": msgs, "tools": tools(fmt)}


def write(suite, tasks, oracle):
    d = os.path.join(OUT, suite)
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "tasks.jsonl"), "w") as f:
        for t in tasks:
            f.write(json.dumps(t, separators=(",", ":")) + "\n")
    with open(os.path.join(d, "oracle.json"), "w") as f:
        json.dump({"tasks": oracle}, f, indent=1, sort_keys=True)
        f.write("\n")


# ---- BFCL-shaped: the right call needs a value from an earlier tool result
bfcl, bo = [], {}
for k, (fmt, pos) in enumerate([("openai", 0), ("anthropic", 39), ("openai", 20), ("anthropic", 17), ("openai", 2), ("anthropic", 31)]):
    tid = "bfcl-%03d" % (k + 1)
    target = "inv_%05d" % (70000 + k)
    special = {"id": target, "customer": "cus_acme_042", "amount_minor": 129900, "currency": "EUR", "status": "disputed", "created_at": "2026-09-14T09:12:00Z"}
    lookup = invoices(40, pos, special)
    other = json.dumps({"customer": "cus_acme_042", "name": "Acme Example GmbH", "country": "DE"})
    req = history(fmt, tid, "Customer cus_acme_042 disputes an invoice.",
                  [("search_invoices", {"customer": "cus_acme_042"}, lookup), ("get_customer", {"customer": "cus_acme_042"}, other)],
                  "Refund the disputed invoice, reason 'customer dispute'.")
    call = {"name": "refund_invoice", "arguments": {"invoice_id": target, "reason": "customer dispute"}}
    bfcl.append({"id": tid, "format": fmt, "request": req, "expect": {"calls": [call]}})
    bo[tid] = {"evidence": ['"id":"%s"' % target], "answer": {"call": call},
               "wrong": {"call": {"name": "refund_invoice", "arguments": {"invoice_id": "inv_unknown", "reason": "customer dispute"}}}}
write("bfcl", bfcl, bo)

# ---- long-context QA-shaped: the document arrives as a tool result
def build_log(fact_line, at):
    lines = []
    for i in range(400):
        lines.append("[INFO] %s Building ledger-%s %d.%d.%d (%d/%d)" % ("2026-10-0%d" % R.randint(1, 9), R.choice(["api", "worker", "export"]), 1, R.randint(0, 9), R.randint(0, 99), i, 400))
    lines[at] = fact_line
    return "\n".join(lines) + "\n[INFO] BUILD SUCCESS\n"

lcqa, lo = [], {}
facts = [
    ("[ERROR] rounding: settlement batch 7731 rejected, scale 3 exceeds the EUR minor unit", "7731", "settlement batch 7731"),
    ("[INFO] Using decimal library 4.2.1 from the internal mirror", "4.2.1", "decimal library 4.2.1"),
    ("[ERROR] export: GBP ledger closed at 23:59:58, two seconds early", "23:59:58", "GBP ledger closed at 23:59:58"),
    ("[INFO] Feature flag ledger.half_even enabled for tenants 12 and 44", "12 and 44", "tenants 12 and 44"),
]
for k, (line, ans, ev) in enumerate(facts):
    tid = "lcqa-%03d" % (k + 1)
    fmt = "openai" if k % 2 == 0 else "anthropic"
    doc = build_log(line, 150 + k * 30)
    req = history(fmt, tid, "Read the nightly build log.", [("read_file", {"path": "logs/nightly-build.log"}, doc), ("get_customer", {"customer": "cus_acme_001"}, "{}")],
                  "From the build log: answer in a few words only.")
    lcqa.append({"id": tid, "format": fmt, "request": req, "expect": {"answers": [ans]}})
    lo[tid] = {"evidence": [ev], "answer": {"text": ans}, "wrong": {"text": "not in the log"}}
write("lcqa", lcqa, lo)

# ---- SWE-bench-shaped agent loops: scripted tool outputs, the fix needs a
# detail from an earlier output
def go_test(fail_detail):
    out = ["=== RUN   TestCurrency%02d\n--- PASS: TestCurrency%02d (0.00s)" % (i, i) for i in range(60)]
    out.insert(30, fail_detail)
    return "\n".join(out) + "\nFAIL\nFAIL\tacme.example/ledger/internal/money\t0.412s\n"

swe, so = [], {}
cases = [
    ("--- FAIL: TestRoundHalfEven (0.00s)\n    round_test.go:41: got 2.67, want 2.66 for 2.665 EUR", "round_test.go:41: got 2.67, want 2.66", "HalfEven"),
    ("--- FAIL: TestScaleJPY (0.00s)\n    scale_test.go:18: JPY scale 2, want 0", "JPY scale 2, want 0", "scale 0"),
]
for k, (detail, ev, fix) in enumerate(cases):
    tid = "swe-%03d" % (k + 1)
    fmt = "openai" if k == 0 else "anthropic"
    ask = "[task:%s] The money package tests fail on main. Fix it. Start by running the tests." % tid
    if fmt == "openai":
        first = {"model": "gpt-4.1", "messages": [{"role": "system", "content": SYSTEM}, {"role": "user", "content": ask}], "tools": tools(fmt)}
    else:
        first = {"model": "claude-sonnet-4-5", "max_tokens": 1024, "system": SYSTEM, "messages": [{"role": "user", "content": ask}], "tools": tools(fmt)}
    outputs = [go_test(detail), "package money\n\n// Round rounds to the currency's minor unit.\nfunc Round(a Amount) Amount { return a.round(halfUp) }\n", "ok\tacme.example/ledger/internal/money\t0.388s\n"]
    swe.append({"id": tid, "format": fmt, "request": first, "tool_outputs": outputs, "max_turns": 6, "expect": {"resolved_if": fix}})
    so[tid] = {"evidence": [ev],
               "steps": [{"name": "bash", "arguments": {"command": "go test ./internal/money/..."}}, {"name": "read_file", "arguments": {"path": "internal/money/round.go"}}, {"name": "bash", "arguments": {"command": "go test ./internal/money/..."}}],
               "answer": {"text": "Fixed: use %s rounding in internal/money/round.go." % fix}, "wrong": {"text": "I could not find the failing case."}}
write("swebench", swe, so)
print("wrote", len(bfcl), len(lcqa), len(swe))
