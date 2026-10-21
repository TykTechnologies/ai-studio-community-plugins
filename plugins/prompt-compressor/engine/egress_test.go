package engine

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/TykTechnologies/midsommar/community/plugins/prompt-compressor/config"
)

func TestBlockedIP(t *testing.T) {
	for addr, want := range map[string]bool{
		"169.254.169.254":        true,
		"169.254.0.1":            true,
		"fe80::1":                true,
		"fd00:ec2::254":          true,
		"0.0.0.0":                true,
		"::":                     true,
		"::ffff:169.254.169.254": true,
		"127.0.0.1":              false,
		"::1":                    false,
		"10.0.0.7":               false,
		"192.168.1.2":            false,
		"fd00:ec2::253":          false,
		"93.184.216.34":          false,
	} {
		if got := blockedIP(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: blocked %v, want %v", addr, got, want)
		}
	}
}

// withLookup replaces name resolution for a test and counts lookups.
func withLookup(t *testing.T, fn func(host string) []netip.Addr) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	old := lookupIP
	lookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
		n.Add(1)
		if ip, err := netip.ParseAddr(host); err == nil {
			return []netip.Addr{ip}, nil
		}
		return fn(host), nil
	}
	t.Cleanup(func() { lookupIP = old })
	return &n
}

func callURL(t *testing.T, url, token string) error {
	t.Helper()
	c := New(config.EngineRef{Name: "main", Kind: config.EngineHeadroom, TimeoutMs: 500}, config.EngineCredentials{URL: url, Token: token})
	_, err := c.call(context.Background(), "m", job)
	return err
}

// Link-local, metadata and unspecified addresses are refused at dial time,
// also behind a name that resolves to one (DNS rebinding).
func TestEgressRefusesMetadataAndLinkLocal(t *testing.T) {
	withLookup(t, func(host string) []netip.Addr {
		switch host {
		case "metadata.rebind.test":
			return []netip.Addr{netip.MustParseAddr("169.254.169.254")}
		case "mixed.rebind.test":
			return []netip.Addr{netip.MustParseAddr("10.0.0.9"), netip.MustParseAddr("fe80::1")}
		}
		return nil
	})
	for _, u := range []string{
		"http://169.254.169.254/latest",
		"http://[fd00:ec2::254]/",
		"http://[fe80::1]:8080/",
		"http://0.0.0.0:9000/",
		"http://[::]:9000/",
		"http://[::ffff:169.254.169.254]/",
		"http://metadata.rebind.test/",
		"http://mixed.rebind.test/",
	} {
		if err := callURL(t, u, ""); !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("%s: %v", u, err)
		}
	}
}

// The token goes only over https or to a loopback host; otherwise no call
// is made at all.
func TestEgressTokenNeedsHTTPSOrLoopback(t *testing.T) {
	lookups := withLookup(t, func(string) []netip.Addr { return []netip.Addr{netip.MustParseAddr("10.0.0.9")} })
	if err := callURL(t, "http://engine.internal:8787", secretToken); !errors.Is(err, ErrInsecureToken) {
		t.Errorf("http with a token: %v", err)
	}
	if err := callURL(t, "http://10.0.0.9:8787", secretToken); !errors.Is(err, ErrInsecureToken) {
		t.Errorf("http to a private IP with a token: %v", err)
	}
	if lookups.Load() != 0 {
		t.Error("an insecure token call still dialled")
	}
	for _, u := range []string{"http://127.0.0.1:1", "http://localhost:1", "http://[::1]:1", "https://engine.internal:1"} {
		c := New(config.EngineRef{Name: "main", Kind: config.EngineHeadroom}, config.EngineCredentials{URL: u, Token: secretToken})
		if c.insecureToken {
			t.Errorf("%s: token refused", u)
		}
	}
	// Without a token, plain http to a private sidecar is fine.
	if c := New(config.EngineRef{Name: "main", Kind: config.EngineHeadroom}, config.EngineCredentials{URL: "http://10.0.0.9:8787"}); c.insecureToken || c.endpoint == "" {
		t.Error("a tokenless private sidecar was refused")
	}
}

// A URL with userinfo is not used (net/http would send it as basic auth),
// and errors never carry it.
func TestEgressRefusesUserinfo(t *testing.T) {
	err := callURL(t, "http://user:pw-secret@127.0.0.1:1/", "")
	if !errors.Is(err, ErrNoURL) || strings.Contains(err.Error(), "pw-secret") {
		t.Errorf("userinfo URL: %v", err)
	}
}
