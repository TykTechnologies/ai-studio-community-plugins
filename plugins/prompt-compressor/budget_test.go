package main

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

func TestByteBudget(t *testing.T) {
	var b byteBudget
	if !b.acquire(10, 16) || b.acquire(7, 16) || !b.acquire(6, 16) {
		t.Fatal("acquire must take what fits and refuse what does not")
	}
	b.release(10)
	b.release(6)
	if b.inFlight.Load() != 0 {
		t.Fatalf("in flight %d after release", b.inFlight.Load())
	}
	if !b.acquire(config.DefaultMaxInFlightBytes, 0) || b.acquire(1, 0) {
		t.Fatal("limit 0 is the default budget")
	}
	b.release(config.DefaultMaxInFlightBytes)

	// Concurrent takers never hold more than the limit between them.
	var held, peak atomic.Int64
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				if b.acquire(3, 10) {
					h := held.Add(3)
					for p := peak.Load(); h > p && !peak.CompareAndSwap(p, h); p = peak.Load() {
					}
					held.Add(-3)
					b.release(3)
				}
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 10 || b.inFlight.Load() != 0 {
		t.Fatalf("peak %d, in flight %d", peak.Load(), b.inFlight.Load())
	}
}

// bigBody is a chat request of about n bytes whose tool output level 1
// compresses.
func bigBody(t *testing.T, n int) []byte {
	t.Helper()
	line := "\u001b[32mok\u001b[0m line   \n\n\n"
	b, err := json.Marshal(map[string]any{"model": "gpt-4o", "messages": []any{
		map[string]any{"role": "user", "content": "run it"},
		map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "shell", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": strings.Repeat(line, n/(len(line)+12))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Sixteen parallel ~4 MiB requests against the default 16 MiB budget: no
// more than the budget is ever being compressed, and the rest are forwarded
// unchanged with reason over_budget, without waiting (eac.78).
func TestPostAuthInFlightBudget(t *testing.T) {
	const parallel = 16
	p, base := hookPlugin(t, `{"mode":"enforce"}`)
	body := bigBody(t, 3_900_000)
	if len(body) > config.DefaultMaxBodyBytes || len(body) < 3_500_000 {
		t.Fatalf("body is %d bytes", len(body))
	}
	fit := config.DefaultMaxInFlightBytes / len(body)

	var entered, inside, peak atomic.Int64
	gate := make(chan struct{})
	p.compressFn = func(in compress.Input) ([]byte, compress.Report) {
		entered.Add(1)
		n := inside.Add(int64(len(in.Body)))
		for q := peak.Load(); n > q && !peak.CompareAndSwap(q, n); q = peak.Load() {
		}
		<-gate
		inside.Add(-int64(len(in.Body)))
		return in.Body, compress.Report{Outcome: compress.OutcomeUnchanged}
	}

	var returned atomic.Int64
	var wg sync.WaitGroup
	for i := range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer returned.Add(1)
			ctx := base
			ctx.RequestID = "req-" + strconv.Itoa(i)
			resp, err := p.HandlePostAuth(ctx, request(body, nil))
			if err != nil || resp.Modified || resp.Block {
				t.Errorf("request %d: %+v %v", i, resp, err)
			}
		}()
	}
	// Every request either holds a share and waits in the compressor, or
	// has already returned: none waits for the budget.
	deadline := time.Now().Add(hangGuard)
	for entered.Load()+returned.Load() < parallel {
		if time.Now().After(deadline) {
			close(gate)
			t.Fatalf("%d compressing, %d returned: a request waited for the budget", entered.Load(), returned.Load())
		}
		time.Sleep(time.Millisecond)
	}
	if entered.Load() != int64(fit) || peak.Load() > int64(config.DefaultMaxInFlightBytes) {
		t.Errorf("%d compressed at once (%d bytes), want %d within %d", entered.Load(), peak.Load(), fit, config.DefaultMaxInFlightBytes)
	}
	close(gate)
	wg.Wait()

	over := 0
	for i := range parallel {
		rec, ok := p.pending.take("req-" + strconv.Itoa(i))
		if !ok {
			t.Fatalf("request %d left no record", i)
		}
		if rec.Report.Reason == ReasonOverBudget {
			over++
			if rec.Outcome != string(compress.OutcomeSkipped) || rec.PolicyHash == "" {
				t.Errorf("over-budget record %+v", rec)
			}
		}
	}
	if over != parallel-fit {
		t.Errorf("%d over budget, want %d", over, parallel-fit)
	}
	if p.budget.inFlight.Load() != 0 {
		t.Fatalf("%d bytes still held after every request returned", p.budget.inFlight.Load())
	}
}

// A share is held for the tool-definition parse too, and given back on
// every path: bypass, skip and panic.
func TestBudgetReleasedOnEveryPath(t *testing.T) {
	body := noisyBody(t)
	p, ctx := hookPlugin(t, `{"mode":"enforce","allow_client_bypass":true,"bindings":{"llm:other":"off"}}`)
	p.HandlePostAuth(ctx, request(body, map[string]string{"X-Tyk-Compression": "off"}))
	p.HandlePostAuth(ctx, request(body, nil))
	p.compressFn = func(compress.Input) ([]byte, compress.Report) { panic("boom") }
	p.HandlePostAuth(ctx, request(body, nil))
	if n := p.budget.inFlight.Load(); n != 0 {
		t.Fatalf("%d bytes still held", n)
	}

	// With the budget taken, a request is not parsed at all.
	p, ctx = hookPlugin(t, `{"mode":"enforce"}`)
	p.budget.acquire(config.DefaultMaxInFlightBytes, 0)
	p.compressFn = func(compress.Input) ([]byte, compress.Report) {
		t.Fatal("compressed over budget")
		return nil, compress.Report{}
	}
	resp, _ := p.HandlePostAuth(ctx, request(body, nil))
	rec, _ := p.pending.take("req-1")
	if resp.Modified || rec.Report.Reason != ReasonOverBudget {
		t.Fatalf("over budget: %+v %+v", resp, rec)
	}
}

func TestPreviewOverBudget(t *testing.T) {
	p, _ := hookPlugin(t, `{"mode":"enforce"}`)
	p.budget.acquire(config.DefaultMaxInFlightBytes, 0)
	payload, _ := json.Marshal(map[string]any{"mode": "body", "vendor": "openai", "path": "/v1/chat/completions", "body": json.RawMessage(noisyBody(t))})
	if _, err := p.rpcPreview(payload); !errors.Is(err, errPreviewBusy) {
		t.Fatalf("preview over budget: %v", err)
	}
	p.budget.release(config.DefaultMaxInFlightBytes)
	if _, err := p.rpcPreview(payload); err != nil {
		t.Fatalf("preview within budget: %v", err)
	}
}
