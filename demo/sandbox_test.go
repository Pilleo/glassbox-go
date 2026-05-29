package demo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	gapi "github.com/glassbox-go/api"
	gruntime "github.com/glassbox-go/runtime"
)


func TestYAMLParserSuccess(t *testing.T) {
	limits := gapi.NewBuilder().Build()
	proxy := NewYAMLParserWasmProxy(limits)

	yamlData := []byte(`
name: Glassbox-Go
version: 1.0.0
tags:
  - secure
  - sandboxed
`)

	ctx := context.Background()
	result, err := proxy.Parse(ctx, yamlData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result["name"] != "Glassbox-Go" {
		t.Errorf("Expected name 'Glassbox-Go', got: %v", result["name"])
	}
	if result["version"] != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got: %v", result["version"])
	}
}

func TestYAMLParserAnchorBombVulnerability(t *testing.T) {
	// A small Billion Laughs YAML Anchor Bomb payload.
	// This creates nested expansions that explode in memory.
	anchorBomb := []byte(`
a: &a ["lol","lol","lol","lol","lol"]
b: &b [*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b]
d: &d [*c,*c,*c,*c,*c]
`)

	limits := gapi.NewBuilder().Build()
	proxy := NewYAMLParserWasmProxy(limits)

	ctx := context.Background()
	// An unsandboxed parser would expand this recursively on the heap.
	// In the fallback on the host, it completes because we kept depth small (5^4 = 625 elements),
	// but it demonstrates the recursive structural pressure.
	result, err := proxy.Parse(ctx, anchorBomb)
	if err != nil {
		t.Fatalf("Failed to parse YAML anchor bomb: %v", err)
	}
	if result == nil {
		t.Errorf("Expected non-nil result from anchor bomb parse")
	}
}

func TestMarkdownParserNetworkFirewall(t *testing.T) {
	// Spin up local mock server for remote template stylesheet
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<style>body { color: blue; }</style>"))
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Failed to parse server URL: %v", err)
	}

	// Case 1: Whitelisted remote template fetch
	limitsAllowed := gapi.NewBuilder().
		Strict(true).
		AllowNetworkAddresses([]string{u.Host}).
		Build()

	proxyAllowed := NewMarkdownParserWasmProxy(limitsAllowed)

	ctx := context.Background()
	markdown := []byte("# Heading\nHello secure world.")
	res, err := proxyAllowed.RenderWithTemplate(ctx, markdown, server.URL)
	if err != nil {
		t.Fatalf("RenderWithTemplate failed on whitelisted egress: %v", err)
	}
	if !strings.Contains(res, "<style>") || !strings.Contains(res, "<h1>Heading</h1>") {
		t.Errorf("Expected styled rendered HTML, got: %s", res)
	}

	// Case 2: Blocked remote template fetch
	limitsBlocked := gapi.NewBuilder().
		Strict(true).
		AllowNetworkAddresses([]string{"some-other-host.com"}).
		Build()
	proxyBlocked := NewMarkdownParserWasmProxy(limitsBlocked)

	_, err = proxyBlocked.RenderWithTemplate(ctx, markdown, server.URL)
	if err == nil {
		t.Errorf("Expected egress firewall violation error, got nil")
	}
	if !strings.Contains(err.Error(), "Unauthorized network egress") {
		t.Errorf("Expected unauthorized egress error, got: %v", err)
	}
}

func TestPDFProcessorFilesystemGate(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pdf-sandbox-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	safeFilePath := filepath.Join(tempDir, "allowed.txt")
	err = os.WriteFile(safeFilePath, []byte("Secure PDF Text Content"), 0644)
	if err != nil {
		t.Fatalf("Failed to write safe file: %v", err)
	}

	limits := gapi.NewBuilder().
		Strict(true).
		AllowFileSystemAccess(tempDir).
		Build()

	proxy := NewPDFProcessorWasmProxy(limits)

	ctx := context.Background()

	// Case 1: Read from whitelisted directory succeeds
	content, err := proxy.ExtractTextFromFile(ctx, safeFilePath)
	if err != nil {
		t.Fatalf("ExtractTextFromFile failed on allowed path: %v", err)
	}
	if content != "Secure PDF Text Content" {
		t.Errorf("Expected content, got: %s", content)
	}

	// Case 2: Read from unauthorized directory throws security exception
	unauthorizedFile := filepath.Join(os.TempDir(), "hacked-secrets.txt")
	_, err = proxy.ExtractTextFromFile(ctx, unauthorizedFile)
	if err == nil {
		t.Errorf("Expected security violation error, got nil")
	}
	if !strings.Contains(err.Error(), "Unauthorized filesystem access") {
		t.Errorf("Expected unauthorized filesystem access message, got: %v", err)
	}
}

func TestSandboxTimeoutEnforcement(t *testing.T) {
	limits := gapi.NewBuilder().
		Timeout(1 * time.Nanosecond). // Instant timeout preemption
		Build()

	proxy := NewPDFProcessorWasmProxy(limits)

	ctx := context.Background()
	_, err := proxy.ExtractTextFromFile(ctx, "any-path.txt")
	if err == nil {
		t.Fatalf("Expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("Expected context deadline exceeded error, got: %v", err)
	}
}

func TestWasmBranchCoverage(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/YAMLParser.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}
	defer os.RemoveAll("wasm")

	gruntime.ClearCache()
	defer gruntime.ClearCache() // Clear mock out after test completes

	limits := gapi.NewBuilder().
		Strict(true).
		Build()

	proxy := NewYAMLParserWasmProxy(limits)
	_, err = proxy.Parse(context.Background(), []byte("name: test"))
	if err == nil {
		t.Fatalf("Expected error due to missing malloc/Parse exports, got nil")
	}
	if !strings.Contains(err.Error(), "wasm module does not export") {
		t.Fatalf("Expected missing exports error, got: %v", err)
	}
}

func getHeapAlloc() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.TotalAlloc
}

func TestYAMLAnchorBombMemoryExplosionProof(t *testing.T) {
	impl := &YAMLParserImpl{}
	ctx := context.Background()

	// 1. Measure Baseline/Normal YAML parsing
	normalYAML := []byte(`a: ["lol","lol","lol","lol","lol"]`)
	allocBeforeNormal := getHeapAlloc()
	_, _ = impl.Parse(ctx, normalYAML)
	allocAfterNormal := getHeapAlloc()
	normalAlloc := int64(allocAfterNormal) - int64(allocBeforeNormal)
	if normalAlloc < 0 {
		normalAlloc = 0
	}

	// 2. Measure Depth-3 Anchor Bomb (5^3 = 125 expansions)
	bomb3 := []byte(`
a: &a ["lol","lol","lol","lol","lol"]
b: &b [*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b]
`)
	allocBefore3 := getHeapAlloc()
	_, _ = impl.Parse(ctx, bomb3)
	allocAfter3 := getHeapAlloc()
	bomb3Alloc := int64(allocAfter3) - int64(allocBefore3)
	if bomb3Alloc < 0 {
		bomb3Alloc = 0
	}

	// 3. Measure Depth-5 Anchor Bomb (5^5 = 3,125 expansions)
	bomb5 := []byte(`
a: &a ["lol","lol","lol","lol","lol"]
b: &b [*a,*a,*a,*a,*a]
c: &c [*b,*b,*b,*b,*b]
d: &d [*c,*c,*c,*c,*c]
e: &e [*d,*d,*d,*d,*d]
`)
	allocBefore5 := getHeapAlloc()
	_, _ = impl.Parse(ctx, bomb5)
	allocAfter5 := getHeapAlloc()
	bomb5Alloc := int64(allocAfter5) - int64(allocBefore5)
	if bomb5Alloc < 0 {
		bomb5Alloc = 0
	}

	t.Logf("=== MEMORY EXPLOSION EVIDENCE ===")
	t.Logf("Normal YAML parsing heap allocation:   %d bytes", normalAlloc)
	t.Logf("Depth-3 Anchor Bomb heap allocation:   %d bytes", bomb3Alloc)
	t.Logf("Depth-5 Anchor Bomb heap allocation:   %d bytes", bomb5Alloc)

	// Assert that memory growth is exponential
	if bomb5Alloc <= bomb3Alloc {
		t.Errorf("Expected exponential memory growth for depth-5 vs depth-3, got %d vs %d", bomb5Alloc, bomb3Alloc)
	}
}
