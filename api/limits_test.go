package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandboxLimitsBuilder(t *testing.T) {
	called := false
	logger := func(lvl LogLevel, msg string) {
		called = true
	}

	limits := NewBuilder().
		MaxMemoryPages(10).
		PermissiveMode().
		Logger(logger).
		AllowFileSystemAccess("/tmp").
		AllowFileSystemAccess(""). // Should be ignored
		AllowNetworkAddresses("localhost:8080").
		Timeout(5 * time.Second).
		Build()

	if limits.MaxMemoryPages() == nil || *limits.MaxMemoryPages() != 10 {
		t.Errorf("Expected MaxMemoryPages 10, got %v", limits.MaxMemoryPages())
	}
	if !limits.IsPermissive() {
		t.Errorf("Expected permissive true")
	}
	if limits.Timeout() != 5*time.Second {
		t.Errorf("Expected timeout 5s, got %v", limits.Timeout())
	}
	if len(limits.AllowedDirectories()) != 1 || limits.AllowedDirectories()[0] != "/tmp" {
		t.Errorf("Expected allowed directories [/tmp], got %v", limits.AllowedDirectories())
	}
	if len(limits.AllowedNetworkAddresses()) != 1 || limits.AllowedNetworkAddresses()[0] != "localhost:8080" {
		t.Errorf("Expected allowed networks [localhost:8080], got %v", limits.AllowedNetworkAddresses())
	}

	limits.Logger()(LevelInfo, "test log")
	if !called {
		t.Errorf("Expected logger callback to be invoked")
	}
}

func TestWithActiveLimits(t *testing.T) {
	ctx := context.Background()
	if GetActiveLimits(ctx) != nil {
		t.Errorf("Expected nil active limits for fresh context")
	}

	limits := NewBuilder().Build()
	ctx = WithActiveLimits(ctx, limits)
	retrieved := GetActiveLimits(ctx)
	if retrieved != limits {
		t.Errorf("Expected retrieved limits to match set limits")
	}

	// Retrieve with bad value
	badCtx := context.WithValue(context.Background(), activeLimitsKey, "not-a-limits-struct")
	if GetActiveLimits(badCtx) != nil {
		t.Errorf("Expected nil active limits for invalid type in context")
	}
}

func TestSandboxSecurityError(t *testing.T) {
	err := NewSandboxSecurityError("Access Denied")
	var secErr *SandboxSecurityError
	if !errors.As(err, &secErr) {
		t.Errorf("Expected SandboxSecurityError type")
	}
	if err.Error() != "Access Denied" {
		t.Errorf("Expected 'Access Denied' message, got: %s", err.Error())
	}
}

func TestSecurityGateFileSystem(t *testing.T) {
	gate := &SecurityGate{}

	// Case 1: No limits in context -> allowed
	ctx := context.Background()
	if err := gate.CheckFileAccess(ctx, "/etc/passwd"); err == nil {
		t.Errorf("Expected error for context without limits, got nil")
	}

	// Case 2: Permissive mode -> allowed
	limitsPermissive := NewBuilder().PermissiveMode().Build()
	ctxPermissive := WithActiveLimits(ctx, limitsPermissive)
	if err := gate.CheckFileAccess(ctxPermissive, "/etc/passwd"); err != nil {
		t.Errorf("Expected nil error for permissive limits, got %v", err)
	}

	// Case 3: Strict mode (default) with filesystem whitelisting
	tempDir, err := os.MkdirTemp("", "security-gate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	allowedPath := filepath.Join(tempDir, "allowed")
	err = os.MkdirAll(allowedPath, 0755)
	if err != nil {
		t.Fatalf("Failed to create allowed path: %v", err)
	}

	limitsStrict := NewBuilder().
		AllowFileSystemAccess(allowedPath).
		Build()
	ctxStrict := WithActiveLimits(ctx, limitsStrict)

	// Sub-case 3a: Access within allowed folder -> success
	fileInAllowed := filepath.Join(allowedPath, "file.txt")
	if err := gate.CheckFileAccess(ctxStrict, fileInAllowed); err != nil {
		t.Errorf("Expected access allowed inside %s, got: %v", allowedPath, err)
	}

	// Sub-case 3b: Access to parent or unauthorized folder -> violation
	unauthorizedFile := filepath.Join(tempDir, "hacked.txt")
	err = gate.CheckFileAccess(ctxStrict, unauthorizedFile)
	if err == nil {
		t.Errorf("Expected security violation accessing unauthorized folder")
	} else if !strings.Contains(err.Error(), "Unauthorized filesystem access") {
		t.Errorf("Expected filesystem access violation, got: %v", err)
	}

	// Sub-case 3c: Root directory whitelisting (test fix for Issue #2)
	limitsRoot := NewBuilder().
		AllowFileSystemAccess("/").
		Build()
	ctxRoot := WithActiveLimits(ctx, limitsRoot)
	if err := gate.CheckFileAccess(ctxRoot, "/etc/passwd"); err != nil {
		t.Errorf("Expected access allowed for root whitelisting, got: %v", err)
	}
}

func TestSecurityGateNetwork(t *testing.T) {
	gate := &SecurityGate{}

	// Case 1: No limits in context -> allowed
	ctx := context.Background()
	if err := gate.CheckNetworkAccess(ctx, "google.com:443"); err == nil {
		t.Errorf("Expected error for context without limits, got nil")
	}

	// Case 2: Permissive mode -> allowed
	limitsPermissive := NewBuilder().PermissiveMode().Build()
	ctxPermissive := WithActiveLimits(ctx, limitsPermissive)
	if err := gate.CheckNetworkAccess(ctxPermissive, "google.com:443"); err != nil {
		t.Errorf("Expected nil error for permissive limits, got %v", err)
	}

	// Case 3: Strict mode with whitelisted egress
	limitsStrict := NewBuilder().
		AllowNetworkAddresses("api.rates.com", "localhost:8080").
		Build()
	ctxStrict := WithActiveLimits(ctx, limitsStrict)

	// Sub-case 3a: Access whitelisted hostname exactly -> success
	if err := gate.CheckNetworkAccess(ctxStrict, "api.rates.com"); err != nil {
		t.Errorf("Expected whitelisted hostname allowed, got %v", err)
	}

	// Sub-case 3b: Access whitelisted host:port exactly -> success
	if err := gate.CheckNetworkAccess(ctxStrict, "localhost:8080"); err != nil {
		t.Errorf("Expected whitelisted host:port allowed, got %v", err)
	}

	// Sub-case 3c: Access whitelisted host:port via host:prefix match -> success
	if err := gate.CheckNetworkAccess(ctxStrict, "api.rates.com:443"); err != nil {
		t.Errorf("Expected api.rates.com:443 to match prefix api.rates.com, got %v", err)
	}

	// Sub-case 3d: Access unauthorized address -> violation
	err := gate.CheckNetworkAccess(ctxStrict, "google.com:443")
	if err == nil {
		t.Errorf("Expected network egress violation error, got nil")
	} else if !strings.Contains(err.Error(), "Unauthorized network egress") {
		t.Errorf("Expected unauthorized egress error, got: %v", err)
	}
}

func TestVirtualHTTPClient(t *testing.T) {
	// Start local mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mock response payload"))
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Failed to parse server URL: %v", err)
	}

	client := NewVirtualHTTPClient()

	// Case 1: Unrestricted context -> should fail
	ctx := context.Background()
	_, err = client.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatalf("Fetch should fail on unrestricted context")
	}

	// Case 2: Restricted whitelisted context -> should succeed
	limits := NewBuilder().
		AllowNetworkAddresses(u.Host).
		Build()
	ctxRestricted := WithActiveLimits(ctx, limits)
	resp, err := client.Fetch(ctxRestricted, server.URL)
	if err != nil {
		t.Fatalf("Fetch failed on whitelisted address: %v", err)
	}
	if resp != "mock response payload" {
		t.Errorf("Expected 'mock response payload', got: %s", resp)
	}

	// Case 3: Restricted unauthorized context -> should fail-fast
	limitsBlocked := NewBuilder().
		AllowNetworkAddresses("some-other-egress.com").
		Build()
	ctxBlocked := WithActiveLimits(ctx, limitsBlocked)
	_, err = client.Fetch(ctxBlocked, server.URL)
	if err == nil {
		t.Fatalf("Expected egress firewall violation error, got nil")
	}
	if !strings.Contains(err.Error(), "Unauthorized network egress") {
		t.Errorf("Expected firewall validation error, got: %v", err)
	}

	// Case 4: Invalid target URL -> parse error
	_, err = client.Fetch(ctx, "http://[invalid-url::1")
	if err == nil {
		t.Errorf("Expected parse error for invalid URL, got nil")
	}

	// Case 5: Default port checks (https vs http)
	limitsDefaultPort := NewBuilder().
		AllowNetworkAddresses("api.rates.com:443").
		Build()
	ctxDefaultPort := WithActiveLimits(ctx, limitsDefaultPort)
	// Triggers HTTPS branch (which defaults port to 443) -> CheckNetworkAccess whitelisted match,
	// but HTTP client.Do fails because of offline host (desired behavior, checking firewall bypass).
	_, err = client.Fetch(ctxDefaultPort, "https://api.rates.com/data")
	if err == nil {
		t.Fatalf("Expected offline error, got nil")
	}
	if strings.Contains(err.Error(), "Unauthorized network egress") {
		t.Errorf("Egress should NOT be blocked by firewall, got firewall error: %v", err)
	}

	// Same for HTTP (defaults port to 80)
	limitsHTTPPort := NewBuilder().
		AllowNetworkAddresses("api.rates.com:80").
		Build()
	ctxHTTPPort := WithActiveLimits(ctx, limitsHTTPPort)
	_, err = client.Fetch(ctxHTTPPort, "http://api.rates.com/data")
	if err == nil {
		t.Fatalf("Expected offline error, got nil")
	}
	if strings.Contains(err.Error(), "Unauthorized network egress") {
		t.Errorf("Egress should NOT be blocked by firewall, got firewall error: %v", err)
	}
}
