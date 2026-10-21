// Package engine calls an external compression engine (a Headroom-compatible
// /v1/compress or an LLMLingua-2-compatible compressPrompt service) for the
// units the compressor selected, after levels 1–3.
//
// Every result is memoised by the text sent, the policy hash and the
// engine, first in process and then in the edge's KV, so a unit already
// sent compresses to the same bytes on every later turn even if the engine
// is down or not deterministic. A failure in failure_mode open memoises a
// passthrough for the same reason: a later success must not rewrite history
// the vendor has already cached. The edge KV is per edge, so two edges may
// disagree on a unit unless the engine is deterministic.
//
// The engine URL and token never appear in errors or logs. Only the host is
// logged. Egress is checked at dial time (egress.go).
package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/compress"
	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
	"github.com/TykTechnologies/midsommar/v2/pkg/lrucache"
)

// RefusedCredential is what the edge puts in a config value whose $SECRET/
// reference it would not resolve (eac.37).
const RefusedCredential = "refused-credential-reference"

const (
	// maxConcurrent bounds one request's engine calls in flight.
	maxConcurrent = 8
	// maxResponseBytes bounds an engine's answer.
	maxResponseBytes = 16 << 20
	// memoEntries and memoBytes bound the in-process memo.
	memoEntries = 20000
	memoBytes   = 64 << 20
	// kvTimeout bounds a memo read from KV on the request path.
	kvTimeout = 100 * time.Millisecond
	// kvTTL is how long a memoised result is kept in KV.
	kvTTL = 7 * 24 * time.Hour
	// llmlinguaRate is the share of tokens LLMLingua-2 keeps.
	llmlinguaRate = 0.5
)

// Errors name what failed, never the URL.
var (
	ErrNoURL       = errors.New("engine: no usable URL")
	ErrTimeout     = errors.New("engine: timed out")
	ErrStatus      = errors.New("engine: error status")
	ErrMalformed   = errors.New("engine: malformed response")
	ErrUnreachable = errors.New("engine: unreachable")
)

// KV is the edge's key-value store (plugin_sdk.KVService).
type KV interface {
	Read(ctx context.Context, key string) ([]byte, error)
	Write(ctx context.Context, key string, value []byte, expireAt *time.Time) (bool, error)
}

// Logger logs an engine failure; fields never include the URL or token.
type Logger func(msg string, fields ...interface{})

// Stats count an engine's work.
type Stats struct {
	Calls, MemoHits, KVHits, Failures, Rejected atomic.Uint64
}

// Client is one engine: its kind, where it is and its memo. It is safe for
// concurrent use and lives across requests.
type Client struct {
	name, kind string
	endpoint   string // the full URL called
	host       string // for logs
	token      string
	// insecureToken: the token would go in clear text, so no call is made.
	insecureToken bool
	timeout       time.Duration
	id            string // folds kind, name and URL into memo keys
	http          *http.Client
	memo          *lrucache.Cache[result]
	Stats         Stats
}

// result is a memoised outcome: Text, or Passthrough when the unit is to be
// sent at levels 1–3 for good.
type result struct {
	Text        string `json:"t,omitempty"`
	Passthrough bool   `json:"p,omitempty"`
}

// New returns a client for ref with the credentials creds. An unusable URL
// is not an error here: every call then fails (ErrNoURL), and failure_mode
// decides what that means.
func New(ref config.EngineRef, creds config.EngineCredentials) *Client {
	c := &Client{name: ref.Name, kind: ref.Kind, timeout: time.Duration(ref.TimeoutMs) * time.Millisecond}
	if c.timeout <= 0 {
		c.timeout = time.Duration(config.DefaultEngineTimeout) * time.Millisecond
	}
	if u, ok := usableURL(creds.URL); ok {
		path := "/v1/compress"
		if ref.Kind == config.EngineLLMLingua2 {
			path = "/compressPrompt"
		}
		u.Path = strings.TrimSuffix(u.Path, "/") + path
		c.endpoint, c.host = u.String(), u.Hostname()
	}
	if t := strings.TrimSpace(creds.Token); t != "" && t != RefusedCredential && !strings.HasPrefix(t, "$SECRET/") {
		c.token = t
		c.insecureToken = c.endpoint != "" && !strings.HasPrefix(c.endpoint, "https://") && !loopbackHost(c.host)
	}
	sum := sha256.Sum256([]byte(ref.Kind + "\x00" + ref.Name + "\x00" + c.endpoint))
	c.id = hex.EncodeToString(sum[:8])
	c.http = &http.Client{
		// An engine never redirects: following one could send unit text, and
		// the token, somewhere the admin did not configure.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     transport(),
	}
	c.memo = lrucache.NewWeighted(memoEntries, memoBytes, func(k string, r result) int64 { return int64(len(k) + len(r.Text) + 16) })
	return c
}

// Key identifies the client for reuse: a change of kind, URL, token or
// timeout makes a new client (and memo).
func Key(ref config.EngineRef, creds config.EngineCredentials) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s", ref.Kind, ref.Name, ref.TimeoutMs, creds.URL, creds.Token)))
	return hex.EncodeToString(sum[:])
}

// usableURL accepts an absolute http or https URL with a host and no
// userinfo (net/http would send it as basic auth: credentials belong in the
// token). A $SECRET/ reference the edge did not resolve is not one.
func usableURL(raw string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == RefusedCredential || strings.HasPrefix(raw, "$SECRET/") {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, false
	}
	return u, true
}

// Run is the compressor's engine hook (compress.EngineFunc) for one
// request: it compresses jobs concurrently, through the memo. With
// failureMode open a failed job is memoised as a passthrough and the call
// never fails; with closed the first failure fails the call (nothing is
// memoised, since nothing is sent).
func (c *Client) Run(ctx context.Context, kv KV, log Logger, policyHash string, failureMode config.FailureMode) compress.EngineFunc {
	return func(model string, jobs []compress.EngineJob) ([]compress.EngineResult, error) {
		out := make([]compress.EngineResult, len(jobs))
		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			firstErr error
		)
		sem := make(chan struct{}, maxConcurrent)
		for i, j := range jobs {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int, j compress.EngineJob) {
				defer func() { <-sem; wg.Done() }()
				r, err := c.one(ctx, kv, log, policyHash, failureMode, model, j)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
				out[i] = r
			}(i, j)
		}
		wg.Wait()
		if firstErr != nil {
			return nil, firstErr
		}
		return out, nil
	}
}

// one compresses one job through the memo.
func (c *Client) one(ctx context.Context, kv KV, log Logger, policyHash string, failureMode config.FailureMode, model string, j compress.EngineJob) (compress.EngineResult, error) {
	key := c.memoKey(policyHash, j.Text)
	hit, failed := true, false
	r, err := c.memo.Do(key, func() (result, error) {
		hit = false
		if r, ok := c.readKV(ctx, kv, key); ok {
			c.Stats.KVHits.Add(1)
			return r, nil
		}
		c.Stats.Calls.Add(1)
		text, err := c.call(ctx, model, j)
		var r result
		switch {
		case err != nil:
			c.Stats.Failures.Add(1)
			failed = true
			if log != nil {
				log("Compression engine call failed", "engine", c.name, "host", c.host, "error", err.Error())
			}
			if failureMode == config.FailClosed {
				return result{}, err
			}
			r = result{Passthrough: true}
		case len(text) >= len(j.Text) || !keepsAll(text, j.Protect):
			c.Stats.Rejected.Add(1)
			r = result{Passthrough: true}
		default:
			r = result{Text: text}
		}
		c.writeKV(kv, key, r)
		return r, nil
	})
	if err != nil {
		return compress.EngineResult{}, err
	}
	if hit {
		c.Stats.MemoHits.Add(1)
	}
	if r.Passthrough {
		return compress.EngineResult{Text: j.Text, Failed: failed}, nil
	}
	return compress.EngineResult{Text: r.Text, Changed: true}, nil
}

// memoKey is sha256 over the engine, the policy hash and the text sent,
// each length-prefixed.
func (c *Client) memoKey(policyHash, text string) string {
	h := sha256.New()
	for _, s := range []string{c.id, policyHash, text} {
		fmt.Fprintf(h, "%d:", len(s))
		h.Write([]byte(s))
	}
	return "pc/engine/" + hex.EncodeToString(h.Sum(nil))
}

func (c *Client) readKV(ctx context.Context, kv KV, key string) (result, bool) {
	if kv == nil {
		return result{}, false
	}
	rctx, cancel := context.WithTimeout(ctx, kvTimeout)
	defer cancel()
	raw, err := kv.Read(rctx, key)
	if err != nil || len(raw) == 0 {
		return result{}, false
	}
	var r result
	if json.Unmarshal(raw, &r) != nil || (r.Text == "" && !r.Passthrough) {
		return result{}, false
	}
	return r, true
}

// writeKV stores r in the background; the in-process memo already holds it.
func (c *Client) writeKV(kv KV, key string, r result) {
	if kv == nil {
		return
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		exp := time.Now().Add(kvTTL)
		_, _ = kv.Write(ctx, key, raw, &exp)
	}()
}

// keepsAll reports whether out still holds every protected string.
func keepsAll(out string, protect []string) bool {
	for _, p := range protect {
		if !strings.Contains(out, p) {
			return false
		}
	}
	return true
}

// call sends one job to the engine.
func (c *Client) call(ctx context.Context, model string, j compress.EngineJob) (string, error) {
	if c.endpoint == "" {
		return "", ErrNoURL
	}
	if c.insecureToken {
		return "", ErrInsecureToken
	}
	var req any
	if c.kind == config.EngineLLMLingua2 {
		req = map[string]any{"context": []string{j.Text}, "rate": llmlinguaRate, "force_tokens": nonNil(j.Protect), "use_llmlingua2": true}
	} else {
		req = map[string]any{"messages": []any{map[string]string{"role": j.Role, "content": j.Text}}, "model": model}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", ErrMalformed
	}
	cctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	hreq, err := http.NewRequestWithContext(cctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", ErrNoURL
	}
	hreq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		hreq.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(hreq)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			return "", ErrBlockedAddress
		}
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return "", ErrTimeout
		}
		return "", ErrUnreachable
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", ErrTimeout
		}
		return "", ErrUnreachable
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("%w %d", ErrStatus, resp.StatusCode)
	}
	if len(raw) > maxResponseBytes {
		return "", ErrMalformed
	}
	return c.parse(raw)
}

func (c *Client) parse(raw []byte) (string, error) {
	if c.kind == config.EngineLLMLingua2 {
		var r struct {
			CompressedPrompt *string `json:"compressed_prompt"`
		}
		if json.Unmarshal(raw, &r) != nil || r.CompressedPrompt == nil {
			return "", ErrMalformed
		}
		return *r.CompressedPrompt, nil
	}
	var r struct {
		Messages []struct {
			Content *string `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &r) != nil || len(r.Messages) != 1 || r.Messages[0].Content == nil {
		return "", ErrMalformed
	}
	return *r.Messages[0].Content, nil
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
