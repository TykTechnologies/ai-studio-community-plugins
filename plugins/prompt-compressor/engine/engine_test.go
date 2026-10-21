package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/internal/corpus"
	"github.com/TykTechnologies/midsommar/v2/guardrails/segments"
)

// fakeEngine is a Headroom- or LLMLingua-2-compatible server whose
// behaviour a test switches.
type fakeEngine struct {
	*httptest.Server
	mu       sync.Mutex
	mode     string // ok, slow, 500, 400, malformed, longer, drop, redirect
	calls    atomic.Int32
	bodies   []string
	auth     []string
	sequence atomic.Int32 // makes answers differ per call (not deterministic)
}

func newFakeEngine(t *testing.T) *fakeEngine {
	f := &fakeEngine{mode: "ok"}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeEngine) set(mode string) {
	f.mu.Lock()
	f.mode = mode
	f.mu.Unlock()
}

func (f *fakeEngine) serve(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	raw, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	mode := f.mode
	f.bodies = append(f.bodies, string(raw))
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()
	var in struct {
		Messages []struct{ Content string } `json:"messages"`
		Context  []string                   `json:"context"`
		Force    []string                   `json:"force_tokens"`
	}
	_ = json.Unmarshal(raw, &in)
	text := ""
	if len(in.Messages) == 1 {
		text = in.Messages[0].Content
	} else if len(in.Context) == 1 {
		text = in.Context[0]
	}
	out := fmt.Sprintf("%.*s#%d", len(text)/2, text, f.sequence.Add(1))
	for _, tok := range in.Force {
		out += " " + tok
	}
	switch mode {
	case "slow":
		time.Sleep(300 * time.Millisecond)
	case "500":
		w.WriteHeader(500)
		return
	case "400":
		w.WriteHeader(400)
		return
	case "malformed":
		w.Write([]byte(`{"messages":"nope"`))
		return
	case "longer":
		out = text + text
	case "drop":
		out = "x"
	case "redirect":
		http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
		return
	case "huge":
		// A usable answer behind 1 MiB of padding: read in full it would be
		// accepted.
		json.NewEncoder(w).Encode(map[string]any{"padding": strings.Repeat("p", 1<<20),
			"messages": []any{map[string]string{"role": "tool", "content": out}}})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/compressPrompt") {
		json.NewEncoder(w).Encode(map[string]any{"compressed_prompt": out})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"messages": []any{map[string]string{"role": "tool", "content": out}}, "tokens_saved": 1})
}

// mapKV is an in-memory KV.
type mapKV struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (k *mapKV) Read(ctx context.Context, key string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (k *mapKV) Write(ctx context.Context, key string, v []byte, exp *time.Time) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string][]byte{}
	}
	_, ok := k.m[key]
	k.m[key] = v
	return !ok, nil
}

func (k *mapKV) len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.m)
}

// logs records log lines as text.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) log(msg string, fields ...interface{}) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprint(append([]interface{}{msg}, fields...)...))
	l.mu.Unlock()
}

func (l *logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

const secretToken = "tok-very-secret"

func client(f *fakeEngine, kind string) *Client {
	return New(config.EngineRef{Name: "main", Kind: kind, TimeoutMs: 150}, config.EngineCredentials{URL: f.URL, Token: secretToken})
}

var job = compress.EngineJob{Role: "tool", Text: strings.Repeat("build output line\n", 20)}

func run(t *testing.T, c *Client, kv KV, l *logs, mode config.FailureMode, jobs ...compress.EngineJob) ([]compress.EngineResult, error) {
	t.Helper()
	var lg Logger
	if l != nil {
		lg = l.log
	}
	return c.Run(context.Background(), kv, lg, "hash1", mode)("gpt-4o", jobs)
}

func TestEngineOutcomes(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		failMode  config.FailureMode
		wantErr   bool
		changed   bool
		failed    bool
		wantErrIs error
	}{
		{"ok", config.FailOpen, false, true, false, nil},
		{"slow", config.FailOpen, false, false, true, nil},
		{"500", config.FailOpen, false, false, true, nil},
		{"400", config.FailOpen, false, false, true, nil},
		{"malformed", config.FailOpen, false, false, true, nil},
		{"longer", config.FailOpen, false, false, false, nil},
		{"redirect", config.FailOpen, false, false, true, nil},
		{"huge", config.FailOpen, false, false, true, nil}, // read up to 2x the job + 64 KiB (eac.77)
		{"huge", config.FailClosed, true, false, false, ErrMalformed},
		{"slow", config.FailClosed, true, false, false, ErrTimeout},
		{"500", config.FailClosed, true, false, false, ErrStatus},
		{"malformed", config.FailClosed, true, false, false, ErrMalformed},
		{"longer", config.FailClosed, false, false, false, nil}, // a rejected answer is not a failure
	} {
		t.Run(tc.mode+"/"+string(tc.failMode), func(t *testing.T) {
			f := newFakeEngine(t)
			f.set(tc.mode)
			l := &logs{}
			res, err := run(t, client(f, config.EngineHeadroom), nil, l, tc.failMode, job)
			if (err != nil) != tc.wantErr || (tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs)) {
				t.Fatalf("err %v", err)
			}
			if err == nil && (res[0].Changed != tc.changed || res[0].Failed != tc.failed) {
				t.Errorf("result %+v", res[0])
			}
			if err == nil && !tc.changed && res[0].Text != job.Text {
				t.Error("a passthrough changed the text")
			}
			for _, s := range []string{"pw-secret", secretToken, f.URL} {
				if strings.Contains(l.String(), s) || (err != nil && strings.Contains(err.Error(), s)) {
					t.Errorf("logs or error carry %q: %s / %v", s, l.String(), err)
				}
			}
		})
	}
}

func TestAnswerLimit(t *testing.T) {
	for n, want := range map[int]int{0: 64 << 10, 1 << 10: 66 << 10, 1 << 20: 2<<20 + 64<<10, 16 << 20: maxResponseBytes} {
		if got := answerLimit(n); got != want {
			t.Errorf("answerLimit(%d) = %d, want %d", n, got, want)
		}
	}
}

// A memo hit returns the same bytes with the engine down, and a failure in
// open mode is memoised as a passthrough that a later success cannot undo.
func TestEngineMemo(t *testing.T) {
	f := newFakeEngine(t)
	kv := &mapKV{}
	c := client(f, config.EngineHeadroom)
	first, _ := run(t, c, kv, nil, config.FailOpen, job)
	f.set("500")
	again, _ := run(t, c, kv, nil, config.FailOpen, job)
	if !first[0].Changed || again[0] != first[0] {
		t.Fatalf("memo hit differs: %+v vs %+v", first[0], again[0])
	}
	// A new process finds it in KV.
	time.Sleep(50 * time.Millisecond) // the KV write is in the background
	fresh := client(f, config.EngineHeadroom)
	fromKV, _ := run(t, fresh, kv, nil, config.FailOpen, job)
	if fromKV[0] != first[0] || fresh.Stats.KVHits.Load() != 1 {
		t.Errorf("KV hit differs: %+v (%d KV hits)", fromKV[0], fresh.Stats.KVHits.Load())
	}
	kv.mu.Lock()
	for k, v := range kv.m {
		for _, secret := range []string{secretToken, "pw-secret", "build output line"} {
			if strings.Contains(k, secret) || (secret != "build output line" && strings.Contains(string(v), secret)) {
				t.Errorf("KV entry %s carries %q", k, secret)
			}
		}
	}
	kv.mu.Unlock()

	other := compress.EngineJob{Role: "tool", Text: strings.Repeat("another output\n", 20)}
	failed, _ := run(t, c, kv, nil, config.FailOpen, other)
	f.set("ok")
	later, _ := run(t, c, kv, nil, config.FailOpen, other)
	if !failed[0].Failed || later[0].Changed || later[0].Text != other.Text {
		t.Errorf("a later success rewrote a passthrough: %+v then %+v", failed[0], later[0])
	}
}

func TestEngineCredentials(t *testing.T) {
	f := newFakeEngine(t)
	run(t, client(f, config.EngineHeadroom), nil, nil, config.FailOpen, job)
	for _, token := range []string{RefusedCredential, "$SECRET/engine_token", ""} {
		c := New(config.EngineRef{Name: "main", Kind: config.EngineHeadroom, TimeoutMs: 150}, config.EngineCredentials{URL: f.URL, Token: token})
		run(t, c, nil, nil, config.FailOpen, compress.EngineJob{Role: "tool", Text: strings.Repeat(token+"z", 50)})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.auth[0] != "Bearer "+secretToken {
		t.Errorf("token not sent: %q", f.auth[0])
	}
	for _, a := range f.auth[1:] {
		if a != "" {
			t.Errorf("a refused or empty token was sent: %q", a)
		}
	}
	for _, raw := range []string{"", "ftp://x", "$SECRET/engine_url", RefusedCredential, "/relative"} {
		c := New(config.EngineRef{Name: "main", Kind: config.EngineHeadroom}, config.EngineCredentials{URL: raw})
		if _, err := run(t, c, nil, nil, config.FailClosed, job); !errors.Is(err, ErrNoURL) {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

// LLMLingua-2 gets the protect matches as force tokens, and an answer that
// drops one is not taken.
func TestLLMLinguaForceTokens(t *testing.T) {
	f := newFakeEngine(t)
	c := client(f, config.EngineLLMLingua2)
	j := compress.EngineJob{Role: "tool", Text: strings.Repeat("noise ", 100) + "LED-1234", Protect: []string{"LED-1234"}}
	res, _ := run(t, c, nil, nil, config.FailOpen, j)
	f.mu.Lock()
	body := f.bodies[0]
	f.mu.Unlock()
	if !strings.Contains(body, `"force_tokens":["LED-1234"]`) || !res[0].Changed || !strings.Contains(res[0].Text, "LED-1234") {
		t.Errorf("body %s, result %+v", body, res[0])
	}
	f.set("drop")
	j.Text += " more"
	res, _ = run(t, c, nil, nil, config.FailOpen, j)
	if res[0].Changed {
		t.Error("an answer without a protected token was taken")
	}
}

// openaiBody builds a chat body with a system prompt, an old tool result
// and the newest turn.
func openaiBody(system, old, newest string) []byte {
	b, _ := json.Marshal(map[string]any{"model": "gpt-4o", "messages": []any{
		map[string]any{"role": "system", "content": system},
		map[string]any{"role": "user", "content": "go"},
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "shell", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": old},
		map[string]any{"role": "assistant", "content": "ok"},
		map[string]any{"role": "user", "content": newest},
	}})
	return b
}

func enginePolicy() *config.Policy {
	p := config.DefaultPolicy()
	p.Level, p.MinUnitTokens, p.Roles = 1, 0, []string{config.RoleTool, config.RoleUser, config.RoleSystem}
	p.Engine = &config.EngineRef{Name: "main", Kind: config.EngineHeadroom, TimeoutMs: 150}
	return p
}

// The system prompt, and the newest (protected) turn, are never sent.
func TestSystemPromptNeverSent(t *testing.T) {
	f := newFakeEngine(t)
	c := client(f, config.EngineHeadroom)
	p := enginePolicy()
	body := openaiBody("SYSTEM-CANARY "+strings.Repeat("rules ", 100), strings.Repeat("tool output line\n", 50), "NEWEST-CANARY "+strings.Repeat("please ", 100))
	out, rep := compress.New(compress.Options{}).Compress(compress.Input{Format: "openai", Body: body, Policy: p,
		Engine: c.Run(context.Background(), nil, nil, p.Hash(), config.FailOpen)})
	f.mu.Lock()
	sent := strings.Join(f.bodies, "\n")
	f.mu.Unlock()
	if strings.Contains(sent, "SYSTEM-CANARY") || strings.Contains(sent, "NEWEST-CANARY") {
		t.Errorf("sent to the engine: %s", sent)
	}
	if rep.Outcome != compress.OutcomeCompressed || rep.EngineUnits < 1 || rep.Strategies["engine_headroom"] == nil {
		t.Errorf("report %+v", rep)
	}
	if !bytes.Contains(out, []byte("SYSTEM-CANARY")) {
		t.Error("the system prompt was changed")
	}
}

// Closed mode fails the body; open mode never does.
func TestCompressFailureModes(t *testing.T) {
	f := newFakeEngine(t)
	f.set("500")
	p := enginePolicy()
	body := openaiBody("sys", strings.Repeat("tool output line   \n\n", 50), "next")
	for _, mode := range []config.FailureMode{config.FailOpen, config.FailClosed} {
		c := client(f, config.EngineHeadroom)
		out, rep := compress.New(compress.Options{}).Compress(compress.Input{Format: "openai", Body: body, Policy: p,
			Engine: c.Run(context.Background(), nil, nil, p.Hash(), mode)})
		switch mode {
		case config.FailOpen:
			if rep.Outcome != compress.OutcomeCompressed || rep.EngineFailed < 1 || bytes.Equal(out, body) {
				t.Errorf("open: %+v", rep)
			}
		case config.FailClosed:
			if rep.Outcome != compress.OutcomeEngineFailed || !bytes.Equal(out, body) {
				t.Errorf("closed: %+v", rep)
			}
		}
	}
}

// Corpus sessions keep their earlier messages byte-identical turn after
// turn with a non-deterministic engine that goes down mid-session: the memo
// holds what was sent, and a failure's passthrough holds too.
func TestSessionsCacheStableWithEngine(t *testing.T) {
	m, err := corpus.Load(corpus.Dir())
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeEngine(t)
	p := config.DefaultPolicy()
	p.Level = 2
	p.Engine = &config.EngineRef{Name: "main", Kind: config.EngineHeadroom, TimeoutMs: 500}
	cmp := compress.New(compress.Options{})
	sessions, engineChanged := 0, 0
	for _, s := range m.Sessions {
		if s.History != "full" || !segments.Supports(s.Format) {
			continue
		}
		sessions++
		c := client(f, config.EngineHeadroom)
		kv := &mapKV{}
		bodies := s.Bodies()
		var prev []json.RawMessage
		prevNewest := 0
		for k, body := range bodies {
			f.set("ok")
			if k > len(bodies)/3 && k < 2*len(bodies)/3 {
				f.set("500") // the outage
			}
			out, rep := cmp.Compress(compress.Input{Format: s.Format, Body: body, Policy: p,
				Engine: c.Run(context.Background(), kv, nil, p.Hash(), config.FailOpen)})
			if rep.Outcome == compress.OutcomeRejected || rep.Outcome == compress.OutcomeEngineFailed {
				t.Fatalf("%s turn %d: %s", s.Name, k, rep.Outcome)
			}
			if rep.Strategies["engine_headroom"] != nil {
				engineChanged++
			}
			cur := conversation(t, out)
			for i := 0; prev != nil && i < prevNewest; i++ {
				if !bytes.Equal(prev[i], cur[i]) {
					t.Fatalf("%s turn %d: message %d differs from turn %d's", s.Name, k, i, k-1)
				}
			}
			prev, prevNewest = cur, newestStart(t, s.Format, out, len(cur))
		}
	}
	if sessions == 0 || engineChanged == 0 {
		t.Fatalf("%d sessions, engine changed %d bodies", sessions, engineChanged)
	}
}

func conversation(t *testing.T, body []byte) []json.RawMessage {
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
	_ = json.Unmarshal(b.Input, &items)
	return items
}

// newestStart is the first message of the body's newest turn (protected
// at level 1, so it changes once a newer turn follows).
func newestStart(t *testing.T, format string, body []byte, n int) int {
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
