//go:build !wasip1

package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// VirtualHTTPClient is a capability-scoped outbound HTTP client in Go.
type VirtualHTTPClient struct {
	client *http.Client
}

func NewVirtualHTTPClient() *VirtualHTTPClient {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 15 * time.Second,
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}

			var safeConn net.Conn
			var lastErr error
			for _, ip := range ips {
				if isInternalIP(ip) {
					gate := &SecurityGate{}
					if err := gate.CheckNetworkAccess(ctx, net.JoinHostPort(ip.String(), port)); err != nil {
						lastErr = fmt.Errorf("DNS rebinding blocked: resolved IP %s is private/internal and not explicitly whitelisted", ip.String())
						continue
					}
				}

				safeAddr := net.JoinHostPort(ip.String(), port)
				conn, dialErr := dialer.DialContext(ctx, network, safeAddr)
				if dialErr == nil {
					safeConn = conn
					lastErr = nil
					break
				}
				lastErr = dialErr
			}

			if safeConn == nil {
				if lastErr == nil {
					return nil, fmt.Errorf("no IP addresses resolved for %s", host)
				}
				return nil, lastErr
			}

			return safeConn, nil
		},
	}

	return &VirtualHTTPClient{
		client: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				if !strings.EqualFold(req.URL.Scheme, "http") && !strings.EqualFold(req.URL.Scheme, "https") {
					return fmt.Errorf("redirect blocked by sandbox security policy: unsupported scheme %q (only http/https allowed)", req.URL.Scheme)
				}
				host, port, err := net.SplitHostPort(req.URL.Host)
				if err != nil {
					host = req.URL.Host
					if strings.EqualFold(req.URL.Scheme, "https") {
						port = "443"
					} else {
						port = "80"
					}
				}
				gate := &SecurityGate{}
				if err := gate.CheckNetworkAccess(req.Context(), net.JoinHostPort(host, port)); err != nil {
					return fmt.Errorf("redirect blocked by sandbox security policy: %w", err)
				}
				return nil
			},
		},
	}
}

// Fetch executes a GET request against the target URL if allowed by active boundaries.
func (v *VirtualHTTPClient) Fetch(ctx context.Context, urlString string) (string, error) {
	u, err := url.Parse(urlString)
	if err != nil {
		return "", err
	}

	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return "", fmt.Errorf("blocked by sandbox security policy: unsupported scheme %q (only http/https allowed)", u.Scheme)
	}

	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		// Port omitted, resolve scheme default port
		host = u.Host
		if strings.EqualFold(u.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}

	// Intercept and assert permission clearance
	gate := &SecurityGate{}
	if err := gate.CheckNetworkAccess(ctx, host+":"+port); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlString, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Glassbox-Virtual-HttpClient/1.0")

	resp, err := v.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var maxBytes int64 = 50 * 1024 * 1024 // 50MB default limit
	limits := GetActiveLimits(ctx)
	if limits != nil && limits.MaxMemoryPages() != nil {
		// Cap the response body to 50% of the sandbox memory limit
		// to leave enough heap space for the guest to allocate the response string buffer.
		maxBytes = (int64(*limits.MaxMemoryPages()) * 64 * 1024) / 2
	}

	resp.Body = http.MaxBytesReader(nil, resp.Body, maxBytes)
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(bodyBytes), nil
}

// isInternalIP returns true if the IP is reserved for internal/private use,
// including Carrier-Grade NAT (RFC 6598) and other non-public ranges.
func isInternalIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Check for Carrier-Grade NAT (100.64.0.0/10)
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && (ip4[1]&0xc0) == 64 {
			return true
		}
		// Check for TEST-NET-1 (192.0.2.0/24), TEST-NET-2 (198.51.100.0/24), TEST-NET-3 (203.0.113.0/24)
		if ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2 {
			return true
		}
		if ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100 {
			return true
		}
		if ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113 {
			return true
		}
		// Benchmark Testing (198.18.0.0/15)
		if ip4[0] == 198 && (ip4[1]&0xfe) == 18 {
			return true
		}
	}
	return false
}

