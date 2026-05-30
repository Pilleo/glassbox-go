package runtime

import (
	"context"
	"os"
	"strings"
	"testing"

	gapi "github.com/glassbox-go/api"
)

func TestSafeLogWriter(t *testing.T) {
	var loggedLvl gapi.LogLevel
	var loggedMsg string

	logger := func(lvl gapi.LogLevel, msg string) {
		loggedLvl = lvl
		loggedMsg = msg
	}

	writer := &safeLogWriter{
		logger: logger,
	}

	// Write standard print log bytes with trailing newline
	p := []byte("Hello wazero logger!\n")
	n, err := writer.Write(p)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(p) {
		t.Errorf("Expected n=%d, got: %d", len(p), n)
	}

	if loggedLvl != gapi.LevelInfo {
		t.Errorf("Expected level INFO, got: %v", loggedLvl)
	}
	if loggedMsg != "Hello wazero logger!" { // Ensure trailing newline was stripped
		t.Errorf("Expected stripped message, got: %s", loggedMsg)
	}
}

func TestWasmCompilationAndCaching(t *testing.T) {
	// Create temporary wasm directory in the root so loadWasmBytes can find it
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	// Write minimal valid Wasm binary
	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/EngineCacheTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	limits := gapi.NewBuilder().
		PermissiveMode().
		Build()

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	// 1. First compilation: loads from disk and JIT compiles
	mod1, err := engine.GetInstance(ctx, "EngineCacheTest", limits)
	if err != nil {
		t.Fatalf("GetInstance first load failed: %v", err)
	}
	if mod1 == nil {
		t.Fatalf("Expected non-nil module instance")
	}

	// Verify it was successfully cached in our global cache
	engine.cacheMutex.RLock()
	_, exists := engine.moduleCache["EngineCacheTest"]
	engine.cacheMutex.RUnlock()
	if !exists {
		t.Errorf("Expected module to be cached in moduleCache")
	}

	// Close the first instance to unregister its name and allow the next instance to use the same name
	mod1.Close(ctx)

	// 2. Second compilation: should hit compilation cache
	mod2, err := engine.GetInstance(ctx, "EngineCacheTest", limits)
	if err != nil {
		t.Fatalf("GetInstance cached load failed: %v", err)
	}
	if mod2 == nil {
		t.Fatalf("Expected non-nil cached module instance")
	}
	mod2.Close(ctx)
}

func TestLoadWasmBytesErrors(t *testing.T) {
	limits := gapi.NewBuilder().
		Build()

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	// Try to get an instance of a non-existent module, should fail cleanly with standard file stat errors
	_, err = engine.GetInstance(ctx, "NonExistentModule-ABC-123", limits)
	if err == nil {
		t.Fatalf("Expected error loading non-existent module, got nil")
	}

	if !strings.Contains(err.Error(), "no such file or directory") {
		t.Errorf("Expected file not found error, got: %v", err)
	}
}

func TestWasmWithMemoryLimitPages(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/MemoryLimitTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	// 2 pages limit
	limits := gapi.NewBuilder().
		MaxMemoryPages(2).
		Build()

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	mod, err := engine.GetInstance(ctx, "MemoryLimitTest", limits)
	if err != nil {
		t.Fatalf("GetInstance with memory limits failed: %v", err)
	}
	if mod == nil {
		t.Fatalf("Expected non-nil module instance")
	}
	mod.Close(ctx)
}

func TestWasmWithAllowedDirectories(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/DirLimitTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "sandbox-test-dir-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	limits := gapi.NewBuilder().
		AllowFileSystemAccess(tempDir).
		Build()

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	mod, err := engine.GetInstance(ctx, "DirLimitTest", limits)
	if err != nil {
		t.Fatalf("GetInstance with directory limits failed: %v", err)
	}
	if mod == nil {
		t.Fatalf("Expected non-nil module instance")
	}
	mod.Close(ctx)
}

func TestWasmWithLoggerRedirection(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/LoggerLimitTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	var loggedLvl gapi.LogLevel
	var loggedMsg string
	logger := func(lvl gapi.LogLevel, msg string) {
		loggedLvl = lvl
		loggedMsg = msg
	}

	limits := gapi.NewBuilder().
		Logger(logger).
		Build()

	limits.Logger()(gapi.LevelWarn, "Simulated warning log")
	if loggedLvl != gapi.LevelWarn || loggedMsg != "Simulated warning log" {
		t.Errorf("Expected logger callback to function correctly")
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	mod, err := engine.GetInstance(ctx, "LoggerLimitTest", limits)
	if err != nil {
		t.Fatalf("GetInstance with custom logger failed: %v", err)
	}
	if mod == nil {
		t.Fatalf("Expected non-nil module instance")
	}
	mod.Close(ctx)
}

func TestWasmCompileError(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	// Invalid wasm bytes
	invalidWasm := []byte{0x11, 0x22, 0x33, 0x44, 0x55}
	err = os.WriteFile("wasm/InvalidWasmTest.wasm", invalidWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid Wasm file: %v", err)
	}

	limits := gapi.NewBuilder().Build()
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	_, err = engine.GetInstance(ctx, "InvalidWasmTest", limits)
	if err == nil {
		t.Fatalf("Expected compile error on invalid Wasm, got nil")
	}
	if !strings.Contains(err.Error(), "failed to JIT compile wasm module") {
		t.Errorf("Expected compile error message, got: %v", err)
	}
}

func TestWasmCompileErrorWithLimits(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	// Invalid wasm bytes
	invalidWasm := []byte{0x11, 0x22, 0x33, 0x44, 0x55}
	err = os.WriteFile("wasm/InvalidWasmTestLimits.wasm", invalidWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid Wasm file: %v", err)
	}

	limits := gapi.NewBuilder().
		MaxMemoryPages(2).
		Build()
	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	_, err = engine.GetInstance(ctx, "InvalidWasmTestLimits", limits)
	if err == nil {
		t.Fatalf("Expected compile error on invalid Wasm with memory limits, got nil")
	}
	if !strings.Contains(err.Error(), "failed to JIT compile wasm module") {
		t.Errorf("Expected compile error message, got: %v", err)
	}
}
