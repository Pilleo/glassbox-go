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
	ctx := context.Background()
	engine, err := gruntime.NewEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		Timeout(10 * time.Millisecond).
		Build()
	proxy, err := NewYAMLParserWasmProxy(engine, limits)
	if err != nil {
		b.Fatal(err)
	}
	yamlData := []byte("name: Glassbox-Go\nversion: 1.0.0\nsecure: true\n")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := proxy.Parse(ctx, yamlData)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Compute Benchmarks

func benchmarkNativeCompute(b *testing.B, iterations int) {
	impl := &ComputeWorkerImpl{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := impl.Process(ctx, iterations)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkWasmCompute(b *testing.B, iterations int) {
	ctx := context.Background()
	engine, err := gruntime.NewEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		Timeout(5 * time.Second).
		Build()
	proxy, err := NewComputeWorkerWasmProxy(engine, limits)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := proxy.Process(ctx, iterations)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkComputeNative_10(b *testing.B)    { benchmarkNativeCompute(b, 10) }
func BenchmarkComputeWasm_10(b *testing.B)      { benchmarkWasmCompute(b, 10) }

func BenchmarkComputeNative_1000(b *testing.B)  { benchmarkNativeCompute(b, 1000) }
func BenchmarkComputeWasm_1000(b *testing.B)    { benchmarkWasmCompute(b, 1000) }

func BenchmarkComputeNative_50000(b *testing.B) { benchmarkNativeCompute(b, 50000) }
func BenchmarkComputeWasm_50000(b *testing.B)   { benchmarkWasmCompute(b, 50000) }

/*
Benchmark Results (Compute iterations):

goos: linux
goarch: amd64
pkg: github.com/glassbox-go/demo

BenchmarkComputeNative_10-4             144932        7828 ns/op
BenchmarkComputeWasm_10-4                   75    19554683 ns/op
BenchmarkComputeNative_1000-4             1465      824229 ns/op
BenchmarkComputeWasm_1000-4                 24    45884611 ns/op
BenchmarkComputeNative_50000-4              25    44631167 ns/op
BenchmarkComputeWasm_50000-4                 1  1832119475 ns/op

Observation:
For very small tasks (10 iterations), Wasm overhead (including instantiation and context setup) makes it significantly slower (ns vs ms scale).
As computation complexity increases (50000 iterations), the overhead of Wasm communication/setup becomes proportionately smaller, though memory and serialization bottlenecks start playing a role.
*/
