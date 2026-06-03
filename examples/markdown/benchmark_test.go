package markdown

import (
	"context"
	"testing"
	"time"

	gapi "github.com/glassbox-go/api"
	gruntime "github.com/glassbox-go/runtime"
)

// BenchmarkNativeMarkdownRender measures direct unsandboxed markdown rendering performance
func BenchmarkNativeMarkdownRender(b *testing.B) {
	impl := &MarkdownParserImpl{}
	markdown := []byte("# Heading\nHello world from benchmark.")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := impl.Render(ctx, markdown)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkGlassboxedMarkdownRender(b *testing.B, pooled bool) {
	ctx := context.Background()
	engine, err := gruntime.NewEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		Timeout(500 * time.Millisecond).
		PoolInstances(pooled).
		WasmPath("../../wasm"). // Target the compiled wasm in workspace root
		Build()
	proxy, err := NewMarkdownParserWasmProxy(engine, limits)
	if err != nil {
		b.Fatal(err)
	}
	markdown := []byte("# Heading\nHello world from benchmark.")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := proxy.Render(ctx, markdown)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGlassboxedMarkdownRender_NonPooled measures glassboxed markdown rendering without instance reuse.
func BenchmarkGlassboxedMarkdownRender_NonPooled(b *testing.B) {
	benchmarkGlassboxedMarkdownRender(b, false)
}

// BenchmarkGlassboxedMarkdownRender_Pooled measures glassboxed markdown rendering with instance reuse.
func BenchmarkGlassboxedMarkdownRender_Pooled(b *testing.B) {
	benchmarkGlassboxedMarkdownRender(b, true)
}
