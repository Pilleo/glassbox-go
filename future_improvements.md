# Future Improvements: Glassbox-Go

This document details the roadmap for advanced capabilities and architectural refinements planned for future phases of the **Glassbox-Go** framework.

---

## 🚀 Future Roadmap

### 1. Instruction-Level Fuel Limiter (Gas Instrumentation)

#### The Vision
While cooperative context timeouts (currently implemented) halt runaway loops, a malicious plugin can still consume 100% CPU for the duration of that timeout. We want a **precise instruction quota** that preempts execution after exactly $N$ opcodes.

#### The Challenge
Wazero's JIT does not expose an instruction counter by default. To implement this without a 10x performance hit (like an interpreter would have), we need to:
1.  **Instrument the Wasm Binary**: Pre-process the Wasm bytecode before JIT compilation to inject "decrement fuel" opcodes at the start of every basic block.
2.  **Host-side Trap**: When the counter reaches zero, the guest calls a host function that triggers a panic or trap, safely halting the JIT execution.

### 2. Multi-Module Sandbox Orchestration (Micro-Plugin Mesh)

#### The Vision
Enable multiple isolated plugins to communicate directly within the **same Wazero Namespace** without bouncing back to the Go host. This allows for complex pipelines (e.g., Auth -> Transform -> Log) with near-zero latency between steps.

#### Implementation Blueprint
1.  **Shared Namespace**: Proxies for different interfaces should optionally share a single `wazero.Namespace`.
2.  **Export Linking**: Modify the runtime to resolve imports of Module B from the exports of Module A during instantiation.

### 3. Zero-Copy Shared Memory (Large Data Optimization)

#### The Vision
Currently, data is copied into the Wasm linear memory via `mod.Memory().Write`. For multi-megabyte payloads (e.g., large PDF processing or video frame analysis), this copy is a bottleneck.

#### Implementation Blueprint
- Utilize **Wasm Shared Memory** and **Atomics** (once stable in Wazero).
- Explore **Memory Mapping** techniques to allow the guest to read directly from a host-provided buffer via a restricted memory view.

### 4. Generator Enhancements

#### The Vision
Expand the `gobox-gen` tool to handle more complex Go types and interface patterns.
- **Embedded Interfaces**: Support interfaces that embed other interfaces.
- **Type Aliases**: Correct handling of user-defined type aliases.
- **Auto-Embedding**: Optional automatic generation of `//go:embed` directives to bundle Wasm binaries directly into the Go application binary.

### 5. Disk-Based Compilation Cache (Cold Start Optimization)

#### The Vision
Currently, the JIT-compiled module lives only in process memory — every cold start (new process, restart, deploy) re-compiles the Wasm binary from scratch. For large plugins this can take hundreds of milliseconds. Wazero already exposes a file-system compilation cache that serializes native JIT artifacts to disk and reuses them across restarts.

#### Implementation Blueprint
```go
cache, err := wazero.NewCompilationCacheWithDir("/var/cache/glassbox")
if err != nil {
    return err
}
rConfig := wazero.NewRuntimeConfigCompiler().
    WithCompilationCache(cache)
```
The cache is keyed on the Wasm binary hash, so stale entries are never replayed after an update. Exposes as an optional `WithCompilationCache(dir string)` method on `SandboxLimitsBuilder` or the future `Runtime` type.

### 6. WASI Preview 2 / WebAssembly Component Model (Strategic North Star)

#### The Vision
The current ABI is hand-rolled: arguments are msgpack-serialized into a single byte buffer, passed as a packed `uint64` (pointer + length), and the return is read back the same way. This works but it is Go-specific, brittle to extend, and not interoperable with plugins written in Rust, C, or Swift.

The **WebAssembly Component Model** (WASI Preview 2) is the standards-track answer: plugins and hosts agree on a typed `.wit` (Wasm Interface Type) file, and toolchains auto-generate the marshaling in any language. The interface boundary becomes language-agnostic.

#### What This Enables
- Plugins written in **Rust, C, Swift, or Kotlin** callable from the same Go proxy
- No hand-written ABI — the `.wit` file is the contract
- Automatic msgpack/custom serialization replaced by canonical Component Model encoding

#### Current State
Wazero has experimental WASI Preview 2 support as of v1.7. Full Component Model (`wasmtime`-level) is not yet stable in wazero. This is a 1–2 year horizon item but the `//gobox:sandbox` annotation on an interface maps naturally onto a `.wit` interface definition — the migration path is clear.
