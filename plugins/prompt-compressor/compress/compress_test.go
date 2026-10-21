package compress

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// determinismHelperEnv makes the test binary print the digest of every
// corpus body it compresses, for TestDeterministicAcrossProcesses.
const determinismHelperEnv = "PC_COMPRESS_DIGESTS"

func TestMain(m *testing.M) {
	// No test may reach the network, the tokenizer included.
	http.DefaultTransport = noNetwork{}
	if os.Getenv(determinismHelperEnv) != "" {
		fmt.Print(corpusDigest())
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type noNetwork struct{}

func (noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("network access in a test: %s", r.URL)
}

// allRoles is the default policy with every role selected.
func allRoles() *config.Policy {
	p := config.DefaultPolicy()
	p.Roles = []string{config.RoleTool, config.RoleUser, config.RoleAssistant, config.RoleSystem}
	p.MinUnitTokens = 0
	return p
}

func loadCorpus(t testing.TB) corpus.Manifest {
	t.Helper()
	m, err := corpus.Load(corpus.Dir())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func corpusDigest() string {
	m, err := corpus.Load(corpus.Dir())
	if err != nil {
		return "error: " + err.Error()
	}
	c := New(Options{})
	var b strings.Builder
	for _, e := range m.Entries {
		out, rep := c.Compress(Input{Format: e.Format, Body: e.Body(), Policy: allRoles()})
		sum := sha256.Sum256(out)
		fmt.Fprintf(&b, "%s %s %s %d\n", e.Name, hex.EncodeToString(sum[:]), rep.Outcome, rep.TokensSaved())
	}
	return b.String()
}

// Zero edits through Apply give every corpus body back byte for byte, and
// a level-0 policy returns it unchanged.
func TestCorpusZeroEditsAreIdentity(t *testing.T) {
	c := New(Options{})
	off := config.DefaultPolicy()
	off.Level = 0
	for _, e := range loadCorpus(t).Entries {
		out, err := segments.Apply(e.Body(), nil)
		if err != nil || !bytes.Equal(out, e.Body()) {
			t.Errorf("%s: Apply without edits changed the body (%v)", e.Name, err)
		}
		out, rep := c.Compress(Input{Format: e.Format, Body: e.Body(), Policy: off})
		if !bytes.Equal(out, e.Body()) || rep.Outcome != OutcomeSkipped || rep.Reason != ReasonOff {
			t.Errorf("%s: level 0 gave %s/%s", e.Name, rep.Outcome, rep.Reason)
		}
	}
}

// Every readable corpus body compresses, and the result keeps everything
// that is not a selected leaf byte for byte, keeps tool calls paired, is
// no larger and still reads in its format.
func TestCorpusProperties(t *testing.T) {
	c := New(Options{})
	compressed := 0
	for _, e := range loadCorpus(t).Entries {
		t.Run(e.Name, func(t *testing.T) {
			body := e.Body()
			out, rep := c.Compress(Input{Format: e.Format, Body: body, Policy: allRoles()})
			if !e.Readable(segments.Supports) {
				if !bytes.Equal(out, body) || rep.Outcome != OutcomeSkipped {
					t.Fatalf("an unreadable body came back %s (%s)", rep.Outcome, rep.Reason)
				}
				return
			}
			switch rep.Outcome {
			case OutcomeCompressed:
				compressed++
			case OutcomeUnchanged:
				if !bytes.Equal(out, body) {
					t.Fatal("unchanged outcome with a changed body")
				}
				return
			default:
				t.Fatalf("outcome %s (%s)", rep.Outcome, rep.Reason)
			}
			if len(out) > len(body) {
				t.Errorf("output %d bytes > input %d", len(out), len(body))
			}
			if rep.BytesAfter != len(out) || rep.TokensAfter > rep.TokensBefore {
				t.Errorf("report: %+v", rep)
			}
			if segments.CheckToolPairing(e.Format, body) == nil {
				if err := segments.CheckToolPairing(e.Format, out); err != nil {
					t.Errorf("pairing lost: %v", err)
				}
			}
			if !bytes.Equal(skeleton(t, e.Format, body), skeleton(t, e.Format, out)) {
				t.Error("bytes outside the text leaves changed")
			}
			if segments.NativeContextManagement(e.Format, body) != rep.NativeContextManagement {
				t.Error("native context management not reported")
			}
		})
	}
	if compressed == 0 {
		t.Fatal("no corpus body compressed")
	}
}

// skeleton is body with every text leaf emptied: what must survive any
// compression byte for byte (ids, images, signatures, thinking,
// cache_control, unknown fields, key order, numbers).
func skeleton(t *testing.T, format string, body []byte) []byte {
	t.Helper()
	units, err := segments.ExtractUnits(format, body)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	var edits []segments.Edit
	for _, u := range units {
		for _, l := range u.Leaves {
			edits = append(edits, segments.Edit{Path: l.Path, Old: l.Text, New: ""})
		}
	}
	out, err := segments.Apply(body, edits)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The same input and policy give the same bytes every time: 1,000 runs of
// the first body of each format, 10 of every other.
func TestDeterministic(t *testing.T) {
	m := loadCorpus(t)
	seenFormat := map[string]bool{}
	for _, e := range m.Entries {
		if !e.Readable(segments.Supports) {
			continue
		}
		runs := 10
		if !seenFormat[e.Format] {
			seenFormat[e.Format] = true
			runs = 1000
		}
		first, _ := New(Options{}).Compress(Input{Format: e.Format, Body: e.Body(), Policy: allRoles()})
		c := New(Options{})
		for i := 0; i < runs; i++ {
			// Alternate cold and warm memo.
			if i%100 == 0 {
				c = New(Options{})
			}
			out, _ := c.Compress(Input{Format: e.Format, Body: e.Body(), Policy: allRoles()})
			if !bytes.Equal(out, first) {
				t.Fatalf("%s: run %d differs", e.Name, i)
			}
		}
	}
}

// Another process compresses the corpus to the same bytes.
func TestDeterministicAcrossProcesses(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), determinismHelperEnv+"=1")
	got, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := corpusDigest(); string(got) != want {
		t.Fatalf("another process compressed differently:\n%s\nwant\n%s", got, want)
	}
}

// Cache stability: in every full-history session at level 1, turn k's
// compressed conversation, less its newest message, is the start of turn
// k+1's, element for element and byte for byte. That is what keeps the
// provider's prompt cache hitting.
//
// At level 2 the newest turn is protected (level 1) and is reduced once a
// newer turn follows, so the comparison stops before turn k's newest turn:
// everything older is byte-identical.
func TestSessionsCacheStable(t *testing.T) {
	for _, level := range []int{1, 2} {
		reduced := 0
		c := New(Options{})
		p := config.DefaultPolicy()
		p.Level = level
		for _, s := range loadCorpus(t).Sessions {
			if s.History != "full" || !segments.Supports(s.Format) {
				continue
			}
			t.Run(fmt.Sprintf("level %d %s", level, s.Name), func(t *testing.T) {
				var prev []json.RawMessage
				prevNewest := 0
				saved := 0
				for k, body := range s.Bodies() {
					out, rep := c.Compress(Input{Format: s.Format, Body: body, Policy: p})
					if rep.Outcome == OutcomeRejected || rep.Outcome == OutcomeSkipped {
						t.Fatalf("turn %d: %s (%s)", k, rep.Outcome, rep.Reason)
					}
					saved += rep.TokensSaved()
					cur := conversation(t, s.Format, out)
					if prev != nil {
						if len(cur) < len(prev) {
							t.Fatalf("turn %d has fewer messages than turn %d", k, k-1)
						}
						for i := 0; i < prevNewest; i++ {
							if !bytes.Equal(prev[i], cur[i]) {
								t.Fatalf("turn %d: message %d differs from turn %d's:\n%s\nvs\n%s", k, i, k-1, prev[i], cur[i])
							}
						}
					}
					prev, prevNewest = cur, len(cur)-1
					if level >= 2 {
						prevNewest = newestTurnStart(t, s.Format, out, len(cur))
					}
					for name := range rep.Strategies {
						if strings.HasPrefix(name, "reduce_") {
							reduced++
						}
					}
				}
				if saved == 0 {
					t.Error("the session saved nothing")
				}
			})
		}
		if level >= 2 && reduced == 0 {
			t.Error("no session output was reduced at level 2")
		}
	}
}

// newestTurnStart is the index of the first message of the body's newest
// turn (what the client sent since the model last spoke); n when unknown.
func newestTurnStart(t *testing.T, format string, body []byte, n int) int {
	t.Helper()
	units, err := segments.ExtractUnits(format, body)
	if err != nil {
		t.Fatal(err)
	}
	last := 0
	for _, u := range units {
		last = max(last, u.Turn)
	}
	start := n
	for _, u := range units {
		if u.Turn == last && u.Role != "assistant" && u.Message >= 0 {
			start = min(start, u.Message)
		}
	}
	return start
}

// conversation returns the raw elements of the body's conversation array.
func conversation(t *testing.T, format string, body []byte) []json.RawMessage {
	t.Helper()
	var b struct {
		Messages []json.RawMessage `json:"messages"`
		Contents []json.RawMessage `json:"contents"`
		Input    json.RawMessage   `json:"input"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatal(err)
	}
	switch {
	case b.Messages != nil:
		return b.Messages
	case b.Contents != nil:
		return b.Contents
	}
	var items []json.RawMessage
	if err := json.Unmarshal(b.Input, &items); err != nil {
		t.Fatalf("%s body has no conversation array", format)
	}
	return items
}

// Malformed, unknown-format and generically read bodies come back unchanged.
func TestSkipsWhatItCannotRead(t *testing.T) {
	c := New(Options{})
	for _, tc := range []struct {
		format, body, reason string
	}{
		{"openai", `not json`, ReasonUnreadable},
		{"openai", `{"messages":[],"messages":[]}`, ReasonUnreadable},
		{"openai", `{"input":"an embeddings body"}`, ReasonUnreadable},
		{"huggingface", `{"inputs":"x"}`, ReasonUnsupportedFormat},
		{"openai_decisions", `{"questions":[]}`, ReasonUnsupportedFormat},
		{"openai_input", `{"input":"text   \n\n\n\nmore"}`, ReasonUnsupportedFormat},
		{"", `{"messages":[{"role":"tool","content":"x"}]}`, ReasonUnsupportedFormat},
	} {
		out, rep := c.Compress(Input{Format: tc.format, Body: []byte(tc.body), Policy: allRoles()})
		if string(out) != tc.body || rep.Outcome != OutcomeSkipped || rep.Reason != tc.reason {
			t.Errorf("%s %s: %s/%s", tc.format, tc.body, rep.Outcome, rep.Reason)
		}
	}
	big := []byte(`{"messages":[{"role":"tool","tool_call_id":"c","content":"` + strings.Repeat("x  \n", 300) + `"}]}`)
	if _, rep := c.Compress(Input{Format: "openai", Body: big, Policy: allRoles(), MaxBodyBytes: 100}); rep.Reason != ReasonTooLarge {
		t.Errorf("over max_body_bytes: %+v", rep)
	}
}

// Selection: tools only by default, min_unit_tokens, tool excludes,
// undecodable units, protected turns at level 1.
func TestSelection(t *testing.T) {
	noisy := strings.Repeat("line with trailing spaces   \n\n\n", 200)
	body := mustJSON(t, map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": noisy},
		map[string]any{"role": "user", "content": noisy},
		map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "shell", "arguments": "{}"}},
			map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "secret_tool", "arguments": "{}"}},
			map[string]any{"id": "c3", "type": "function", "function": map[string]any{"name": "shell", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": noisy},
		map[string]any{"role": "tool", "tool_call_id": "c2", "content": noisy},
		map[string]any{"role": "tool", "tool_call_id": "c3", "content": "tiny   "},
	}})
	p := config.DefaultPolicy()
	p.ToolOverrides = map[string]*config.ToolOverride{"secret_tool": {Exclude: true}}
	out, rep := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p})
	if rep.Outcome != OutcomeCompressed || rep.UnitsSelected != 1 || rep.UnitsCompressed != 1 {
		t.Fatalf("report %+v", rep)
	}
	units, _ := segments.ExtractUnits("openai", out)
	for _, u := range units {
		changed := u.Text != noisy && u.Text != "tiny   "
		wantChanged := u.ToolName == "shell" && u.ToolCallID == "c1"
		if changed != wantChanged {
			t.Errorf("unit %s/%s (%s) changed = %v", u.Role, u.ToolName, u.ToolCallID, changed)
		}
	}
}

// cache_mode skip_cached leaves the units before the last breakpoint alone.
func TestSkipCached(t *testing.T) {
	noisy := strings.Repeat("progress 10%\r100%   \n\n\n", 100)
	body := mustJSON(t, map[string]any{"messages": []any{
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "t1", "name": "a", "input": map[string]any{}}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": noisy, "cache_control": map[string]any{"type": "ephemeral"}}}},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "t2", "name": "b", "input": map[string]any{}}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t2", "content": noisy}}},
	}})
	p := config.DefaultPolicy()
	p.CacheMode = config.CacheSkipCached
	p.ProtectRecentTurns = 0
	out, rep := New(Options{}).Compress(Input{Format: "anthropic", Body: body, Policy: p})
	if rep.CacheSkipped != 1 || rep.UnitsCompressed != 1 {
		t.Fatalf("report %+v", rep)
	}
	units, _ := segments.ExtractUnits("anthropic", out)
	for _, u := range units {
		if u.ToolCallID == "t1" && u.Text != noisy {
			t.Error("a cached tool result was rewritten")
		}
		if u.ToolCallID == "t2" && u.Text == noisy {
			t.Error("the uncached tool result was not compressed")
		}
	}
}

// dedupe_references replaces a repeated tool output with a reference to the
// first; it is off by default.
func TestDedupeReferences(t *testing.T) {
	output := strings.Repeat("identical tool output line\n", 100)
	body := mustJSON(t, map[string]any{"messages": []any{
		map[string]any{"role": "assistant", "tool_calls": []any{
			map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{}"}},
			map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "read", "arguments": "{}"}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": output},
		map[string]any{"role": "tool", "tool_call_id": "c2", "content": output},
	}})
	p := config.DefaultPolicy()
	p.ProtectRecentTurns = 0
	if _, rep := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p}); rep.Strategies[StrategyDedupe] != nil {
		t.Fatal("dedupe ran while off")
	}
	p.DedupeReferences = true
	out, rep := New(Options{}).Compress(Input{Format: "openai", Body: body, Policy: p})
	if rep.Strategies[StrategyDedupe] == nil || rep.Strategies[StrategyDedupe].Leaves != 1 {
		t.Fatalf("report %+v", rep)
	}
	if !strings.Contains(string(out), "[identical to the earlier output of read (call c1)]") {
		t.Errorf("out = %s", out)
	}
}

// The model's own Responses items (reasoning, calls, built-in tool actions)
// are read by guardrails but never rewritten, whatever roles the policy
// selects, and encrypted content is never touched.
func TestResponsesModelItemsUntouched(t *testing.T) {
	noisy := "\u001b[31mnoisy\u001b[0m   \n\n\n\n" + strings.Repeat("x  \n", 50)
	items := []any{
		map[string]any{"type": "reasoning", "id": "rs", "summary": []any{map[string]any{"type": "summary_text", "text": noisy}}, "encrypted_content": "ENC  \n\n\n"},
		map[string]any{"type": "local_shell_call", "id": "ls", "call_id": "c1", "action": map[string]any{"type": "exec", "command": []any{noisy}}},
		map[string]any{"type": "local_shell_call_output", "call_id": "c1", "output": noisy},
		map[string]any{"type": "function_call", "name": "f", "call_id": "c2", "arguments": `{"a":  "` + strings.Repeat("y", 400) + `"}`},
		map[string]any{"type": "function_call_output", "call_id": "c2", "output": noisy},
	}
	body := mustJSON(t, map[string]any{"model": "gpt-5", "input": items})
	p := allRoles()
	p.ProtectRecentTurns = 0
	out, rep := New(Options{}).Compress(Input{Format: "openai_responses", Body: body, Policy: p})
	if rep.Outcome != OutcomeCompressed || rep.UnitsCompressed != 2 {
		t.Fatalf("report %+v", rep)
	}
	var before, after struct{ Input []json.RawMessage }
	if json.Unmarshal(body, &before) != nil || json.Unmarshal(out, &after) != nil {
		t.Fatal("not JSON")
	}
	for _, i := range []int{0, 1, 3} {
		if !bytes.Equal(before.Input[i], after.Input[i]) {
			t.Errorf("model item %d changed:\n%s\n%s", i, before.Input[i], after.Input[i])
		}
	}
	for _, i := range []int{2, 4} {
		if bytes.Equal(before.Input[i], after.Input[i]) {
			t.Errorf("tool output %d was not compressed", i)
		}
	}
}

// With compress_server_tool_results, the vendor tool results checked live
// (eac.56) are compressed like the client's; MCP connector results and the
// kinds not checked stay as sent. The policy hash changes only when it is on.
func TestServerToolResultsOptIn(t *testing.T) {
	noisy := strings.Repeat("result line   \n\n\n", 100)
	for _, tc := range []struct {
		format string
		body   map[string]any
		want   map[string]bool // ServerToolType -> compressed
	}{
		{"anthropic", map[string]any{"messages": []any{
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "server_tool_use", "id": "s1", "name": "web_fetch", "input": map[string]any{}},
				map[string]any{"type": "web_fetch_tool_result", "tool_use_id": "s1", "content": map[string]any{"type": "web_fetch_result", "url": "u",
					"content": map[string]any{"type": "document", "source": map[string]any{"type": "text", "media_type": "text/plain", "data": noisy}}}},
				map[string]any{"type": "server_tool_use", "id": "s2", "name": "bash_code_execution", "input": map[string]any{}},
				map[string]any{"type": "bash_code_execution_tool_result", "tool_use_id": "s2", "content": map[string]any{"type": "bash_code_execution_result", "stdout": noisy}},
				map[string]any{"type": "server_tool_use", "id": "s3", "name": "code_execution", "input": map[string]any{}},
				map[string]any{"type": "code_execution_tool_result", "tool_use_id": "s3", "content": map[string]any{"type": "code_execution_result", "stdout": noisy}},
				map[string]any{"type": "mcp_tool_use", "id": "m1", "name": "x", "server_name": "s", "input": map[string]any{}},
				map[string]any{"type": "mcp_tool_result", "tool_use_id": "m1", "content": []any{map[string]any{"type": "text", "text": noisy}}},
			}},
			map[string]any{"role": "user", "content": "next"},
		}}, map[string]bool{"web_fetch_tool_result": true, "bash_code_execution_tool_result": true, "code_execution_tool_result": false, "mcp_tool_result": false}},
		{"openai_responses", map[string]any{"input": []any{
			map[string]any{"type": "file_search_call", "id": "fs", "queries": []any{"q"}, "results": []any{map[string]any{"text": noisy}}},
			map[string]any{"type": "code_interpreter_call", "id": "ci", "code": "x", "outputs": []any{map[string]any{"type": "logs", "logs": noisy}}},
			map[string]any{"type": "mcp_call", "id": "mc", "name": "n", "arguments": "{}", "output": noisy},
			map[string]any{"role": "user", "content": "next"},
		}}, map[string]bool{"file_search_call": true, "code_interpreter_call": true, "mcp_call": false}},
	} {
		t.Run(tc.format, func(t *testing.T) {
			body := mustJSON(t, tc.body)
			p := config.DefaultPolicy()
			p.ProtectRecentTurns = 0
			off := p.Hash()
			if _, rep := New(Options{}).Compress(Input{Format: tc.format, Body: body, Policy: p}); rep.Outcome == OutcomeCompressed {
				t.Fatalf("compressed a vendor result with the switch off: %+v", rep)
			}
			on := config.DefaultPolicy()
			on.ProtectRecentTurns, on.CompressServerToolResults = 0, true
			if on.Hash() == off {
				t.Error("the switch does not change the policy hash")
			}
			out, rep := New(Options{}).Compress(Input{Format: tc.format, Body: body, Policy: on})
			if rep.Outcome != OutcomeCompressed {
				t.Fatalf("report %+v", rep)
			}
			units, _ := segments.ExtractUnits(tc.format, out)
			seen := map[string]bool{}
			for _, u := range units {
				want, ok := tc.want[u.ServerToolType]
				if !u.ServerTool || !ok {
					continue
				}
				seen[u.ServerToolType] = true
				if changed := !strings.Contains(u.Text, noisy); changed != want {
					t.Errorf("%s: compressed %v, want %v", u.ServerToolType, changed, want)
				}
			}
			if len(seen) != len(tc.want) {
				t.Errorf("saw %v, want every one of %v", seen, tc.want)
			}
		})
	}
}

// Tool results the vendor produced are never selected; the client's own are.
func TestServerToolResultsNotSelected(t *testing.T) {
	noisy := strings.Repeat("result line   \n\n\n", 100)
	for _, tc := range []struct {
		format string
		body   map[string]any
		client string // text of the one client tool result
	}{
		{"anthropic", map[string]any{"messages": []any{
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "server_tool_use", "id": "s1", "name": "web_search", "input": map[string]any{}},
				map[string]any{"type": "web_search_tool_result", "tool_use_id": "s1", "content": []any{map[string]any{"type": "web_search_result", "title": noisy, "url": "u", "encrypted_content": "E"}}},
				map[string]any{"type": "server_tool_use", "id": "s2", "name": "code_execution", "input": map[string]any{}},
				map[string]any{"type": "code_execution_tool_result", "tool_use_id": "s2", "content": map[string]any{"type": "code_execution_result", "stdout": noisy}},
				map[string]any{"type": "mcp_tool_use", "id": "m1", "name": "x", "server_name": "s", "input": map[string]any{}},
				map[string]any{"type": "mcp_tool_result", "tool_use_id": "m1", "content": []any{map[string]any{"type": "text", "text": noisy}}},
				map[string]any{"type": "tool_use", "id": "c1", "name": "shell", "input": map[string]any{}},
			}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "c1", "content": noisy + "client"}}},
		}}, noisy + "client"},
		{"openai_responses", map[string]any{"input": []any{
			map[string]any{"type": "file_search_call", "id": "fs", "queries": []any{"q"}, "results": []any{map[string]any{"text": noisy}}},
			map[string]any{"type": "code_interpreter_call", "id": "ci", "code": "x", "outputs": []any{map[string]any{"type": "logs", "logs": noisy}}},
			map[string]any{"type": "function_call", "name": "shell", "call_id": "c1", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": "c1", "output": noisy + "client"},
		}}, noisy + "client"},
	} {
		t.Run(tc.format, func(t *testing.T) {
			body := mustJSON(t, tc.body)
			p := config.DefaultPolicy()
			p.ProtectRecentTurns = 0
			out, rep := New(Options{}).Compress(Input{Format: tc.format, Body: body, Policy: p})
			if rep.Outcome != OutcomeCompressed || rep.UnitsCompressed != 1 || rep.ServerToolSkipped == 0 {
				t.Fatalf("report %+v", rep)
			}
			units, _ := segments.ExtractUnits(tc.format, out)
			for _, u := range units {
				if u.ServerTool && u.Text != noisy && !strings.Contains(u.Text, noisy) {
					t.Errorf("server tool result %s was rewritten: %q", u.ToolCallID, u.Text)
				}
				if !u.ServerTool && u.Kind == "tool_result" && u.Text == tc.client {
					t.Error("the client tool result was not compressed")
				}
			}
		})
	}
}

// The post-check rejects a rewrite that breaks the body, returning the
// original.
func TestPostCheckRejects(t *testing.T) {
	parse := func(b []byte) *segments.Doc {
		t.Helper()
		d, err := segments.Parse("openai", b)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	body := []byte(`{"messages":[{"role":"tool","tool_call_id":"c","content":"a"}]}`)
	if err := postCheck(parse(body), parse(body), []segments.Edit{{Path: "/messages/0/content", Old: "a", New: "b"}}); !errors.Is(err, errPostCheck) {
		t.Errorf("a leaf that does not read back passed: %v", err)
	}
	if err := postCheck(parse(body), parse(append(body, ' ')), nil); !errors.Is(err, errPostCheck) {
		t.Errorf("a larger body passed: %v", err)
	}
	paired := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c","function":{"name":"f"}}]},{"role":"tool","tool_call_id":"c","content":"a"}]}`)
	broken := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c","function":{"name":"f"}}]},{"role":"tool","tool_call_id":"d","content":"a"}]}`)
	if err := postCheck(parse(paired), parse(broken), nil); !errors.Is(err, errPostCheck) {
		t.Errorf("lost pairing passed: %v", err)
	}
}

func TestEstimator(t *testing.T) {
	for _, tc := range []struct{ format, model, want string }{
		{"openai", "gpt-4o", EstimatorO200k},
		{"openai_responses", "gpt-5-codex", EstimatorO200k},
		{"openai", "gpt-3.5-turbo", EstimatorCL100k},
		{"openai", "gpt-4", EstimatorCL100k},
		{"ollama", "llama3", EstimatorChars},
		{"anthropic", "claude-sonnet-4-5", EstimatorChars},
		{"bedrock", "openai.gpt-oss-120b", EstimatorChars},
	} {
		e := EstimatorFor(tc.format, tc.model)
		if e.Name() != tc.want {
			t.Errorf("%s %s: %s, want %s", tc.format, tc.model, e.Name(), tc.want)
		}
		if n := e.Count("hello world, this is a test"); n <= 0 || n > 20 {
			t.Errorf("%s: count %d", e.Name(), n)
		}
	}
}

func TestMemoIsBounded(t *testing.T) {
	m := newMemo(64 << 10)
	for i := 0; i < 1000; i++ {
		text := fmt.Sprintf("%d %s", i, strings.Repeat("x", 1000))
		m.put(newMemoKey("p", text), memoValue{Text: text})
	}
	if m.bytes > m.maxBytes || m.len() == 0 || m.len() > 64 {
		t.Errorf("memo holds %d entries, %d bytes", m.len(), m.bytes)
	}
	k := newMemoKey("p", "big")
	m.put(k, memoValue{Text: strings.Repeat("y", 32<<10)})
	if _, ok := m.get(k); ok {
		t.Error("an entry over a quarter of the memo was kept")
	}
	// Length prefixes keep "ab"+"c" and "a"+"bc" apart.
	if newMemoKey("ab", "c") == newMemoKey("a", "bc") {
		t.Error("memo keys collide")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
