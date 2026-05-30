package runtime

import (
	"context"
	"os"
	"testing"

	gapi "github.com/glassbox-go/api"
)

func TestEngineLRUEviction(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/LRUTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	// Create 15 limits (LRU size is 10)
	for i := int32(1); i <= 15; i++ {
		limits := gapi.NewBuilder().MaxMemoryPages(i).Build()
		mod, err := engine.GetInstance(ctx, "LRUTest", limits)
		if err != nil {
			t.Fatalf("Failed to get instance for limit %d: %v", i, err)
		}
		mod.Close(ctx)
	}

	engine.cacheMutex.RLock()
	cacheSize := len(engine.limitedRTs)
	engine.cacheMutex.RUnlock()

	// It should be exactly 10 because the cache eviction kicks in at >= 10
	if cacheSize != 10 {
		t.Errorf("Expected LRU cache to limit at 10, got: %d", cacheSize)
	}
}

func TestEngineInstancePooling(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/PoolTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().PoolInstances(true).Build()

	mod1, err := engine.GetInstance(ctx, "PoolTest", limits)
	if err != nil {
		t.Fatalf("Failed to get instance: %v", err)
	}

	// Release it to the pool
	engine.ReleaseInstance(ctx, mod1, limits, true)

	// Get it again, it should be the SAME instance
	mod2, err := engine.GetInstance(ctx, "PoolTest", limits)
	if err != nil {
		t.Fatalf("Failed to get instance: %v", err)
	}

	if mod1.Name() != mod2.Name() {
		t.Errorf("Expected to receive the pooled instance (%s), but got a new one (%s)", mod1.Name(), mod2.Name())
	}

	engine.ReleaseInstance(ctx, mod2, limits, true)
}
