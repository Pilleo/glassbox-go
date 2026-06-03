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

func benchmarkGlassboxedYAMLParse(b *testing.B, pooled bool) {
	ctx := context.Background()
	engine, err := gruntime.NewEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		Timeout(500 * time.Millisecond).
		PoolInstances(pooled).
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

func BenchmarkGlassboxedYAMLParse_NonPooled(b *testing.B) { benchmarkGlassboxedYAMLParse(b, false) }
func BenchmarkGlassboxedYAMLParse_Pooled(b *testing.B)    { benchmarkGlassboxedYAMLParse(b, true) }

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

func benchmarkWasmCompute(b *testing.B, iterations int, pooled bool) {
	ctx := context.Background()
	engine, err := gruntime.NewEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close(ctx)

	limits := gapi.NewBuilder().
		Timeout(5 * time.Second).
		PoolInstances(pooled).
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

func BenchmarkComputeNative_10(b *testing.B)         { benchmarkNativeCompute(b, 10) }
func BenchmarkComputeWasm_10_NonPooled(b *testing.B) { benchmarkWasmCompute(b, 10, false) }
func BenchmarkComputeWasm_10_Pooled(b *testing.B)    { benchmarkWasmCompute(b, 10, true) }

func BenchmarkComputeNative_1000(b *testing.B)         { benchmarkNativeCompute(b, 1000) }
func BenchmarkComputeWasm_1000_NonPooled(b *testing.B) { benchmarkWasmCompute(b, 1000, false) }
func BenchmarkComputeWasm_1000_Pooled(b *testing.B)    { benchmarkWasmCompute(b, 1000, true) }

func BenchmarkComputeNative_50000(b *testing.B)         { benchmarkNativeCompute(b, 50000) }
func BenchmarkComputeWasm_50000_NonPooled(b *testing.B) { benchmarkWasmCompute(b, 50000, false) }
func BenchmarkComputeWasm_50000_Pooled(b *testing.B)    { benchmarkWasmCompute(b, 50000, true) }

/*
Benchmark Results (Compute iterations):

goos: linux
goarch: amd64
pkg: github.com/glassbox-go/demo

BenchmarkStandardSerialization-4           	    2310	    518935 ns/op
BenchmarkZeroCopySerialization-4           	611973456	         1.953 ns/op
BenchmarkNativeYAMLParse-4                 	   76692	     15438 ns/op
BenchmarkGlassboxedYAMLParse_NonPooled-4   	      81	  17388484 ns/op
BenchmarkGlassboxedYAMLParse_Pooled-4      	    1470	    814714 ns/op
BenchmarkComputeNative_10-4                	  141914	      8191 ns/op
BenchmarkComputeWasm_10_NonPooled-4        	      79	  17522309 ns/op
BenchmarkComputeWasm_10_Pooled-4           	    1722	    601927 ns/op
BenchmarkComputeNative_1000-4              	    1449	    821611 ns/op
BenchmarkComputeWasm_1000_NonPooled-4      	      22	  47202676 ns/op
BenchmarkComputeWasm_1000_Pooled-4         	      34	  37454496 ns/op
BenchmarkComputeNative_50000-4             	      26	  45794320 ns/op
BenchmarkComputeWasm_50000_NonPooled-4     	       1	1852501865 ns/op
BenchmarkComputeWasm_50000_Pooled-4        	       1	2245103848 ns/op

Observation:
For very small tasks (10 iterations), Wasm overhead (including instantiation and context setup) makes it significantly slower (ns vs ms scale). Using Pooled instances mitigates instantiation cost drastically.
As computation complexity increases (50000 iterations), the overhead of Wasm communication/setup becomes proportionately smaller, though memory and serialization bottlenecks start playing a role.
*/
