package runtime

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	gapi "github.com/glassbox-go/api"
)

// TestGetInstanceRace tests the interaction between GetInstance and LRU eviction
// by rapidly instantiating modules with many different memory limits concurrently.
// This is designed to be run with the race detector.
func TestGetInstanceRace(t *testing.T) {
	err := os.MkdirAll("wasm", 0755)
	if err != nil {
		t.Fatalf("Failed to create Wasm directory: %v", err)
	}
	defer os.RemoveAll("wasm")

	minimalWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	err = os.WriteFile("wasm/RaceTest.wasm", minimalWasm, 0644)
	if err != nil {
		t.Fatalf("Failed to write mock Wasm file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	var wg sync.WaitGroup
	// Run 100 goroutines, each requesting a module with a random memory limit
	// between 1 and 20. Since the LRU cache is limited to 10 items, this will
	// force rapid eviction while instantiations are happening.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			// Cycle memory limits to trigger eviction (>10)
			memLimit := int32((id % 15) + 1)
			limits := gapi.NewBuilder().
				MaxMemoryPages(memLimit).
				Build()

			mod, err := engine.GetInstance(ctx, "RaceTest", limits)
			if err != nil {
				// We might get timeout errors if context expires, but no panics
				if err != context.DeadlineExceeded && ctx.Err() == nil {
					t.Errorf("GetInstance failed: %v", err)
				}
				return
			}
			if mod != nil {
				engine.ReleaseInstance(ctx, mod, limits, true)
			}
		}(i)
	}

	wg.Wait()
}
