package api

import (
	"context"
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
	return &VirtualHTTPClient{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Fetch executes a GET request against the target URL if allowed by active boundaries.
func (v *VirtualHTTPClient) Fetch(ctx context.Context, urlString string) (string, error) {
	u, err := url.Parse(urlString)
	if err != nil {
		return "", err
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(bodyBytes), nil
}
