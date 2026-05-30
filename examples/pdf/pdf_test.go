package pdf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gapi "github.com/glassbox-go/api"
	gruntime "github.com/glassbox-go/runtime"
)

func TestPDFProcessorFilesystemGate(t *testing.T) {
	ctx := context.Background()
	engine, err := gruntime.NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

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
		AllowFileSystemAccess(tempDir).
		WasmPath("../../wasm").
		Build()

	proxy, err := NewPDFProcessorWasmProxy(engine, limits)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	// Case 1: Read from whitelisted directory succeeds
	content, err := proxy.ExtractTextFromFile(ctx, gapi.SandboxPath(safeFilePath))
	if err != nil {
		t.Fatalf("ExtractTextFromFile failed on allowed path: %v", err)
	}
	if content != "Secure PDF Text Content" {
		t.Errorf("Expected content, got: %s", content)
	}

	// Case 2: Read from unauthorized directory throws security exception
	unauthorizedFile := filepath.Join(os.TempDir(), "hacked-secrets.txt")
	_, err = proxy.ExtractTextFromFile(ctx, gapi.SandboxPath(unauthorizedFile))
	if err == nil {
		t.Errorf("Expected security violation error, got nil")
	}
	if !strings.Contains(err.Error(), "Unauthorized filesystem access") {
		t.Errorf("Expected unauthorized filesystem access message, got: %v", err)
	}
}

func TestSandboxTimeoutEnforcement(t *testing.T) {
	ctx := context.Background()
	engine, err := gruntime.NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		Timeout(1 * time.Nanosecond). // Instant timeout preemption
		WasmPath("../../wasm").
		Build()

	proxy, err := NewPDFProcessorWasmProxy(engine, limits)
	if err != nil {
		t.Fatalf("Failed to create proxy: %v", err)
	}

	_, err = proxy.ExtractTextFromFile(ctx, gapi.SandboxPath("any-path.txt"))
	if err == nil {
		t.Fatalf("Expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("Expected context deadline exceeded error, got: %v", err)
	}
}
