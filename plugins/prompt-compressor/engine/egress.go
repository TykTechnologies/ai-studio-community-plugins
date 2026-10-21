package engine

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// Egress controls (threat model T1). The host's address checks cover
// plugin command loading, not what a plugin dials, so the engine client
// checks its own connections: link-local addresses (cloud metadata among
// them) and unspecified addresses are refused at dial time, after name
// resolution, so a name that resolves to one (DNS rebinding included) is
// refused too. Loopback and private addresses stay allowed: an engine is
// often a sidecar.

// ErrBlockedAddress: the engine URL names, or resolves to, an address an
// engine may never be at.
var ErrBlockedAddress = errors.New("engine: address not allowed")

// ErrInsecureToken: the engine has a token but would get it in clear text
// (http to a host that is not loopback); the call is not made.
var ErrInsecureToken = errors.New("engine: token needs https or a loopback host")

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("169.254.0.0/16"),    // IPv4 link-local, cloud metadata
	netip.MustParsePrefix("fe80::/10"),         // IPv6 link-local
	netip.MustParsePrefix("fd00:ec2::254/128"), // AWS metadata over IPv6
}

// nat64 is the well-known NAT64 prefix, which wraps an IPv4 address.
var nat64 = netip.MustParsePrefix("64:ff9b::/96")

// blockedIP reports an address an engine may never be at. A zone is
// ignored (Prefix.Contains is false for zoned addresses), and an IPv4
// address inside NAT64 is checked as itself.
func blockedIP(ip netip.Addr) bool {
	ip = ip.Unmap().WithZone("")
	if ip.Is6() && nat64.Contains(ip) {
		b := ip.As16()
		ip = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
	}
	if !ip.IsValid() || ip.IsUnspecified() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// lookupIP resolves a host; tests replace it.
var lookupIP = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// dialer dials only allowed addresses. It resolves the host itself and
// checks every address it got (one blocked address refuses the host), and
// checks again in Control, on the address actually connected to.
func dialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{
		Timeout: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || blockedIP(ap.Addr()) {
				return ErrBlockedAddress
			}
			return nil
		},
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, ErrBlockedAddress
		}
		ips, err := lookupIP(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, ErrBlockedAddress
		}
		for _, ip := range ips {
			if blockedIP(ip) {
				return nil, ErrBlockedAddress
			}
		}
		var last error
		for _, ip := range ips {
			c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			if err == nil {
				return c, nil
			}
			last = err
		}
		return nil, last
	}
}

// transport is the engine client's HTTP transport: the default one, minus
// proxies (a proxy would dial for us, unchecked), with the checked dialer.
func transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = dialer()
	return t
}

// loopbackHost reports a host that is loopback by its name alone, so a
// token may go to it over http.
func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}
