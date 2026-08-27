package git

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

// ValidateRepoURL validates a user-supplied repository URL before any network
// operation is performed with it. Only https:// and ssh:// (including
// scp-like git@host:path syntax) are allowed, and hosts that are or resolve
// to internal/private addresses are rejected. This prevents the plugin,
// which runs on the AI Studio host, from being used for SSRF against
// internal services.
//
// The documented development bypass ALLOW_INTERNAL_NETWORK_ACCESS=true
// disables the internal-host check (but not scheme validation).
func ValidateRepoURL(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return fmt.Errorf("repository URL cannot be empty")
	}

	endpoint, err := transport.NewEndpoint(rawURL)
	if err != nil {
		return fmt.Errorf("invalid repository URL: %w", err)
	}

	switch endpoint.Protocol {
	case "https", "ssh":
		// allowed
	default:
		return fmt.Errorf("repository URL scheme %q is not allowed (use https:// or ssh://)", endpoint.Protocol)
	}

	if endpoint.Host == "" {
		return fmt.Errorf("repository URL has no host")
	}

	if os.Getenv("ALLOW_INTERNAL_NETWORK_ACCESS") == "true" {
		return nil
	}

	ips, err := resolveHost(endpoint.Host)
	if err != nil {
		return fmt.Errorf("cannot resolve repository host %q: %w", endpoint.Host, err)
	}
	for _, ip := range ips {
		if isInternalIP(ip) {
			return fmt.Errorf("repository URL targets an internal network address (%s); set ALLOW_INTERNAL_NETWORK_ACCESS=true to allow this in development", endpoint.Host)
		}
	}

	return nil
}

// resolveHost returns the IPs for a host, which may be an IP literal or a
// hostname requiring DNS resolution.
func resolveHost(host string) ([]net.IP, error) {
	// IPv6 literals may be bracketed ([::1])
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if strings.Contains(strings.ToLower(host), "localhost") {
		return []net.IP{net.IPv4(127, 0, 0, 1)}, nil
	}
	return net.LookupIP(host)
}

// isInternalIP reports whether an IP is in a private or special-use range.
func isInternalIP(ip net.IP) bool {
	privateCIDRs := []string{
		"10.0.0.0/8",     // Private Class A
		"172.16.0.0/12",  // Private Class B
		"192.168.0.0/16", // Private Class C
		"127.0.0.0/8",    // IPv4 loopback
		"169.254.0.0/16", // IPv4 link-local (incl. cloud metadata endpoints)
		"0.0.0.0/8",      // "This network"
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local addresses
		"fe80::/10",      // IPv6 link-local
	}

	for _, cidrStr := range privateCIDRs {
		_, cidr, err := net.ParseCIDR(cidrStr)
		if err != nil {
			continue
		}
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
