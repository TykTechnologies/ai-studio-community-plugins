package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// MockHeader is the header the mock upstream (internal/mockvendor) sets on
// every answer, MockOracle when it answers from an oracle file. A dry run
// requires MockOracle before sending a task, so a slug bound to a real
// vendor (or to the mock without an oracle) cannot spend or mislead.
const (
	MockHeader = "X-PC-Mock"
	MockOracle = "oracle"
)

// Gateway is where a live run sends its requests: a Microgateway (or
// CompressingGateway), with one LLM slug per configuration, each bound to
// a policy at that level (0: compression off).
type Gateway struct {
	BaseURL string
	// Secret is the App credential the gateway checks.
	Secret string
	Slugs  map[int]string
	Client *http.Client
}

// maxAnswerBytes bounds one answer read.
const maxAnswerBytes = 16 << 20

// url is the gateway URL of a configuration's LLM for a request format.
func (g Gateway) url(level int, format string) (string, error) {
	slug, ok := g.Slugs[level]
	if !ok || slug == "" {
		return "", fmt.Errorf("no LLM slug for level %d", level)
	}
	path := "/v1/chat/completions"
	if format == FormatAnthropic {
		path = "/v1/messages"
	}
	return strings.TrimRight(g.BaseURL, "/") + "/llm/rest/" + slug + path, nil
}

// send posts body and reads the answer.
func (g Gateway) send(ctx context.Context, level int, format string, body []byte) (reply, http.Header, error) {
	u, err := g.url(level, format)
	if err != nil {
		return reply{}, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return reply{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+g.Secret)
		req.Header.Set("x-api-key", g.Secret)
	}
	if format == FormatAnthropic {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	c := g.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return reply{}, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswerBytes))
	if err != nil {
		return reply{}, resp.Header, err
	}
	if resp.StatusCode/100 != 2 {
		return reply{}, resp.Header, fmt.Errorf("status %d: %s", resp.StatusCode, snippet(raw))
	}
	r, err := parseReply(format, raw)
	return r, resp.Header, err
}

// ErrNotMock: a dry run found a configuration whose upstream is not the
// oracle mock.
var ErrNotMock = errors.New("eval: dry run, but the upstream behind a slug is not the oracle mock (no " + MockHeader + ": " + MockOracle + " header); bind every slug to the mock with an oracle, or approve a paid run")

// Preflight sends one tiny request per configuration and, for a dry run,
// requires the mock's header on each answer: a misconfigured slug costs at
// most that one request.
func (g Gateway) Preflight(ctx context.Context, format string, dryRun bool) error {
	body := []byte(`{"model":"preflight","max_tokens":1,"messages":[{"role":"user","content":"preflight"}]}`)
	levels := make([]int, 0, len(g.Slugs))
	for l := range g.Slugs {
		levels = append(levels, l)
	}
	sort.Ints(levels)
	for _, l := range levels {
		_, h, err := g.send(ctx, l, format, body)
		if dryRun && (h == nil || h.Get(MockHeader) != MockOracle) {
			return fmt.Errorf("level %d (%s): %w", l, g.Slugs[l], ErrNotMock)
		}
		if err != nil {
			return fmt.Errorf("level %d (%s): %w", l, g.Slugs[l], err)
		}
	}
	return nil
}

// TaskResult is one task under one configuration and seed.
type TaskResult struct {
	Task      string  `json:"task"`
	Seed      uint64  `json:"seed"`
	Level     int     `json:"level"`
	Score     float64 `json:"score"`
	EM        float64 `json:"em,omitempty"`
	Malformed bool    `json:"malformed,omitempty"`
	Turns     int     `json:"turns"`
	Usage     Usage   `json:"usage"`
	// Error is why the task did not finish; it scores 0.
	Error   string        `json:"error,omitempty"`
	Elapsed time.Duration `json:"elapsed_ns"`
}

// defaultAgentTurns bounds an agent task that names no max_turns.
const defaultAgentTurns = 40

// Run runs every task of the plan under every configuration and seed,
// concurrency at a time, and returns the results in plan order (task,
// seed, level).
func Run(ctx context.Context, g Gateway, pl Plan, tasks map[string]Task, concurrency int) ([]TaskResult, error) {
	for _, id := range pl.Tasks {
		if _, ok := tasks[id]; !ok {
			return nil, fmt.Errorf("plan task %q is not in the task file", id)
		}
	}
	type job struct {
		i     int
		task  Task
		seed  uint64
		level int
	}
	var jobs []job
	for _, id := range pl.Tasks {
		for _, s := range pl.Seeds {
			for _, l := range pl.Levels {
				jobs = append(jobs, job{len(jobs), tasks[id], s, l})
			}
		}
	}
	out := make([]TaskResult, len(jobs))
	ch := make(chan job)
	var wg sync.WaitGroup
	for range max(concurrency, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				res := runTask(ctx, g, pl.Suite.Name, j.task, j.level)
				res.Seed = j.seed
				out[j.i] = res
			}
		}()
	}
	for _, j := range jobs {
		select {
		case ch <- j:
		case <-ctx.Done():
		}
	}
	close(ch)
	wg.Wait()
	return out, ctx.Err()
}

// runTask runs one task: one request, or for an agent task the loop of
// requests, scripted tool outputs and growing history, until the model
// stops calling tools or the turn limit.
func runTask(ctx context.Context, g Gateway, suite string, t Task, level int) TaskResult {
	res := TaskResult{Task: t.ID, Level: level}
	start := time.Now()
	defer func() { res.Elapsed = time.Since(start) }()
	agent := suite == "swebench"
	maxTurns := 1
	if agent {
		maxTurns = t.MaxTurns
		if maxTurns <= 0 {
			maxTurns = defaultAgentTurns
		}
	}
	body := []byte(t.Request)
	outputs := t.ToolOutputs
	var last reply
	for res.Turns < maxTurns {
		r, _, err := g.send(ctx, level, t.Format, body)
		res.Turns++
		if err != nil {
			res.Error = err.Error()
			return res
		}
		res.Usage = res.Usage.Add(r.Usage)
		last = r
		if !agent || len(r.Calls) == 0 {
			break
		}
		outs := make([]string, len(r.Calls))
		for i := range outs {
			if len(outputs) > 0 {
				outs[i], outputs = outputs[0], outputs[1:]
			} else {
				outs[i] = "error: the tool produced no output"
			}
		}
		if body, err = appendTurn(t.Format, body, r, outs); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	var s score
	switch suite {
	case "bfcl":
		s = scoreCalls(t, last)
	case "lcqa":
		s = scoreAnswer(t, last)
	default:
		s = scoreResolved(t, last)
	}
	res.Score, res.EM, res.Malformed = s.Value, s.EM, s.Malformed
	return res
}
