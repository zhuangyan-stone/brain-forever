package httpx

import (
	"net"
	"net/http"
	"time"
)

// NewHTTPClient creates an HTTP client with a fallback DNS resolver
// timeout: request timeout duration
func NewHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  NewResolverWithFallback(),
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

// NewStreamHTTPClient creates an HTTP client suitable for long-lived streaming (SSE) connections.
// It uses a larger timeout than NewHTTPClient to accommodate long pauses between chunks,
// such as when debugging (stepping through code in a debugger) or when the LLM API
// takes a long time between response chunks.
//
// timeout: request timeout duration (e.g., 15 * time.Minute for debugging sessions)
//
// The dial timeout (30s) and TLS handshake timeout (10s) are still applied to prevent
// hanging during connection establishment.
func NewStreamHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  NewResolverWithFallback(),
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}
