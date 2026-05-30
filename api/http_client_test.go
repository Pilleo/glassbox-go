package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVirtualHTTPClient_MaxBytesTruncation(t *testing.T) {
	// Create a test server that returns 1MB of data
	largeData := strings.Repeat("a", 1024*1024)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(largeData))
	}))
	defer ts.Close()

	client := NewVirtualHTTPClient()

	// 1. Without limits, it should default to 50MB and succeed
	ctx := context.Background()
	
	// Temporarily bypass network gate for the test server address
	// Since the server binds to 127.0.0.1, we must explicitly allow it.
	addr := strings.TrimPrefix(ts.URL, "http://")
	limits := NewBuilder().AllowNetworkAddresses(addr).Build()
	ctxWithLimits := WithActiveLimits(ctx, limits)

	resp, err := client.Fetch(ctxWithLimits, ts.URL)
	if err != nil {
		t.Fatalf("Expected success without breaching default limits, got error: %v", err)
	}
	if len(resp) != len(largeData) {
		t.Fatalf("Expected %d bytes, got %d", len(largeData), len(resp))
	}

	// 2. With 1-page memory limit (64KB), it should fail with "request body too large"
	// instead of returning truncated data silently.
	limitsSmall := NewBuilder().
		AllowNetworkAddresses(addr).
		MaxMemoryPages(1). // 64KB
		Build()
	
	ctxSmall := WithActiveLimits(ctx, limitsSmall)
	
	respSmall, errSmall := client.Fetch(ctxSmall, ts.URL)
	if errSmall == nil {
		t.Fatalf("Expected error when response exceeds MaxBytesReader limit, but got success with %d bytes", len(respSmall))
	}
	
	if !strings.Contains(errSmall.Error(), "http: request body too large") {
		t.Errorf("Expected 'request body too large' error, got: %v", errSmall)
	}

	// 3. To prevent Wasm OOM when allocating the result string, the limit must be < 100% of memory.
	// We test that a 48KB response fails under a 64KB total memory limit (since limit should be 32KB).
	tsMedium := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("b", 48*1024))) // 48KB
	}))
	defer tsMedium.Close()

	addrMedium := strings.TrimPrefix(tsMedium.URL, "http://")
	limitsMedium := NewBuilder().AllowNetworkAddresses(addrMedium).MaxMemoryPages(1).Build()
	ctxMedium := WithActiveLimits(ctx, limitsMedium)

	_, errMedium := client.Fetch(ctxMedium, tsMedium.URL)
	if errMedium == nil {
		t.Fatalf("Expected error for 48KB response under 64KB total memory limit (expected max body size 32KB), but got success")
	}
}

func TestIsInternalIP(t *testing.T) {
	tests := []struct {
		ipStr    string
		internal bool
	}{
		// Public IPs
		{"8.8.8.8", false},
		{"142.250.190.46", false},
		{"93.184.216.34", false},
		
		// Loopback
		{"127.0.0.1", true},
		{"127.0.53.53", true},
		{"::1", true},
		
		// Private (RFC 1918)
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.0", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		
		// AWS Metadata / Link local
		{"169.254.169.254", true},
		{"169.254.0.1", true},
		{"fe80::1", true},
		
		// Carrier-grade NAT
		{"100.64.0.1", true},
		{"100.127.255.255", true},
		
		// Unspecified
		{"0.0.0.0", true},
		{"::", true},
	}

	for _, tt := range tests {
		t.Run(tt.ipStr, func(t *testing.T) {
			ip := net.ParseIP(tt.ipStr)
			if ip == nil {
				t.Fatalf("Invalid test IP: %s", tt.ipStr)
			}
			
			result := isInternalIP(ip)
			if result != tt.internal {
				t.Errorf("IP %s: expected internal=%v, got %v", tt.ipStr, tt.internal, result)
			}
		})
	}
}
