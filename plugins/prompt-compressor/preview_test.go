package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
)

const previewConfig = `{"mode": "enforce", "default_policy": "agents",
  "policies": {"agents": {"level": 2, "min_unit_tokens": 50, "protect_patterns": ["ACC-[0-9]+"]}}}`

func previewPlugin(t *testing.T) (*PromptCompressorPlugin, *memKV) {
	t.Helper()
	kv := &memKV{data: map[string][]byte{}}
	p := NewPromptCompressorPlugin()
	ctx := plugin_sdk.Context{Runtime: plugin_sdk.RuntimeStudio, Services: &telemetryBroker{kv: kv}}
	if err := p.Initialize(ctx, hostConfig(t, previewConfig)); err != nil {
		t.Fatal(err)
	}
	return p, kv
}

func runPreview(t *testing.T, p *PromptCompressorPlugin, req map[string]interface{}) previewResponse {
	t.Helper()
	raw, _ := json.Marshal(req)
	out, err := p.HandleRPC("preview", raw)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	var resp previewResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// The preview's output is the body an edge in enforce mode forwards, for
// every sample, with the same policy.
func TestPreviewMatchesTheEdge(t *testing.T) {
	studio, _ := previewPlugin(t)
	for _, s := range sampleIndex {
		resp := runPreview(t, studio, map[string]interface{}{"mode": "sample", "sample": s.Name, "policy_name": "agents"})

		edge, ctx := hookPlugin(t, previewConfig)
		ctx.Vendor = s.Vendor
		_, body, _ := loadSample(s.Name)
		req := request(body, nil)
		req.Request.Path = "/llm/rest/gpt" + s.Path
		hook, err := edge.HandlePostAuth(ctx, req)
		if err != nil {
			t.Fatalf("%s: HandlePostAuth: %v", s.Name, err)
		}
		forwarded := body
		if hook.Modified {
			forwarded = hook.Body
		}
		if resp.Output != string(forwarded) {
			t.Errorf("%s: preview output (%d bytes, %s) differs from what the edge forwards (%d bytes)", s.Name, len(resp.Output), resp.Report.Outcome, len(forwarded))
		}
		if resp.Report.Outcome == compress.OutcomeCompressed && !resp.Changed {
			t.Errorf("%s: compressed but not marked changed", s.Name)
		}
	}
}

func TestPreviewPastedPytestAtLevel2(t *testing.T) {
	p, _ := previewPlugin(t)
	text := strings.Replace(previewPytest, "ACC-1042", "ACC-1042", 1)
	for _, format := range []string{"openai", "anthropic", "openai_responses"} {
		resp := runPreview(t, p, map[string]interface{}{"mode": "tool_output", "format": format, "tool_name": "pytest", "text": text, "policy_name": "agents"})
		if resp.Report.Outcome != compress.OutcomeCompressed || resp.Level != 2 {
			t.Fatalf("%s: outcome %s, level %d", format, resp.Report.Outcome, resp.Level)
		}
		for _, keep := range []string{"test_ledger_balances_after_refund", "test_monthly_report_totals", "2 failed, 212 passed", "[tyk-compressed:"} {
			if !strings.Contains(resp.Output, keep) {
				t.Errorf("%s: output lacks %q", format, keep)
			}
		}
		var tool *previewUnit
		for i := range resp.Units {
			if resp.Units[i].Changed {
				tool = &resp.Units[i]
			}
		}
		if tool == nil || tool.Level != 2 || len(tool.Capped) != 0 || len(tool.Leaves) == 0 {
			t.Fatalf("%s: tool unit = %+v", format, tool)
		}
		if len(tool.Detected) == 0 || tool.Detected[0] != "pytest" {
			t.Errorf("%s: detected = %v", format, tool.Detected)
		}
		if len(tool.Protected) != 1 || tool.Protected[0].Before == 0 || tool.Protected[0].After != tool.Protected[0].Before {
			t.Errorf("%s: protect hits = %+v", format, tool.Protected)
		}
		var skipped int
		for _, u := range resp.Units {
			if !u.Selected && u.Reason == "" {
				t.Errorf("%s: unit %d skipped with no reason", format, u.Index)
			}
			if !u.Selected {
				skipped++
			}
		}
		if skipped == 0 {
			t.Errorf("%s: no unit was skipped (the user turns should be, by role)", format)
		}
	}
}

func TestPreviewInlinePolicyIsValidatedLikeASavedOne(t *testing.T) {
	p, _ := previewPlugin(t)
	resp := runPreview(t, p, map[string]interface{}{"mode": "tool_output", "format": "openai", "text": previewPytest,
		"policy": map[string]interface{}{"level": 1, "min_unit_tokens": 1}})
	if resp.PolicyName != "(edited here)" || resp.Level != 1 {
		t.Errorf("inline policy: %s level %d", resp.PolicyName, resp.Level)
	}
	for _, bad := range []map[string]interface{}{
		{"level": 7},
		{"protect_patterns": []string{"[a-"}},
		{"levle": 2},
	} {
		raw, _ := json.Marshal(map[string]interface{}{"mode": "tool_output", "format": "openai", "text": "x", "policy": bad})
		if _, err := p.HandleRPC("preview", raw); err == nil {
			t.Errorf("invalid inline policy %v accepted", bad)
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{"mode": "tool_output", "format": "openai", "text": "x", "policy_name": "missing"})
	if _, err := p.HandleRPC("preview", raw); err == nil {
		t.Error("unknown saved policy accepted")
	}
}

// Inputs over 2 MiB are refused before they are parsed; nothing is
// stored and no error or log line repeats the input.
func TestPreviewLimitsAndPrivacy(t *testing.T) {
	p, kv := previewPlugin(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)

	big := strings.Repeat("a", maxPreviewPayload+1)
	if _, err := p.HandleRPC("preview", []byte(big)); err != errPreviewTooLarge {
		t.Errorf("oversized payload: %v", err)
	}
	raw, _ := json.Marshal(map[string]interface{}{"mode": "tool_output", "format": "openai", "text": strings.Repeat("b", maxPreviewBytes+1)})
	if _, err := p.HandleRPC("preview", raw); err == nil {
		t.Error("oversized text accepted")
	}

	const canary = "CANARY-7f3a9c-secret-tool-output"
	for _, req := range []map[string]interface{}{
		{"mode": "tool_output", "format": "openai", "text": canary + "\n" + previewPytest},
		{"mode": "tool_output", "format": "gemini", "text": canary},
		{"mode": "body", "vendor": "openai", "path": "/v1/chat/completions", "body": json.RawMessage(`{"messages": "` + canary + `"}`)},
		{"mode": "body", "vendor": "openai", "path": "no-slash", "body": json.RawMessage(`{"x": "` + canary + `"}`)},
		{"mode": "nope", "text": canary},
	} {
		raw, _ := json.Marshal(req)
		_, err := p.HandleRPC("preview", raw)
		if err != nil && strings.Contains(err.Error(), canary) {
			t.Errorf("an error repeats the input: %v", err)
		}
	}
	if _, err := p.HandleRPC("preview", []byte(`{"mode": "tool_output", "text": "`+canary)); err == nil || strings.Contains(err.Error(), canary) {
		t.Errorf("malformed JSON: %v", err)
	}
	if strings.Contains(logs.String(), canary) {
		t.Error("the input reached the log")
	}
	if kv.writes() != 0 {
		t.Errorf("%d KV keys written by previews", kv.writes())
	}
}

func TestPreviewOptions(t *testing.T) {
	p, _ := previewPlugin(t)
	out, err := p.HandleRPC("getPreviewOptions", nil)
	if err != nil {
		t.Fatal(err)
	}
	var opts struct {
		Samples  []previewSample `json:"samples"`
		Policies []struct {
			Name  string `json:"name"`
			Level int    `json:"level"`
		} `json:"policies"`
		DefaultPolicy string `json:"default_policy"`
	}
	if err := json.Unmarshal(out, &opts); err != nil {
		t.Fatal(err)
	}
	if len(opts.Samples) != len(sampleIndex) || opts.DefaultPolicy != "agents" || len(opts.Policies) != 2 {
		t.Errorf("options = %+v", opts)
	}
	for _, s := range sampleIndex {
		if _, raw, err := loadSample(s.Name); err != nil || !json.Valid(raw) {
			t.Errorf("sample %s: %v", s.Name, err)
		}
	}
}

const previewPytest = `============================= test session starts ==============================
platform linux -- Python 3.12.3, pytest-8.3.2, pluggy-1.5.0
rootdir: /srv/ledger
collected 214 items

tests/test_accounts.py ........................................          [ 18%]
tests/test_ledger.py ......................F.....................        [ 39%]
tests/test_payments.py ..............................................    [ 60%]
tests/test_reports.py ....................F..........................    [ 82%]
tests/test_users.py ......................................               [100%]

=================================== FAILURES ===================================
_____________________ test_ledger_balances_after_refund ______________________

    def test_ledger_balances_after_refund():
        ledger = make_ledger()
        ledger.post("ACC-1042", Decimal("100.00"))
        ledger.refund("ACC-1042", Decimal("20.00"))
>       assert ledger.balance("ACC-1042") == Decimal("120.00")
E       AssertionError: assert Decimal('100.00') == Decimal('120.00')

tests/test_ledger.py:88: AssertionError
_________________________ test_monthly_report_totals __________________________

    def test_monthly_report_totals():
>       assert report.total("2026-09") == 4410
E       assert 4401 == 4410

tests/test_reports.py:57: AssertionError
=========================== short test summary info ============================
FAILED tests/test_ledger.py::test_ledger_balances_after_refund - AssertionError
FAILED tests/test_reports.py::test_monthly_report_totals - assert 4401 == 4410
======================== 2 failed, 212 passed in 9.81s =========================
`
