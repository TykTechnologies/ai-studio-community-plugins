package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// memKV is a concurrency-safe in-memory plugin_sdk.KVService.
type memKV struct {
	mu       sync.Mutex
	data     map[string][]byte
	writeErr error
	readErr  error
}

func newMemKV() *memKV { return &memKV{data: map[string][]byte{}} }

func (m *memKV) Read(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readErr != nil {
		return nil, m.readErr
	}
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("failed to read KV data: %w", status.Error(codes.NotFound, "key not found"))
	}
	return v, nil
}

func (m *memKV) Write(_ context.Context, key string, value []byte, _ *time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return false, m.writeErr
	}
	m.data[key] = value
	return true, nil
}

func (m *memKV) WriteWithTTL(ctx context.Context, key string, value []byte, _ time.Duration) (bool, error) {
	return m.Write(ctx, key, value, nil)
}

func (m *memKV) Delete(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return true, nil
}

func (m *memKV) List(context.Context, string) ([]string, error) { return nil, nil }

func TestValidatePricesURL(t *testing.T) {
	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "")
	for raw, ok := range map[string]bool{
		"https://www.llm-prices.com/current-v1.json": true,
		"http://www.llm-prices.com/current-v1.json":  false,
		"file:///etc/passwd":                         false,
		"gopher://example.com/":                      false,
		"https://":                                   false,
		"not a url\x7f":                              false,
	} {
		if err := validatePricesURL(raw); (err == nil) != ok {
			t.Errorf("validatePricesURL(%q) = %v, want ok=%v", raw, err, ok)
		}
	}

	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "true")
	if err := validatePricesURL("http://localhost:8080/prices.json"); err != nil {
		t.Errorf("http with the development switch: %v", err)
	}
}

func TestIsInternalIP(t *testing.T) {
	for addr, internal := range map[string]bool{
		"127.0.0.1":        true,
		"10.1.2.3":         true,
		"192.168.1.1":      true,
		"169.254.169.254":  true, // cloud metadata
		"100.100.100.200":  true, // CGNAT / Alibaba metadata
		"::1":              true,
		"fe80::1":          true,
		"::ffff:127.0.0.1": true,
		"0.0.0.0":          true,
		"104.21.48.1":      false,
		"2606:4700::1":     false,
	} {
		if got := isInternalIP(netip.MustParseAddr(addr)); got != internal {
			t.Errorf("isInternalIP(%s) = %v, want %v", addr, got, internal)
		}
	}
}

func priceServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"updated_at":"now","prices":[{"id":"m1","vendor":"openai","input":1,"output":2}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The connection-level check refuses internal addresses, whatever the URL
// says, unless the development switch is set.
func TestFetchRefusesInternalAddresses(t *testing.T) {
	srv := priceServer(t) // listens on 127.0.0.1

	// https to an internal address is refused at dial time
	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "")
	httpsURL := strings.Replace(srv.URL, "http://", "https://", 1)
	if _, err := fetchCurrentPrices(context.Background(), httpsURL); !errors.Is(err, errInternalAddress) {
		t.Errorf("fetch from 127.0.0.1 without the switch: %v, want errInternalAddress", err)
	}

	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "true")
	prices, err := fetchCurrentPrices(context.Background(), srv.URL)
	if err != nil || len(prices) != 1 || prices[0].ID != "m1" {
		t.Errorf("fetch with the switch: %v, %v", prices, err)
	}
}

// Through a proxy the target host is resolved and checked up front.
func TestRefuseInternalHost(t *testing.T) {
	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "")
	if err := refuseInternalHost(context.Background(), "localhost"); !errors.Is(err, errInternalAddress) {
		t.Errorf("localhost: %v, want errInternalAddress", err)
	}
	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "true")
	if err := refuseInternalHost(context.Background(), "localhost"); err != nil {
		t.Errorf("localhost with the switch: %v", err)
	}
}

// A redirect to an internal host is refused whether or not a proxy is in
// play: under a proxy, loopback and NO_PROXY targets would go direct.
func TestCheckRedirectRefusesInternalHosts(t *testing.T) {
	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "")
	orig := proxyFor
	t.Cleanup(func() { proxyFor = orig })

	proxy, _ := url.Parse("http://proxy.example.com:3128")
	for name, pick := range map[string]func(*http.Request) (*url.URL, error){
		"proxied": func(*http.Request) (*url.URL, error) { return proxy, nil },
		"direct":  func(*http.Request) (*url.URL, error) { return nil, nil },
	} {
		proxyFor = pick
		req := httptest.NewRequest(http.MethodGet, "https://localhost:8443/prices.json", nil)
		via := []*http.Request{httptest.NewRequest(http.MethodGet, "https://www.llm-prices.com/x", nil)}
		if err := checkRedirect(req, via); !errors.Is(err, errInternalAddress) {
			t.Errorf("%s: redirect to localhost: %v, want errInternalAddress", name, err)
		}
	}
}

// proxiedClient never dials a hop directly: it has no dial-time check.
func TestProxyOnlyRefusesDirectHops(t *testing.T) {
	orig := proxyFor
	t.Cleanup(func() { proxyFor = orig })
	req := httptest.NewRequest(http.MethodGet, "https://localhost:8443/", nil)

	proxyFor = func(*http.Request) (*url.URL, error) { return nil, nil }
	if _, err := proxyOnly(req); !errors.Is(err, errProxyBypass) {
		t.Errorf("direct hop: %v, want errProxyBypass", err)
	}

	proxy, _ := url.Parse("http://proxy.example.com:3128")
	proxyFor = func(*http.Request) (*url.URL, error) { return proxy, nil }
	if got, err := proxyOnly(req); err != nil || got != proxy {
		t.Errorf("proxied hop: %v, %v", got, err)
	}
}

// A cancelled scheduler context stops the sync before it touches prices,
// and the result is still recorded.
func TestDoSyncStopsOnCancelledContext(t *testing.T) {
	t.Setenv("ALLOW_INTERNAL_NETWORK_ACCESS", "true")
	srv := priceServer(t)
	kv := newMemKV()
	p := NewPriceSyncPlugin()
	p.config.PricesURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := p.doSync(ctx, kv)
	if err != nil {
		t.Fatalf("persisting the result: %v", err)
	}
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0], "context canceled") {
		t.Errorf("result errors = %v, want the cancellation", result.Errors)
	}
	if result.ModelsCreated+result.ModelsUpdated != 0 {
		t.Errorf("a cancelled sync changed prices: %+v", result)
	}
	if _, err := kv.Read(context.Background(), lastResultKey); err != nil {
		t.Error("the result of a cancelled sync was not recorded")
	}
}

func TestDoSyncReturnsWriteErrors(t *testing.T) {
	kv := newMemKV()
	kv.writeErr = errors.New("kv down")
	p := NewPriceSyncPlugin()
	p.config.PricesURL = "https://" // fails validation, no network needed

	if _, err := p.doSync(context.Background(), kv); err == nil || !strings.Contains(err.Error(), "kv down") {
		t.Errorf("doSync error = %v, want the KV write error", err)
	}
}

// Concurrent syncs (scheduled and manual) must not drop history entries.
func TestAppendToHistoryIsSerialised(t *testing.T) {
	kv := newMemKV()
	p := NewPriceSyncPlugin()

	var wg sync.WaitGroup
	for i := 0; i < historyLen; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := p.appendToHistory(context.Background(), kv, []byte(fmt.Sprintf(`{"n":%d}`, i))); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()

	data, _ := kv.Read(context.Background(), historyKey)
	var history []json.RawMessage
	if err := json.Unmarshal(data, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != historyLen {
		t.Errorf("history has %d entries after %d concurrent appends", len(history), historyLen)
	}
}

// A failed history read must not be written back as a one-entry history.
func TestAppendToHistoryKeepsHistoryWhenReadFails(t *testing.T) {
	kv := newMemKV()
	p := NewPriceSyncPlugin()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := p.appendToHistory(ctx, kv, []byte(fmt.Sprintf(`{"n":%d}`, i))); err != nil {
			t.Fatal(err)
		}
	}

	kv.readErr = status.Error(codes.Unavailable, "kv down")
	if err := p.appendToHistory(ctx, kv, []byte(`{"n":99}`)); err == nil {
		t.Error("appendToHistory hid the read error")
	}

	kv.readErr = nil
	data, _ := kv.Read(ctx, historyKey)
	var history []json.RawMessage
	if err := json.Unmarshal(data, &history); err != nil || len(history) != 3 {
		t.Errorf("history = %s, want the 3 earlier entries kept", data)
	}
}
