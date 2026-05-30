package runtime

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	gapi "github.com/glassbox-go/api"
	wapi "github.com/tetratelabs/wazero/api"
)

func TestEnginePoolConcurrency(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	// We want to force heavy contention on the pool channels
	limits := gapi.NewBuilder().
		PoolInstances(true).
		Build()

	var wg sync.WaitGroup
	// Spawn 500 goroutines to rapidly acquire and release modules
	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			mod, err := engine.GetInstance(ctx, "PoolTest", limits)
			if err != nil {
				// We might get context deadline exceeded, but no panics
				if err != context.DeadlineExceeded && ctx.Err() == nil {
					t.Errorf("GetInstance failed in pool test: %v", err)
				}
				return
			}

			// Simulate some work
			time.Sleep(1 * time.Millisecond)

			if mod != nil {
				engine.ReleaseInstance(ctx, mod, limits, true)
			}
		}()
	}

	wg.Wait()

	// Verify that the pool exists and has not exceeded capacity
	poolKey := "PoolTest"
	engine.cacheMutex.RLock()
	pool, exists := engine.instancePool[poolKey]
	engine.cacheMutex.RUnlock()

	if !exists {
		t.Fatalf("Expected pool %s to exist, but it doesn't", poolKey)
	}

	if cap(pool) != 50 {
		t.Errorf("Expected pool capacity to be 50, got %d", cap(pool))
	}
}

func TestEngineClearCache(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/ClearCacheTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		PoolInstances(true).
		Build()

	// Get and release instances to populate the pool
	var mods []wapi.Module
	for i := 0; i < 5; i++ {
		mod, err := engine.GetInstance(ctx, "ClearCacheTest", limits)
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}
		mods = append(mods, mod)
	}

	for _, mod := range mods {
		engine.ReleaseInstance(ctx, mod, limits, true)
	}

	// Verify pool is populated
	engine.cacheMutex.RLock()
	poolKey := "ClearCacheTest"
	pool, exists := engine.instancePool[poolKey]
	engine.cacheMutex.RUnlock()

	if !exists || len(pool) != 5 {
		t.Fatalf("Expected pool to have 5 instances, got %d", len(pool))
	}

	// Now clear the cache
	engine.ClearCache()

	// Verify cache and pools are cleared
	engine.cacheMutex.RLock()
	_, poolExistsAfter := engine.instancePool[poolKey]
	_, moduleExistsAfter := engine.moduleCache["ClearCacheTest"]
	engine.cacheMutex.RUnlock()

	if poolExistsAfter {
		t.Errorf("Expected instance pool to be deleted after ClearCache")
	}
	if moduleExistsAfter {
		t.Errorf("Expected module cache to be deleted after ClearCache")
	}
}

func TestEnginePoolTrappedModule(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/TrapTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	ctx := context.Background()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		PoolInstances(true).
		Build()

	mod, err := engine.GetInstance(ctx, "TrapTest", limits)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	// Release it with success = false (simulate a trap/panic in guest)
	engine.ReleaseInstance(ctx, mod, limits, false)

	// Verify the module was NOT returned to the pool
	engine.cacheMutex.RLock()
	poolKey := "TrapTest"
	pool, exists := engine.instancePool[poolKey]
	engine.cacheMutex.RUnlock()

	// It may exist but must be empty
	if exists && len(pool) > 0 {
		t.Fatalf("Expected pool to be empty for trapped module, got %d instances", len(pool))
	}
}
