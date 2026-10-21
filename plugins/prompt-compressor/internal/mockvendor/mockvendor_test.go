package mockvendor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func oracle(t *testing.T) map[string]Task {
	t.Helper()
	var tasks map[string]Task
	if err := json.Unmarshal([]byte(`{
		"qa-1": {"evidence": ["settles at 17:00 \"CET\"", "EUR"], "answer": {"text": "17:00"}, "wrong": {"text": "I don't know"}},
		"call-1": {"evidence": ["inv_42"], "answer": {"call": {"name": "refund", "arguments": {"invoice": "inv_42"}}}, "wrong": {"text": "no call"}},
		"agent-1": {"evidence": ["FAIL: TestRound"], "steps": [{"name": "bash", "arguments": {"command": "go test ./..."}}, {"name": "read_file", "arguments": {"path": "round.go"}}],
			"answer": {"text": "patch: use HalfEven"}, "wrong": {"text": "cannot tell"}}
	}`), &tasks); err != nil {
		t.Fatal(err)
	}
	return tasks
}

func TestAnswerFor(t *testing.T) {
	tasks := oracle(t)
	cases := []struct {
		name, body string
		ok         bool
		text, call string
	}{
		{"evidence as sent", `[task:qa-1] settles at 17:00 "CET" in EUR`, true, "17:00", ""},
		{"evidence JSON-escaped in the body", `{"content":"[task:qa-1] settles at 17:00 \"CET\" in EUR"}`, true, "17:00", ""},
		{"evidence cut", `{"content":"[task:qa-1] [tyk-compressed: 40 lines] EUR"}`, true, "I don't know", ""},
		{"a call answer", `[task:call-1] refund inv_42`, true, "", "refund"},
		{"agent, first request", `{"messages":[{"role":"user","content":"[task:agent-1] fix it"}]}`, true, "", "bash"},
		{"agent, one tool result", `{"messages":[{"role":"user","content":"[task:agent-1]"},{"role":"tool","content":"FAIL: TestRound"}]}`, true, "", "read_file"},
		{"agent, done, evidence kept", `{"messages":[{"role":"user","content":"[task:agent-1]"},{"role":"tool","content":"FAIL: TestRound"},{"role": "tool","content":"x"}]}`, true, "patch: use HalfEven", ""},
		{"agent, done, evidence cut", `{"messages":[{"role":"user","content":"[task:agent-1]"},{"type":"tool_result","content":"[tyk-compressed]"},{"type":"tool_result","content":"x"}]}`, true, "cannot tell", ""},
		{"unknown task", `[task:nope] anything`, false, "", ""},
		{"no task", `plain request`, false, "", ""},
	}
	for _, c := range cases {
		a, ok := answerFor(tasks, []byte(c.body))
		name := ""
		if a.Call != nil {
			name = a.Call.Name
		}
		if ok != c.ok || a.Text != c.text || name != c.call {
			t.Errorf("%s: got ok=%v text=%q call=%q, want ok=%v text=%q call=%q", c.name, ok, a.Text, name, c.ok, c.text, c.call)
		}
	}
}

// Every response says it came from the mock, and an oracle call reaches the
// client as a tool call in the request's format.
func TestHandlerMarksResponsesAndAnswersCalls(t *testing.T) {
	srv := httptest.NewServer(New(Options{Oracle: oracle(t)}))
	t.Cleanup(srv.Close)
	post := func(path, body string) (*http.Response, string) {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, string(b)
	}
	resp, body := post("/v1/chat/completions", `{"messages":[{"role":"user","content":"[task:call-1] refund inv_42"}]}`)
	if resp.Header.Get("X-PC-Mock") != "oracle" || !strings.Contains(body, `"name":"refund"`) || !strings.Contains(body, `inv_42`) {
		t.Errorf("openai: X-PC-Mock %q, body %s", resp.Header.Get("X-PC-Mock"), body)
	}
	resp, body = post("/v1/messages", `{"messages":[{"role":"user","content":"[task:call-1] refund inv_42"}]}`)
	if resp.Header.Get("X-PC-Mock") != "oracle" || !strings.Contains(body, `"type":"tool_use"`) || !strings.Contains(body, `"invoice":"inv_42"`) {
		t.Errorf("anthropic: X-PC-Mock %q, body %s", resp.Header.Get("X-PC-Mock"), body)
	}
	echo := httptest.NewServer(New(Options{}))
	t.Cleanup(echo.Close)
	r, err := http.Post(echo.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.Header.Get("X-PC-Mock") != "echo" {
		t.Errorf("echo mode: X-PC-Mock %q", r.Header.Get("X-PC-Mock"))
	}
}
