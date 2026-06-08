package httpx

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Fallback DNS server addresses (used when system DNS resolution fails)
// Prefer domestic public DNS to ensure resolution speed and stability for domestic API domains (e.g., open.bigmodel.cn)
var defaultFallbackDNS = []string{"114.114.114.114:53", "223.5.5.5:53", "180.76.76.76:53"}

// mergeFallbackDNS merges custom DNS servers with default fallback DNS servers.
// Custom servers come first, duplicates are removed.
func mergeFallbackDNS(custom []string) []string {
	seen := make(map[string]struct{}, len(custom)+len(defaultFallbackDNS))
	merged := make([]string, 0, len(custom)+len(defaultFallbackDNS))

	// Add custom DNS first
	for _, dns := range custom {
		if _, ok := seen[dns]; !ok {
			seen[dns] = struct{}{}
			merged = append(merged, dns)
		}
	}

	// Add default DNS, skipping duplicates
	for _, dns := range defaultFallbackDNS {
		if _, ok := seen[dns]; !ok {
			seen[dns] = struct{}{}
			merged = append(merged, dns)
		}
	}

	return merged
}

// NewResolverWithFallback creates a Resolver with fallback DNS servers
// Prefers system DNS, automatically falls back to backup DNS on failure
// customFallback: optional custom fallback DNS servers (appended before defaults, duplicates removed)
//
// NOTE: This resolver uses a custom Dial function which only provides
// connection-level fallback. If the system DNS server is reachable but returns
// NXDOMAIN (e.g., "no such host") for a domain, the fallback DNS servers will NOT
// be consulted. For resolution-level fallback, use NewDNSFallbackDialContext instead.
func NewResolverWithFallback(customFallback ...[]string) *net.Resolver {
	fallbackServers := defaultFallbackDNS
	if len(customFallback) > 0 && len(customFallback[0]) > 0 {
		fallbackServers = mergeFallbackDNS(customFallback[0])
	}

	return &net.Resolver{
		// Do not set PreferGo, let Go use system DNS on Windows (cgo mode),
		// to avoid timeout issues with Go's built-in DNS resolver in certain network environments (VPN, multi-NIC).
		// PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			// Try system DNS first
			d := net.Dialer{Timeout: 5 * time.Second}
			conn, err := d.DialContext(ctx, network, address)
			if err == nil {
				return conn, nil
			}

			// System DNS failed, try fallback DNS servers
			for _, dnsAddr := range fallbackServers {
				conn, err := d.DialContext(ctx, "udp", dnsAddr)
				if err == nil {
					return conn, nil
				}
			}
			return nil, fmt.Errorf("all DNS servers unavailable. %w", err)
		},
	}
}

// NewDNSFallbackDialContext creates a DialContext function that resolves hostnames
// with intelligent DNS fallback at the resolution level.
//
// It first resolves the hostname using the system-configured DNS (via cgo on Windows).
// If system DNS resolution fails (NXDOMAIN, SERVFAIL, or connection failure),
// it falls back to the specified alternative DNS servers.
//
// This is especially useful in network environments where the ISP's DNS server
// may not properly resolve certain domains (e.g., open.bigmodel.cn), while
// public domestic DNS servers (like 114.114.114.114) can resolve them correctly.
//
// fallbackServers: list of DNS server addresses in "host:port" format.
// If nil or empty, defaultFallbackDNS is used.
func NewDNSFallbackDialContext(dialer *net.Dialer, fallbackServers []string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if len(fallbackServers) == 0 {
		fallbackServers = defaultFallbackDNS
	}

	// Create a pure-Go resolver that connects only to the fallback DNS servers
	fallbackResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			var firstErr error
			for _, dnsAddr := range fallbackServers {
				conn, err := d.DialContext(ctx, "udp", dnsAddr)
				if err == nil {
					return conn, nil
				}
				if firstErr == nil {
					firstErr = err
				}
			}
			return nil, fmt.Errorf("all fallback DNS servers unavailable: %w", firstErr)
		},
	}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		// Step 1: Try system DNS first (uses cgo/getaddrinfo on Windows)
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err == nil && len(ips) > 0 {
			return dialResolvedIPs(ctx, dialer, network, port, ips)
		}

		// Step 2: System DNS failed, try fallback DNS resolver (pure Go with domestic DNS)
		ips, fallbackErr := fallbackResolver.LookupIPAddr(ctx, host)
		if fallbackErr == nil && len(ips) > 0 {
			return dialResolvedIPs(ctx, dialer, network, port, ips)
		}

		// Both failed — return the original system DNS error for clarity
		if err != nil {
			return nil, fmt.Errorf("DNS lookup failed for %s (tried system and fallback DNS): %w", host, err)
		}
		return nil, fmt.Errorf("DNS lookup returned no addresses for %s", host)
	}
}

// dialResolvedIPs attempts to establish a connection to each resolved IP address.
func dialResolvedIPs(ctx context.Context, dialer *net.Dialer, network, port string, ips []net.IPAddr) (net.Conn, error) {
	var firstErr error
	for _, ip := range ips {
		targetAddr := net.JoinHostPort(ip.IP.String(), port)
		conn, err := dialer.DialContext(ctx, network, targetAddr)
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}
