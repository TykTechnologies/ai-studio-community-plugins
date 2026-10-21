package eval

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Task is one live-suite task, one line of testdata/live/<suite>/tasks.jsonl
// (the contract with the fixtures, eac.23). A single-request task (BFCL,
// long-context QA) is Request and Expect; an agent task (SWE-bench-shaped)
// also has ToolOutputs, replayed in order for the tool calls the model
// makes, and MaxTurns.
type Task struct {
	ID string `json:"id"`
	// Format is the vendor shape of Request and of the answers: openai
	// (Chat Completions) or anthropic (Messages).
	Format      string          `json:"format"`
	Request     json.RawMessage `json:"request"`
	ToolOutputs []string        `json:"tool_outputs,omitempty"`
	MaxTurns    int             `json:"max_turns,omitempty"`
	Expect      Expect          `json:"expect"`
}

// Expect is what scores a task. Calls (BFCL): the tool calls the answer
// must make. Answers (QA): acceptable answers, scored by EM and F1.
// ResolvedIf (agent): a substring the final answer must contain.
type Expect struct {
	Calls      []ExpectedCall `json:"calls,omitempty"`
	Answers    []string       `json:"answers,omitempty"`
	ResolvedIf string         `json:"resolved_if,omitempty"`
}

// ExpectedCall is one expected tool call.
type ExpectedCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Formats the live runner speaks.
const (
	FormatOpenAI    = "openai"
	FormatAnthropic = "anthropic"
)

// maxTaskLine bounds one fixture line (a long-context document is large).
const maxTaskLine = 16 << 20

// LoadTasks reads a tasks.jsonl file. Blank lines and lines starting with
// # are skipped; a task without an id, request or known format is an error.
func LoadTasks(path string) ([]Task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), maxTaskLine)
	var tasks []Task
	seen := map[string]bool{}
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var t Task
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		switch {
		case t.ID == "" || len(t.Request) == 0:
			return nil, fmt.Errorf("%s:%d: a task needs an id and a request", path, n)
		case t.Format != FormatOpenAI && t.Format != FormatAnthropic:
			return nil, fmt.Errorf("%s:%d: format %q (openai or anthropic)", path, n, t.Format)
		case seen[t.ID]:
			return nil, fmt.Errorf("%s:%d: task %q twice", path, n, t.ID)
		}
		seen[t.ID] = true
		tasks = append(tasks, t)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, errors.New(path + ": no tasks")
	}
	return tasks, nil
}

// call is one tool call in an answer; Arguments as the answer holds them
// (a JSON string's content for OpenAI, the input object for Anthropic).
type call struct {
	ID        string
	Name      string
	Arguments string
}

// reply is what the runner reads from one answer.
type reply struct {
	Text  string
	Calls []call
	Usage Usage
	// assistant is the answer's assistant turn as the next request's
	// history holds it.
	assistant json.RawMessage
}

// parseReply reads an answer in format.
func parseReply(format string, body []byte) (reply, error) {
	var r reply
	switch format {
	case FormatOpenAI:
		var a struct {
			Choices []struct {
				Message json.RawMessage `json:"message"`
			} `json:"choices"`
			Usage struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
				Details    struct {
					Cached int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(body, &a); err != nil || len(a.Choices) == 0 {
			return r, fmt.Errorf("not a chat completion: %s", snippet(body))
		}
		var m struct {
			Content   *string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(a.Choices[0].Message, &m); err != nil {
			return r, fmt.Errorf("bad message: %w", err)
		}
		if m.Content != nil {
			r.Text = *m.Content
		}
		for _, c := range m.ToolCalls {
			r.Calls = append(r.Calls, call{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments})
		}
		cached := min(a.Usage.Details.Cached, a.Usage.Prompt)
		r.Usage = Usage{Input: a.Usage.Prompt - cached, CacheRead: cached, Output: a.Usage.Completion}
		r.assistant = a.Choices[0].Message
	case FormatAnthropic:
		var a struct {
			Content []json.RawMessage `json:"content"`
			Usage   struct {
				Input      int `json:"input_tokens"`
				Output     int `json:"output_tokens"`
				CacheWrite int `json:"cache_creation_input_tokens"`
				CacheRead  int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(body, &a); err != nil || a.Content == nil {
			return r, fmt.Errorf("not a message: %s", snippet(body))
		}
		for _, raw := range a.Content {
			var b struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if json.Unmarshal(raw, &b) != nil {
				continue
			}
			switch b.Type {
			case "text":
				r.Text += b.Text
			case "tool_use":
				r.Calls = append(r.Calls, call{ID: b.ID, Name: b.Name, Arguments: string(b.Input)})
			}
		}
		r.Usage = Usage{Input: a.Usage.Input, Output: a.Usage.Output, CacheWrite: a.Usage.CacheWrite, CacheRead: a.Usage.CacheRead}
		r.assistant, _ = json.Marshal(map[string]any{"role": "assistant", "content": a.Content})
	default:
		return r, fmt.Errorf("format %q", format)
	}
	return r, nil
}

// appendTurn returns req with the answer's assistant turn and one tool
// result per call (outputs, in order) appended to its messages, as an agent
// does before its next request.
func appendTurn(format string, req []byte, r reply, outputs []string) ([]byte, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(req, &body); err != nil {
		return nil, err
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(body["messages"], &msgs); err != nil {
		return nil, fmt.Errorf("request has no messages: %w", err)
	}
	msgs = append(msgs, r.assistant)
	switch format {
	case FormatOpenAI:
		for i, c := range r.Calls {
			m, _ := json.Marshal(map[string]any{"role": "tool", "tool_call_id": c.ID, "content": outputs[i]})
			msgs = append(msgs, m)
		}
	case FormatAnthropic:
		var blocks []any
		for i, c := range r.Calls {
			blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": c.ID, "content": outputs[i]})
		}
		m, _ := json.Marshal(map[string]any{"role": "user", "content": blocks})
		msgs = append(msgs, m)
	}
	raw, err := json.Marshal(msgs)
	if err != nil {
		return nil, err
	}
	body["messages"] = raw
	return json.Marshal(body)
}

// snippet is the start of a body, for errors.
func snippet(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}
