package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	pb "github.com/TykTechnologies/midsommar/v2/proto"
)

func requestsRule(limit int) Rule {
	return Rule{
		ID: "r1", Name: "per-app", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "requests", Value: limit}, Action: "enforce", Enabled: true,
	}
}

func pendingCount(p *RateLimiterPlugin) int {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	return len(p.pending)
}

// An allowed request's X-RateLimit-* headers go to the client, on the
// response, and not to the vendor: post_auth returned them as request
// headers, so the upstream received the customer's limits and the client saw
// them only on a 429 (2.3.0-rc4 finding M9).
func TestAllowedRequest_HeadersGoToTheClientNotTheVendor(t *testing.T) {
	p := newTestPlugin([]Rule{requestsRule(50)})

	resp, err := p.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 15, "mock-hdr", "req-1"))
	if err != nil {
		t.Fatal(err)
	}
	// What post_auth returns in Headers is set on the request to the vendor.
	if resp.Block || resp.Modified || len(resp.Headers) != 0 {
		t.Fatalf("post_auth must leave the upstream request alone, got block=%v modified=%v headers=%v", resp.Block, resp.Modified, resp.Headers)
	}

	// The response's header hook adds them, next to the vendor's own.
	vendor := map[string]string{
		"Content-Type":                   "application/json",
		"X-Ratelimit-Limit-Requests":     "10000",
		"X-Ratelimit-Remaining-Requests": "9999",
		"X-Ratelimit-Limit-Tokens":       "2000000",
	}
	hdrResp, err := p.OnBeforeWriteHeaders(testCtx(), &pb.HeadersRequest{
		Headers: vendor,
		Context: &pb.PluginContext{RequestId: "req-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hdrResp.Modified {
		t.Fatal("the header hook must mark the response headers as modified")
	}
	if got := hdrResp.Headers["X-RateLimit-Limit"]; got != "50" {
		t.Errorf("X-RateLimit-Limit = %q, want 50", got)
	}
	if got := hdrResp.Headers["X-RateLimit-Remaining"]; got != "49" {
		t.Errorf("X-RateLimit-Remaining = %q, want 49", got)
	}
	reset, err := strconv.ParseInt(hdrResp.Headers["X-RateLimit-Reset"], 10, 64)
	if err != nil || reset <= time.Now().Unix() {
		t.Errorf("X-RateLimit-Reset = %q, want a time in the future", hdrResp.Headers["X-RateLimit-Reset"])
	}
	// The vendor's own rate limit headers are untouched.
	for name, want := range vendor {
		if got := hdrResp.Headers[name]; got != want {
			t.Errorf("vendor header %s = %q, want %q", name, got, want)
		}
	}
	if len(vendor) != 4 {
		t.Error("the hook must not change the map it was given")
	}
}

// With a limit of N the first allowed response says N-1 remaining and the
// last says 0; the 429 that follows agrees. Remaining used to be read before
// the request was counted, so it was one too high.
func TestRemaining_CountsTheRequestItself(t *testing.T) {
	const limit = 5
	p := newTestPlugin([]Rule{requestsRule(limit)})

	for i := 0; i < limit; i++ {
		id := fmt.Sprintf("req-%d", i)
		resp, err := p.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", id))
		if err != nil {
			t.Fatal(err)
		}
		if resp.Block {
			t.Fatalf("request %d of %d must be allowed", i+1, limit)
		}
		headers := clientHeaders(t, p, id)
		if got, want := headers["X-RateLimit-Remaining"], strconv.Itoa(limit-1-i); got != want {
			t.Errorf("request %d: X-RateLimit-Remaining = %q, want %s", i+1, got, want)
		}
		if got := headers["X-RateLimit-Limit"]; got != strconv.Itoa(limit) {
			t.Errorf("request %d: X-RateLimit-Limit = %q, want %d", i+1, got, limit)
		}
	}

	resp, err := p.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", "req-over"))
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Block || resp.StatusCode != 429 {
		t.Fatalf("request %d must get 429, got block=%v status=%d", limit+1, resp.Block, resp.StatusCode)
	}
	// A block's headers are written to the client by the gateway.
	if resp.Headers["X-RateLimit-Limit"] != strconv.Itoa(limit) || resp.Headers["X-RateLimit-Remaining"] != "0" {
		t.Errorf("429 headers = %v, want limit %d and remaining 0", resp.Headers, limit)
	}
	if got := clientHeaders(t, p, "req-over"); len(got) != 0 {
		t.Errorf("nothing is held for a blocked request, got %v", got)
	}
}

// A concurrent rule counts the slot the request took; a token rule cannot
// count tokens it has not seen yet.
func TestRemaining_ConcurrentAndTokenRules(t *testing.T) {
	conc := newTestPlugin([]Rule{{
		ID: "c1", Name: "conc", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "concurrent", Value: 3}, Action: "enforce", Enabled: true,
	}})
	if resp, _ := conc.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", "req-c")); resp.Block {
		t.Fatal("must be allowed")
	}
	if got := clientHeaders(t, conc, "req-c")["X-RateLimit-Remaining"]; got != "2" {
		t.Errorf("concurrent: X-RateLimit-Remaining = %q, want 2", got)
	}

	tokens := newTestPlugin([]Rule{{
		ID: "t1", Name: "tokens", Dimensions: []string{"app_id"},
		Limit: Limit{Type: "tokens", Value: 1000}, Action: "enforce", Enabled: true,
	}})
	if resp, _ := tokens.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", "req-t")); resp.Block {
		t.Fatal("must be allowed")
	}
	if got := clientHeaders(t, tokens, "req-t")["X-RateLimit-Remaining"]; got != "1000" {
		t.Errorf("tokens: X-RateLimit-Remaining = %q, want 1000", got)
	}
}

// A buffered response: the headers are on it after the header hook and after
// the body hook, and nothing is held once it is written.
func TestBufferedResponse_HeadersSetAndReleased(t *testing.T) {
	p := newTestPlugin([]Rule{requestsRule(10)})
	if resp, _ := p.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", "req-b")); resp.Block {
		t.Fatal("must be allowed")
	}
	if pendingCount(p) != 1 {
		t.Fatalf("held entries = %d, want 1", pendingCount(p))
	}

	headers := clientHeaders(t, p, "req-b")
	if headers["X-RateLimit-Remaining"] != "9" {
		t.Fatalf("header hook: %v", headers)
	}

	// The body hook gets the headers as a hook in between left them: here,
	// without ours.
	write := testResponseWriteRequest("req-b", openAIResponse(10, 5), false)
	write.Headers = map[string]string{"Content-Type": "application/json"}
	writeResp, err := p.OnBeforeWrite(testCtx(), write)
	if err != nil {
		t.Fatal(err)
	}
	if !writeResp.Modified || writeResp.Headers["X-RateLimit-Limit"] != "10" || writeResp.Headers["X-RateLimit-Remaining"] != "9" {
		t.Errorf("body hook: modified=%v headers=%v, want the rate limit headers", writeResp.Modified, writeResp.Headers)
	}
	if writeResp.Headers["Content-Type"] != "application/json" {
		t.Error("the response's own headers must be kept")
	}
	if pendingCount(p) != 0 {
		t.Errorf("held entries after the response = %d, want 0", pendingCount(p))
	}
}

// A streamed response: the header hook runs before the first chunk, chunks
// leave the entry alone, and the end of the stream releases it.
func TestStreamedResponse_HeadersSetAndReleased(t *testing.T) {
	p := newTestPlugin([]Rule{requestsRule(10)})
	if resp, _ := p.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", "req-s")); resp.Block {
		t.Fatal("must be allowed")
	}

	headers := clientHeaders(t, p, "req-s")
	if headers["X-RateLimit-Limit"] != "10" || headers["X-RateLimit-Remaining"] != "9" || headers["X-RateLimit-Reset"] == "" {
		t.Fatalf("stream header hook: %v", headers)
	}

	if _, err := p.OnBeforeWrite(testCtx(), testResponseWriteRequest("req-s", []byte("data: {}\n\n"), true)); err != nil {
		t.Fatal(err)
	}
	if pendingCount(p) != 1 {
		t.Errorf("a chunk must not release the headers, held = %d", pendingCount(p))
	}

	if _, err := p.OnStreamComplete(testCtx(), &pb.StreamCompleteRequest{
		Context: &pb.PluginContext{RequestId: "req-s"},
	}); err != nil {
		t.Fatal(err)
	}
	if pendingCount(p) != 0 {
		t.Errorf("held entries after the stream = %d, want 0", pendingCount(p))
	}
}

// A vendor header of the same name, in whatever case it arrived, is replaced
// rather than joined by a second header that differs only in case.
func TestWithHeldHeaders_ReplacesSameNameInAnyCase(t *testing.T) {
	response := map[string]string{
		"X-Ratelimit-Limit":          "1",
		"x-ratelimit-remaining":      "1",
		"X-Ratelimit-Limit-Requests": "10000",
	}
	held := map[string]string{"X-RateLimit-Limit": "50", "X-RateLimit-Remaining": "49"}

	out, ok := withHeldHeaders(response, held)
	if !ok {
		t.Fatal("want modified")
	}
	seen := map[string]int{}
	for name := range out {
		seen[strings.ToLower(name)]++
	}
	for name, n := range seen {
		if n != 1 {
			t.Errorf("header %s appears %d times", name, n)
		}
	}
	if out["X-RateLimit-Limit"] != "50" || out["X-RateLimit-Remaining"] != "49" || out["X-Ratelimit-Limit-Requests"] != "10000" {
		t.Errorf("got %v", out)
	}
	if len(response) != 3 || response["X-Ratelimit-Limit"] != "1" {
		t.Error("the input map must not be changed")
	}

	if out, ok := withHeldHeaders(response, nil); ok || len(out) != 3 {
		t.Error("nothing held: unchanged and not modified")
	}
}

// Requests that end without a response phase (the client went away, a later
// plugin answered) do not grow the held state for ever: the sweep that runs
// from post_auth forgets entries older than pendingHeadersMaxAge.
func TestHeldHeaders_SweptWhenNoResponseComes(t *testing.T) {
	p := newTestPlugin([]Rule{requestsRule(1000)})
	for i := 0; i < 200; i++ {
		if resp, _ := p.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", fmt.Sprintf("gone-%d", i))); resp.Block {
			t.Fatal("must be allowed")
		}
	}
	if pendingCount(p) != 200 {
		t.Fatalf("held = %d, want 200", pendingCount(p))
	}

	// Not yet old enough: kept.
	p.sweepHeaders(time.Now().Add(pendingHeadersMaxAge - time.Minute))
	if pendingCount(p) != 200 {
		t.Errorf("held after an early sweep = %d, want 200", pendingCount(p))
	}

	// Age them, and let the next request's sweep run.
	p.pendingMu.Lock()
	for id, held := range p.pending {
		held.stored = time.Now().Add(-pendingHeadersMaxAge - time.Minute)
		p.pending[id] = held
	}
	p.pendingMu.Unlock()
	p.inflightMu.Lock()
	p.lastSweep = time.Time{}
	p.inflightMu.Unlock()

	if resp, _ := p.HandlePostAuth(testCtx(), testEnrichedRequest(1, 0, 0, "", "next")); resp.Block {
		t.Fatal("must be allowed")
	}
	if pendingCount(p) != 1 {
		t.Errorf("held after the sweep = %d, want 1 (the request that ran it)", pendingCount(p))
	}
}

// The held state has a hard bound: over it, a request is still counted and
// allowed, it only gets no rate limit headers.
func TestHeldHeaders_Bounded(t *testing.T) {
	p := newTestPlugin(nil)
	now := time.Now()
	p.pending = make(map[string]pendingHeaders, pendingHeadersMax)
	for i := 0; i < pendingHeadersMax; i++ {
		p.pending[strconv.Itoa(i)] = pendingHeaders{stored: now}
	}

	p.holdHeaders("one-more", map[string]string{"X-RateLimit-Limit": "1"}, now)
	if pendingCount(p) != pendingHeadersMax {
		t.Errorf("held = %d, want the bound %d", pendingCount(p), pendingHeadersMax)
	}
	if p.heldHeaders("one-more") != nil {
		t.Error("over the bound nothing more is held")
	}

	// While it stays full, requests do not each pay for a walk of the whole
	// table: the sweep ran just now, so the next ones only skip holding.
	if !p.pendingFullSweep.Equal(now) {
		t.Fatalf("the full table was swept at %v, want %v", p.pendingFullSweep, now)
	}
	p.pendingMu.Lock()
	for id, held := range p.pending {
		held.stored = now.Add(-pendingHeadersMaxAge - time.Hour) // a sweep would now empty the table
		p.pending[id] = held
	}
	p.pendingMu.Unlock()
	p.holdHeaders("within-the-interval", map[string]string{"X-RateLimit-Limit": "1"}, now.Add(sweepInterval-time.Second))
	if pendingCount(p) != pendingHeadersMax {
		t.Errorf("held = %d: a full table must be swept at most once per sweepInterval", pendingCount(p))
	}
	p.pendingMu.Lock()
	for id, held := range p.pending {
		held.stored = now
		p.pending[id] = held
	}
	p.pendingMu.Unlock()

	// Once the old entries have aged out there is room again.
	later := now.Add(pendingHeadersMaxAge + time.Second)
	p.holdHeaders("after", map[string]string{"X-RateLimit-Limit": "1"}, later)
	if pendingCount(p) != 1 || p.heldHeaders("after") == nil {
		t.Errorf("held = %d, want only the new entry", pendingCount(p))
	}
}
