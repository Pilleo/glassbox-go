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

// BenchmarkGlassboxedMarkdownRender measures glassboxed/sandboxed markdown rendering performance
func BenchmarkGlassboxedMarkdownRender(b *testing.B) {
	ctx := context.Background()
	engine, err := gruntime.NewEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		Timeout(10 * time.Millisecond).
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
