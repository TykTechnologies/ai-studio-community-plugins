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
func TestSessionsCacheStable(t *testing.T) {
	c := New(Options{})
	for _, s := range loadCorpus(t).Sessions {
		if s.History != "full" || !segments.Supports(s.Format) {
			continue
		}
		t.Run(s.Name, func(t *testing.T) {
			var prev []json.RawMessage
			saved := 0
			for k, body := range s.Bodies() {
				out, rep := c.Compress(Input{Format: s.Format, Body: body, Policy: config.DefaultPolicy()})
				if rep.Outcome == OutcomeRejected || rep.Outcome == OutcomeSkipped {
					t.Fatalf("turn %d: %s (%s)", k, rep.Outcome, rep.Reason)
				}
				saved += rep.TokensSaved()
				cur := conversation(t, s.Format, out)
				if prev != nil {
					if len(cur) < len(prev) {
						t.Fatalf("turn %d has fewer messages than turn %d", k, k-1)
					}
					for i := 0; i < len(prev)-1; i++ {
						if !bytes.Equal(prev[i], cur[i]) {
							t.Fatalf("turn %d: message %d differs from turn %d's:\n%s\nvs\n%s", k, i, k-1, prev[i], cur[i])
						}
					}
				}
				prev = cur
			}
			if saved == 0 {
				t.Error("the session saved nothing")
			}
		})
	}
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

// The post-check rejects a rewrite that breaks the body, returning the
// original.
func TestPostCheckRejects(t *testing.T) {
	body := []byte(`{"messages":[{"role":"tool","tool_call_id":"c","content":"a"}]}`)
	if err := postCheck("openai", body, body, []segments.Edit{{Path: "/messages/0/content", Old: "a", New: "b"}}); !errors.Is(err, errPostCheck) {
		t.Errorf("a leaf that does not read back passed: %v", err)
	}
	if err := postCheck("openai", body, append(body, ' '), nil); !errors.Is(err, errPostCheck) {
		t.Errorf("a larger body passed: %v", err)
	}
	paired := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c","function":{"name":"f"}}]},{"role":"tool","tool_call_id":"c","content":"a"}]}`)
	broken := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c","function":{"name":"f"}}]},{"role":"tool","tool_call_id":"d","content":"a"}]}`)
	if err := postCheck("openai", paired, broken, nil); !errors.Is(err, errPostCheck) {
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
