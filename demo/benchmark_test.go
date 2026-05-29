package demo

import (
	"context"
	"testing"
	"time"

	gapi "github.com/glassbox-go/api"
	gbridge "github.com/glassbox-go/binarybridge"
	gruntime "github.com/glassbox-go/runtime"
)

// BenchmarkStandardSerialization measures MessagePack serializing a slice of 10,000 float32s
func BenchmarkStandardSerialization(b *testing.B) {
	input := make([]float32, 10000)
	for i := range input {
		input[i] = float32(i) * 1.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := gbridge.SerializeAsBytes(input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkZeroCopySerialization measures zero-copy memory reinterpreting of a slice of 10,000 float32s
func BenchmarkZeroCopySerialization(b *testing.B) {
	input := make([]float32, 10000)
	for i := range input {
		input[i] = float32(i) * 1.5
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bytes := gbridge.ZeroCopyFloat32ToBytes(input)
		if bytes == nil {
			b.Fatal("expected non-nil bytes")
		}
	}
}

// BenchmarkNativeYAMLParse measures direct unsandboxed yaml parsing performance
func BenchmarkNativeYAMLParse(b *testing.B) {
	impl := &YAMLParserImpl{}
	yamlData := []byte("name: Glassbox-Go\nversion: 1.0.0\nsecure: true\n")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := impl.Parse(ctx, yamlData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGlassboxedYAMLParse measures glassboxed/sandboxed yaml parsing performance (including context limits, timeouts, and boundaries validation)
func BenchmarkGlassboxedYAMLParse(b *testing.B) {
	gruntime.ClearCache()
	limits := gapi.NewBuilder().
		Timeout(10 * time.Millisecond).
		Build()
	proxy := NewYAMLParserWasmProxy(limits)
	yamlData := []byte("name: Glassbox-Go\nversion: 1.0.0\nsecure: true\n")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := proxy.Parse(ctx, yamlData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

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

// BenchmarkGlassboxedMarkdownRender measures glassboxed/sandboxed markdown rendering performance (including context limits, timeouts, and boundaries validation)
func BenchmarkGlassboxedMarkdownRender(b *testing.B) {
	gruntime.ClearCache()
	limits := gapi.NewBuilder().
		Timeout(10 * time.Millisecond).
		Build()
	proxy := NewMarkdownParserWasmProxy(limits)
	markdown := []byte("# Heading\nHello world from benchmark.")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := proxy.Render(ctx, markdown)
		if err != nil {
			b.Fatal(err)
		}
	}
}
