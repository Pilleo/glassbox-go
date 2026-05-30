# Future Improvements: Glassbox-Go

This document details the roadmap for advanced capabilities and architectural refinements planned for future phases of the **Glassbox-Go** framework.

---

## ✅ Recently Completed

### 1. High-Performance Module Pooling
**Status:** Completed (Phase 1.5)
**Impact:** Replaced O(N) Wasm instantiation per method call with a thread-safe `chan api.Module` pool.

### 2. Dual-Side Code Generation (Transparency)
**Status:** Completed (Phase 2.0)
**Impact:** Automated the generation of both host-side proxies and guest-side Wasm shims. Users no longer write Wasm-specific glue code.

### 3. Integrated Build Automation
**Status:** Completed (Phase 2.1)
**Impact:** Integrated Wasm compilation directly into the generator and added support for `go generate`, making the Wasm build step completely hands-off.

### 4. Context Timeout Fix for Memory-Limited Sandboxes
**Status:** Completed (Phase 2.2)
**Impact:** Memory-limited runtimes now correctly propagate `context.Context` cancellation/timeout into the Wasm guest, preventing CPU starvation from infinite loops.

### 5. Go Generics Support in Code Generator
**Status:** Completed (Phase 2.2)
**Impact:** The AST parser now correctly handles generic types (`List[T]`, `Result[K, V]`) in sandboxed interface signatures.

---

## 🚀 Future Roadmap

### 1. Function-Call Fuel Limiter (Gas Instrumentation)

#### The Vision
While cooperative context timeouts (currently implemented) halt runaway loops, a malicious plugin can still consume 100% CPU for the duration of that timeout. We want a **precise instruction quota** that preempts execution after exactly $N$ opcodes.

#### The Challenge
Requires pre-processing Wasm bytecode to inject decrement-fuel opcodes into basic blocks.

---

### 2. Multi-Module Sandbox Orchestration (Micro-Plugin Mesh)

#### The Vision
Enable multiple isolated plugins to communicate directly within the **same Wazero Namespace** without bouncing back to the Go host.

---

### 3. TinyGo Compatibility
**Status:** Researching
**Impact:** Investigate support for compiling guests with TinyGo for 100x smaller binaries (~50KB vs 6MB).

---

### 4. Zero-Copy Shared Memory (Large Data Optimization)
Utilize shared memory and atomics to avoid serialization overhead for multi-megabyte payloads.

---

### 5. Option A — Replace MessagePack with Canonical ABI Encoding
**Status:** Planned
**Motivation:** The Wasm Component Model's Canonical ABI defines a compact, standardized way to pass data across the host/guest boundary. For common Go types (byte slices, strings, structs), the Canonical ABI writes `(ptr, len)` pairs directly — no header bytes, no length-prefix encoding, no allocation needed for the format itself. This is cheaper than MessagePack for every call.

#### What changes
- `binarybridge/marshaller.go`: Replace `msgpack.Marshal/Unmarshal` with a hand-written encoder that follows the Canonical ABI lifting/lowering spec for the types glassbox-go actually uses (`[]byte`, `string`, `int*`, `float*`, `map`, `error`).
- `generator/generator.go`: Update the guest and proxy templates to use the new encoder instead of `SerializeAsBytes`/`DeserializeFromBytes`.
- Remove the `vmihailenco/msgpack` dependency entirely.

#### What does NOT change
The host security layer (`SecurityGate`, `SandboxLimits`, `VirtualHTTPClient`), instance pooling, LRU cache, context propagation, and the `//gobox:sandbox` developer experience are all unaffected. This is a pure performance improvement to the transport layer.

#### Why this is low-risk
The Canonical ABI encoding for basic types is simple and stable — it's a published specification. It does not require wazero to have any Component Model support; it's just a different way to pack bytes.

---

### 6. Option B — Add Wasmtime as an Optional Runtime Backend
**Status:** Planned
**Motivation:** [Wasmtime](https://github.com/bytecodealliance/wasmtime-go) has first-class Go bindings and full Component Model support, including WIT-defined interfaces and multi-language guests. Users who need to sandbox code written in Rust, Python, or JavaScript alongside Go code — or who need true WIT interface contracts — should be able to do so through the same `gruntime.Engine` API.

#### Design
```go
// Current (wazero, default)
engine, _ := gruntime.NewEngine(ctx)

// Future (wasmtime, opt-in)
engine, _ := gruntime.NewWasmtimeEngine(ctx)
```

Both would implement a common `gruntime.RuntimeBackend` interface. The `Engine` struct becomes a thin wrapper that delegates to the configured backend. The security layer (`SandboxLimits`, `SecurityGate`) applies identically regardless of backend.

#### What this unlocks
- Multi-language guest support: a Rust plugin and a Go plugin can share the same security policy framework.
- True WIT interface contracts: users can optionally write `.wit` files instead of (or alongside) `//gobox:sandbox` annotations.
- The two backends can coexist in the same binary; users choose per-proxy.

#### What does NOT change
The `//gobox:sandbox` annotation, `go generate` workflow, and the generated proxy being a drop-in implementation of the user's Go interface — these remain identical regardless of which backend runs it.

---

### 7. Option C — GOOS=wasip2 / Standard Go Component Model Support
**Status:** Monitoring
**Motivation:** The Go standard compiler (`gc`) does not yet support `wasip2`. When it does, the Canonical ABI and WIT bindings will be available without TinyGo, making Options A and B redundant for new projects. The project is structured to absorb this change with minimal disruption.

#### What changes when wasip2 lands in standard Go
- `generate.go` changes from `GOOS=wasip1` to `GOOS=wasip2`.
- `binarybridge/wasm_mem.go` (the custom `malloc`/`free`/`allocations` map) is **deleted** — `cabi_realloc` handles this.
- `binarybridge/marshaller.go` is **deleted** — the Canonical ABI handles this.
- The generator's guest template no longer emits `malloc`/`free` exports or `KeepAliveAndPack`.
- The `uint64` `(ptr << 32) | len` packing trick is **deleted** — multi-value returns in the Canonical ABI handle this.

#### What does NOT change
Every layer above the transport:
- `SecurityGate` and `SandboxLimits` — unchanged.
- `VirtualHTTPClient` with DNS rebinding + CGNAT protection — unchanged.
- `//gobox:sandbox` annotation and `go generate` workflow — unchanged.
- The generated proxy implementing the user's Go interface — unchanged.
- `Engine` LRU cache, instance pooling, context timeout propagation — unchanged.

---

## 🏗️ Why Glassbox-Go Remains Relevant as the Component Model Matures

The Component Model is a **transport standard**. Glassbox-Go is a **developer experience and capability policy system**. These are different layers that complement rather than replace each other.

### The DX gap is fundamental, not cosmetic

The Component Model workflow for sandboxing Go code is **WIT-first**:
```
Your idea → write .wit file (new language) → wit-bindgen-go → implement generated stubs → compile with TinyGo → wire up host runtime
```

The Glassbox-Go workflow is **Go-first**:
```
Existing Go interface → add //gobox:sandbox → go generate ./... → done
```

Three things the Component Model cannot replicate:

**1. The proxy IS your interface.** `YAMLParserWasmProxy` implements `YAMLParser` exactly. You inject it anywhere a `YAMLParser` is accepted. No adapter layer, no type conversion, no new type hierarchy to learn. WIT-generated bindings implement their own generated types, which you then map to your types yourself.

**2. `context.Context` is native.** Glassbox-Go understands that `ctx context.Context` is special: it's excluded from serialization, drives timeouts, and carries capability limits from host to guest call. The WIT spec has no concept of context; wit-bindgen-go generated functions have no `ctx` parameter. You layer that in manually or not at all.

**3. Standard Go toolchain, no TinyGo.** Every guest compiles with `go build GOOS=wasip1`. TinyGo (required for wasip2 today) has significant standard library limitations — no `reflect`, limited `net`, many packages simply absent. The moment a user's existing code touches anything TinyGo doesn't support, the experience breaks.

### What Glassbox-Go adds that the Component Model spec never will

- **Capability policy management**: The Component Model defines how to call `wasi:http/outgoing-handler`. Glassbox-Go defines which resolved IP addresses that call is allowed to reach, including CGNAT ranges and link-local.
- **Symlink-safe path resolution**: WASI filesystem defines how to call `open`. Glassbox-Go defines which real paths (after symlink resolution) are within the allowed directory list.
- **Per-call limits via context**: `WithActiveLimits(ctx, limits)` threads capability decisions through the normal Go call chain. Nothing in any Wasm standard addresses this.
- **Go-idiomatic error propagation**: `error` as a return type flows correctly through the host/guest boundary with no WIT `result<T, E>` type mapping required.

The Component Model maturation is an opportunity to replace the transport layer (binarybridge) with a cleaner implementation, not a reason to retire the product.

