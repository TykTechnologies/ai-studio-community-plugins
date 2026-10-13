package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"syscall"
	"time"
)

// externalResponse is the top-level wrapper from llm-prices.com
type externalResponse struct {
	UpdatedAt string          `json:"updated_at"`
	Prices    []ExternalPrice `json:"prices"`
}

// ExternalPrice represents a single model pricing entry from llm-prices.com
// Prices are in $/MTok (per million tokens).
type ExternalPrice struct {
	ID          string   `json:"id"`
	Vendor      string   `json:"vendor"`
	Name        string   `json:"name"`
	Input       *float64 `json:"input"`
	Output      *float64 `json:"output"`
	InputCached *float64 `json:"input_cached"`
}

// allowInternalNetwork is the platform's development switch for reaching
// internal addresses. Plugins on AI Studio 2.3+ only see it when the operator
// passes it through (PLUGIN_ENV_PASSTHROUGH=ALLOW_INTERNAL_NETWORK_ACCESS).
func allowInternalNetwork() bool {
	return os.Getenv("ALLOW_INTERNAL_NETWORK_ACCESS") == "true"
}

// validatePricesURL accepts an absolute https URL with a host. Plain http is
// accepted only with the internal-network development switch.
func validatePricesURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("prices_url is not a valid URL: %w", err)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && allowInternalNetwork():
	case u.Scheme == "http":
		return fmt.Errorf("prices_url must use https (http is allowed only with ALLOW_INTERNAL_NETWORK_ACCESS=true, for development)")
	default:
		return fmt.Errorf("prices_url scheme %q is not allowed; use https", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("prices_url has no host")
	}
	return nil
}

// cgnat is 100.64.0.0/10 (carrier-grade NAT; includes cloud metadata such
// as Alibaba's 100.100.100.200), which netip does not count as private.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isInternalIP reports whether ip is loopback, private, link-local (cloud
// metadata at 169.254.169.254), CGNAT, multicast or unspecified.
func isInternalIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || cgnat.Contains(ip)
}

var errInternalAddress = errors.New("prices_url resolves to an internal network address")

// refuseInternal runs on every connection, after DNS resolution and for each
// redirect, so a hostname that resolves (or later re-resolves) to an
// internal address is refused too.
func refuseInternal(_, address string, _ syscall.RawConn) error {
	if allowInternalNetwork() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if isInternalIP(ip) {
		return fmt.Errorf("%w (%s); set ALLOW_INTERNAL_NETWORK_ACCESS=true to allow this in development", errInternalAddress, ip)
	}
	return nil
}

// checkRedirect applies the configured-URL rules to every redirect target.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	if err := validatePricesURL(req.URL.String()); err != nil {
		return err
	}
	// Checked for every hop, whichever client follows it (defence in depth;
	// directClient's dial hook checks the connected address as well).
	return refuseInternalHost(req.Context(), req.URL.Hostname())
}

// proxyFor picks the proxy for a request; a variable so tests can set one.
var proxyFor = http.ProxyFromEnvironment

var errProxyBypass = errors.New("request would bypass the configured proxy (NO_PROXY or loopback target); refused")

// proxyOnly is proxiedClient's proxy function. proxiedClient has no dial
// hook (it dials the proxy), so a hop the proxy settings send direct, such
// as a redirect to localhost or a NO_PROXY host, would go unchecked: it is
// refused instead.
func proxyOnly(req *http.Request) (*url.URL, error) {
	proxyURL, err := proxyFor(req)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		return nil, errProxyBypass
	}
	return proxyURL, nil
}

// directClient checks each connection's resolved address. Through an HTTP
// proxy the connection goes to the proxy (often an internal address), so
// proxiedClient leaves that alone; the target host is resolved and checked
// before the request and on every redirect instead, and hops that would
// skip the proxy are refused.
var (
	directClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second, Control: refuseInternal}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: checkRedirect,
	}
	proxiedClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:               proxyOnly,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: checkRedirect,
	}
)

func usesProxy(req *http.Request) bool {
	proxyURL, err := proxyFor(req)
	return err == nil && proxyURL != nil
}

// refuseInternalHost resolves host and refuses it if any address is internal.
func refuseInternalHost(ctx context.Context, host string) error {
	if allowInternalNetwork() {
		return nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", host, err)
	}
	for _, ip := range addrs {
		if isInternalIP(ip) {
			return fmt.Errorf("%w (%s); set ALLOW_INTERNAL_NETWORK_ACCESS=true to allow this in development", errInternalAddress, ip)
		}
	}
	return nil
}

func fetchCurrentPrices(ctx context.Context, rawURL string) ([]ExternalPrice, error) {
	if err := validatePricesURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "TykAIStudio-PriceSyncPlugin/1.0")
	req.Header.Set("Accept", "application/json")

	client := directClient
	if usesProxy(req) {
		if err := refuseInternalHost(ctx, req.URL.Hostname()); err != nil {
			return nil, err
		}
		client = proxiedClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching prices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var parsed externalResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing prices JSON: %w", err)
	}

	return parsed.Prices, nil
}
