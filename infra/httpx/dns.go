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
