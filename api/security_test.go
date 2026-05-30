package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityGate_SymlinkBreakout(t *testing.T) {
	// Create a temporary sandbox directory
	tempDir, err := os.MkdirTemp("", "glassbox-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create an allowed directory inside the sandbox
	allowedDir := filepath.Join(tempDir, "allowed")
	err = os.MkdirAll(allowedDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create allowed dir: %v", err)
	}

	// Create a secret file OUTSIDE the allowed directory
	secretFile := filepath.Join(tempDir, "secret.txt")
	err = os.WriteFile(secretFile, []byte("super secret host data"), 0644)
	if err != nil {
		t.Fatalf("Failed to write secret file: %v", err)
	}

	// Create a symlink INSIDE the allowed directory pointing OUTSIDE
	symlinkPath := filepath.Join(allowedDir, "link_to_secret")
	err = os.Symlink(secretFile, symlinkPath)
	if err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	limits := NewBuilder().AllowFileSystemAccess(allowedDir).Build()
	ctx := WithActiveLimits(context.Background(), limits)
	gate := &SecurityGate{}

	// Attempt to access the symlink which resides inside allowedDir
	err = gate.CheckFileAccess(ctx, symlinkPath)
	
	// The access MUST be blocked because it resolves outside
	if err == nil {
		t.Fatalf("Security bypass: Symlink breakout allowed! Path %s was permitted but it resolves outside the allowed prefix.", symlinkPath)
	}

	if !strings.Contains(err.Error(), "Security Sandbox Violation") {
		t.Errorf("Expected Security Sandbox Violation error, got: %v", err)
	}
}

func TestVirtualHTTPClient_DNSRebinding(t *testing.T) {
	// Start a local test server pretending to be internal cloud metadata
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "internal AWS metadata")
	}))
	defer ts.Close()

	// Extract the port of the local test server
	_, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to parse test server addr: %v", err)
	}

	// We allow 'localhost' in the network policy, which resolves to 127.0.0.1
	// The HTTPClient must verify the RESOLVED IP against the policy!
	limits := NewBuilder().AllowNetworkAddresses("localhost").Build()
	ctx := WithActiveLimits(context.Background(), limits)

	client := NewVirtualHTTPClient()

	// Attempting to fetch localhost
	// The domain "localhost" is allowed, but it resolves to 127.0.0.1 which is private.
	// Since 127.0.0.1 is not explicitly whitelisted (only the string "localhost" is), 
	// the DNS rebinding check should block it, or if it checks "127.0.0.1", it fails.
	// Actually, wait, if we explicitly whitelist "localhost", it will block the IP "127.0.0.1"
	// because "127.0.0.1" != "localhost". This proves the DNS rebinding block works!
	_, err = client.Fetch(ctx, "http://localhost:"+port)

	if err == nil {
		t.Fatalf("Security bypass: DNS Rebinding / Private IP access allowed!")
	}

	if !strings.Contains(err.Error(), "DNS rebinding blocked: resolved IP 127.0.0.1 is private/internal") {
		t.Errorf("Expected DNS rebinding blocked error, got: %v", err)
	}
}

func TestVirtualHTTPClient_DNSRebindingExplicitAllow(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "internal AWS metadata")
	}))
	defer ts.Close()

	_, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to parse test server addr: %v", err)
	}

	// If we explicitly whitelist the IP, it should pass
	limits := NewBuilder().AllowNetworkAddresses("127.0.0.1").Build()
	ctx := WithActiveLimits(context.Background(), limits)

	client := NewVirtualHTTPClient()
	_, err = client.Fetch(ctx, "http://127.0.0.1:"+port)

	if err != nil {
		t.Fatalf("Expected explicit IP whitelist to pass, got error: %v", err)
	}
}

func TestLimits_NegativeMaxMemoryPages(t *testing.T) {
	b := NewBuilder()
	// Pass a negative value
	b.MaxMemoryPages(-5)

	limits := b.Build()
	maxPages := limits.MaxMemoryPages()

	if maxPages == nil {
		t.Fatal("Expected maxMemoryPages to be non-nil")
	}

	// Should clamp to 1
	if *maxPages != 1 {
		t.Errorf("Expected negative MaxMemoryPages to clamp to 1, got: %d", *maxPages)
	}

	b.MaxMemoryPages(999999)
	limits = b.Build()
	maxPages = limits.MaxMemoryPages()
	
	if *maxPages != 65536 {
		t.Errorf("Expected large MaxMemoryPages to clamp to 65536, got: %d", *maxPages)
	}
}

func TestVirtualHTTPClient_CGNATRebinding(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "internal CGNAT data")
	}))
	defer ts.Close()

	_, port, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to parse test server addr: %v", err)
	}

	limits := NewBuilder().AllowNetworkAddresses("100.64.0.1").Build()
	ctx := WithActiveLimits(context.Background(), limits)
	
	client := NewVirtualHTTPClient()

	// If we fetch 100.64.0.1 without explicit whitelist, it blocks.
	// But wait, here it is explicitly whitelisted. So it should pass.
	_, err = client.Fetch(ctx, "http://100.64.0.1:"+port)
	if err != nil && !strings.Contains(err.Error(), "network is unreachable") && !strings.Contains(err.Error(), "no route to host") && !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("Expected explicit CGNAT IP whitelist to pass (or fail with unreachable), got error: %v", err)
	}

	// Now try without whitelist
	limitsBlocked := NewBuilder().AllowNetworkAddresses("example.com").Build()
	ctxBlocked := WithActiveLimits(context.Background(), limitsBlocked)
	clientBlocked := NewVirtualHTTPClient()
	
	_, errBlocked := clientBlocked.Fetch(ctxBlocked, "http://100.64.0.1:"+port)
	if errBlocked == nil {
		t.Fatalf("Security bypass: CGNAT / Private IP access allowed without whitelist!")
	}
}
