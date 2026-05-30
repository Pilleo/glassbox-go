package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gapi "github.com/glassbox-go/api"
)

func TestSandboxHostFunctions_FetchHTTP(t *testing.T) {
	// 1. Create a mock HTTP server to hit
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("sandbox-host-test-response"))
	}))
	defer ts.Close()

	// 2. Create the Wasm guest source code
	tempDir, err := os.MkdirTemp("", "host-test-wasm-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockSrc := `package main

import "unsafe"

//go:wasmimport gobox_host fetch_http
func fetchHTTP(urlPtr uint32, urlLen uint32) uint64

//go:wasmexport fetch_test
func FetchTest(urlPtr uint32, urlLen uint32) uint64 {
	return fetchHTTP(urlPtr, urlLen)
}

//go:wasmexport malloc
func malloc(size uint32) uint32 {
	b := make([]byte, size)
	return uint32(uintptr(unsafe.Pointer(&b[0])))
}

func main() {}
`
	srcPath := filepath.Join(tempDir, "main.go")
	err = os.WriteFile(srcPath, []byte(mockSrc), 0644)
	if err != nil {
		t.Fatalf("Failed to write mock source: %v", err)
	}

	// 3. Compile it to Wasm using standard Go wasip1
	wasmPath := filepath.Join(tempDir, "HostTest.wasm")
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", wasmPath, "main.go")
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("Skipping test because go build for wasip1 failed (requires Go 1.21+). Output: %s", string(output))
	}

	// Move the wasm to our local "wasm" dir so Engine can find it
	os.MkdirAll("wasm", 0755)
	defer os.RemoveAll("wasm")
	
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("Failed to read compiled wasm: %v", err)
	}
	err = os.WriteFile("wasm/HostTest.wasm", wasmBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to copy wasm to local dir: %v", err)
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	// 4. Test 1: Without network permission, fetch_http should return failure (0xFFFFFFFFFFFFFFFF)
	limitsBlocked := gapi.NewBuilder().Build()
	
	modBlocked, err := engine.GetInstance(ctx, "HostTest", limitsBlocked)
	if err != nil {
		t.Fatalf("Failed to get instance: %v", err)
	}
	defer modBlocked.Close(ctx)

	fetchFuncBlocked := modBlocked.ExportedFunction("fetch_test")
	if fetchFuncBlocked == nil {
		t.Fatalf("fetch_test function not found in wasm module")
	}

	// Write URL to memory
	urlBytes := []byte(ts.URL)
	urlPtr := uint32(1024) // Arbitrary safe memory location
	modBlocked.Memory().Write(urlPtr, urlBytes)

	ctxBlocked := gapi.WithActiveLimits(ctx, limitsBlocked)
	res, err := fetchFuncBlocked.Call(ctxBlocked, uint64(urlPtr), uint64(len(urlBytes)))
	if err != nil {
		t.Fatalf("Wasm call failed: %v", err)
	}

	if res[0] != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("Expected fetch to be blocked (0xFFFFFFFFFFFFFFFF), got %x", res[0])
	}

	// 5. Test 2: With network permission, it should succeed and return the string
	addr := strings.TrimPrefix(ts.URL, "http://")
	limitsAllowed := gapi.NewBuilder().AllowNetworkAddresses(addr).Build()
	
	modAllowed, err := engine.GetInstance(ctx, "HostTest", limitsAllowed)
	if err != nil {
		t.Fatalf("Failed to get instance: %v", err)
	}
	defer modAllowed.Close(ctx)

	fetchFuncAllowed := modAllowed.ExportedFunction("fetch_test")
	if fetchFuncAllowed == nil {
		t.Fatalf("fetch_test function not found in wasm module")
	}

	modAllowed.Memory().Write(urlPtr, urlBytes)

	ctxAllowed := gapi.WithActiveLimits(ctx, limitsAllowed)
	resAllowed, err := fetchFuncAllowed.Call(ctxAllowed, uint64(urlPtr), uint64(len(urlBytes)))
	if err != nil {
		t.Fatalf("Wasm call failed: %v", err)
	}

	if resAllowed[0] == 0xFFFFFFFFFFFFFFFF {
		t.Fatalf("Expected fetch to succeed, but was blocked or failed internally")
	}

	// Decode response
	respPtr := uint32(resAllowed[0] >> 32)
	respLen := uint32(resAllowed[0] & 0xFFFFFFFF)

	respBytes, ok := modAllowed.Memory().Read(respPtr, respLen)
	if !ok {
		t.Fatalf("Failed to read response from memory")
	}

	respStr := string(respBytes)
	if respStr != "sandbox-host-test-response" {
		t.Errorf("Expected response 'sandbox-host-test-response', got '%s'", respStr)
	}
}
