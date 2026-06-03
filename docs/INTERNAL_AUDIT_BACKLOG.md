# Glassbox Internal Audit Backlog
This file tracks architectural, structural, and DX issues found during continuous holistic system audits.

🔴 [Severity: CRITICAL]: The Context/Capability Boundary Paradox
Dimension: DX / Safety / Architecture
Target Area: `generator/generator.go`, `api/security_gate.go`, `examples/pdf/pdf_processor.go`
Finding Description: The system conceptually relies on `context.Context` to carry capability boundaries (`gapi.SandboxLimits`) through the execution graph. The Host-side proxy correctly attaches these limits and validates them. However, inside the Wasm guest, the generator injects `context.Background()` when calling the user's implementation. The `SandboxLimits` never cross the boundary. As a result, if a user attempts to call `gapi.SecurityGate{}.CheckFileAccess(ctx, path)` or anything relying on Context inside their guest logic, `GetActiveLimits(ctx)` will return `nil` and the operation will unconditionally fail due to the default-deny rule.
Developer Impact / Risk: The examples tell users to run `gate.CheckFileAccess(ctx)` inside Wasm, but doing so completely breaks their code because the capabilities don't exist there. The architecture promises per-call capability boundaries, but they are physically isolated to the host.
Recommendation: Remove `gate.CheckFileAccess` from the guest completely. Document that `SecurityGate` is a HOST-only construct. Add explicit tooling in the generator to inject host-side evaluations before invoking the Wasm module, which is already done for `gapi.SandboxPath`. Update the `README.md` and examples to clarify that the Wasm guest inherits limits via WASI, not via `context.Context` limits map.

🔴 [Severity: HIGH]: Loss of Error Granularity on Host-Function Calls (`fetch_http`)
Dimension: DX / Maintainability
Target Area: `runtime/engine.go:registerHostFunctions`, `api/http_client_wasm.go`
Finding Description: The host function `fetch_http` is exposed to the Wasm guest to perform egress network calls. If the host proxy (`VirtualHTTPClient`) rejects the call due to security constraints, or if a network error occurs, the host function simply returns a magic sentinel value `0xFFFFFFFFFFFFFFFF`. The guest wrapper `fetchHTTP` checks this and unconditionally returns `errors.New("HTTP fetch failed: host rejected or network error")`.
Developer Impact / Risk: Developers cannot diagnose why their HTTP call failed. Was it a DNS error? Did it violate the egress firewall? Did it timeout? They receive an opaque message, severely degrading the debugging experience.
Recommendation: The host function `fetch_http` should be refactored to pack the error string into Wasm memory and return a structured type `(resultPtr, errPtr)` or write the error to a predefined pointer, allowing the guest to reconstruct the exact `error` value and return it to the developer.

🔴 [Severity: MEDIUM]: Uncapped `malloc` Request Size in `binarybridge/wasm_mem.go`
Dimension: Safety
Target Area: `binarybridge/wasm_mem.go`
Finding Description: The exported `malloc(size uint32)` function inside the Wasm guest blindly allocates a slice `make([]byte, size)` without any sanity checks on `size`. While Wazero bounds the overall linear memory of the instance (if `MaxMemoryPages` is configured), an extremely large allocation request from the host proxy or maliciously forged inputs could cause a panic within the Wasm guest's runtime allocator before Wazero's limit is cleanly enforced.
Developer Impact / Risk: Potential guest runtime panic, leading to abrupt sandbox termination rather than a clean capability-denial error.
Recommendation: Introduce a maximum allowed `malloc` size aligned with the configured `MaxMemoryPages` or a hardcoded sane upper limit (e.g., 50MB) and return a distinct failure/null pointer if exceeded, allowing graceful error handling.

🔴 [Severity: DX-FRICTION]: Missing Configuration Validation in `SandboxLimitsBuilder`
Dimension: Configuration
Target Area: `api/limits.go`
Finding Description: The `SandboxLimitsBuilder` allows configuration of paths and network addresses via `AllowFileSystemAccess` and `AllowNetworkAddresses`. However, it does not validate these inputs (e.g., checking if a path is absolute, or if a network address is a valid CIDR/Host-Port format) at build time. Validation is deferred until runtime when the actual check is performed in `SecurityGate`.
Developer Impact / Risk: A developer might misconfigure a path (e.g., passing a relative path) and the builder will silently accept it. The error will only surface at runtime when a function is executed, leading to brittle deployments.
Recommendation: Move the eager validation logic (like `filepath.IsAbs`) into the `SandboxLimitsBuilder` methods (`AllowFileSystemAccess`, `AllowNetworkAddresses`) so the system fails fast at initialization.

🔴 [Severity: MEDIUM]: Instance Pool State Leakage via Explicit Return Values
Dimension: Safety / Configuration
Target Area: `runtime/engine.go:ReleaseInstance`
Finding Description: The system provides a `PoolInstances(true)` feature that keeps a `wazero.Module` alive and reuses it across invocations to save instantiation overhead. While `ReleaseInstance` correctly discards the module if the context was cancelled or if an error occurred during Wasm execution (`success == false`), it DOES NOT wipe linear memory. The `README.md` acknowledges this risk ("linear memory is not wiped between calls"). However, this risk is severe: global variables in the guest implementation will persist across sandboxed invocations.
Developer Impact / Risk: If a user writes a plugin that caches a user ID in a global variable, `PoolInstances(true)` will cause subsequent invocations to read the cached global state of the *previous* request, resulting in a critical cross-request data leak.
Recommendation: The documentation should be upgraded to a blaring red warning indicating that global state is preserved. A better structural fix would be to deprecate `PoolInstances(true)` entirely, or automatically re-invoke `_initialize` on the module (if Wazero supports resetting memory bounds) prior to reusing it.

🔴 [Severity: HIGH]: SecurityGate Network Port Parsing Brittleness
Dimension: Safety / Maintainability
Target Area: `api/security_gate.go:CheckNetworkAccess`
Finding Description: The `CheckNetworkAccess` method validates egress requests against the `AllowedNetworkAddresses` whitelist. The logic checks `targetNorm == allowedNorm || strings.HasPrefix(targetNorm, allowedNorm+":")`. This simple prefix check is extremely brittle and vulnerable to bypasses. For example, if the whitelist allows `api.example.com`, the prefix check `api.example.com:443` passes. But if the input is maliciously crafted as `api.example.com.malicious.com`, `strings.HasPrefix` logic might be tricked depending on how the input was formatted or passed (although `+ ":"` mitigates some of this, `targetNorm` might not contain a port).
Developer Impact / Risk: A malicious plugin could potentially bypass egress filtering if the string matching rules do not strictly enforce hostname and port boundaries.
Recommendation: Replace rudimentary `strings.HasPrefix` logic with robust standard library parsing (e.g., `net.SplitHostPort` and strict exact matches).

🔴 [Severity: DX-FRICTION]: Missing Context Cancellation in `VirtualHTTPClient.Fetch` Response Read
Dimension: DX / Safety
Target Area: `api/http_client.go`
Finding Description: The `VirtualHTTPClient.Fetch` method correctly uses `http.NewRequestWithContext(ctx, ...)` and limits the response size using `http.MaxBytesReader`. However, reading the response body via `io.ReadAll(resp.Body)` is not strictly preemptable if the underlying transport is stalled and the context is cancelled during the read phase, though `net/http` typically handles this. More importantly, the HTTP timeout is hardcoded to 15 seconds in the Transport, overriding the caller's context if the context is longer, but not short-circuiting efficiently if the reader blocks.
Developer Impact / Risk: Potential goroutine leaks or stalling during large file downloads in the host proxy if the context timeouts are not robustly respected throughout the entire I/O lifecycle.
Recommendation: Ensure the reader explicitly selects on `ctx.Done()` or verify that `http.MaxBytesReader` is fully responsive to context cancellation in this configuration.

🔴 [Severity: LOW]: Silent Failure on Host Proxy Registration
Dimension: Maintainability / Futureproofness
Target Area: `runtime/engine.go:registerHostFunctions`
Finding Description: In `registerHostFunctions`, the host function `fetch_http` is registered. However, if multiple `wazero.Runtime` instances try to register the same host module name concurrently (e.g., in a heavily concurrent environment not strictly covered by the engine's mutex), or if another plugin uses the name `gobox_host`, it could panic or fail.
Developer Impact / Risk: Naming collisions for host modules could break multi-tenant environments.
Recommendation: Namespace the host module registration dynamically (e.g., `gobox_host_<engine_id>`) or ensure strict singleton registration guarantees.

🔴 [Severity: LOW]: Inefficient MessagePack Array Deserialization in Proxy
Dimension: Maintainability / Code Health
Target Area: `generator/generator.go:proxyTemplateText`
Finding Description: The code generator uses an inline anonymous struct to deserialize the results returned from the Wasm guest: `var outResults struct { _msgpack struct{} ... }`. This forces the reflection-based `msgpack.Unmarshal` to do extra work inspecting the anonymous struct definition on every single function call.
Developer Impact / Risk: Unnecessary CPU overhead and garbage collection pressure due to reflection on anonymous types during every boundary crossing.
Recommendation: Generate a named struct type for the results (e.g., `type MethodNameResults struct { ... }`) at the package level within the `_proxy.go` file and reuse it, or transition to the Canonical ABI as proposed in the roadmap.

🔴 [Severity: MEDIUM]: Unprotected LRU Cache Slices in `runtime/engine.go`
Dimension: Safety / Concurrency
Target Area: `runtime/engine.go:GetInstance`
Finding Description: The LRU eviction logic modifies `e.lruOrder` using slice appending: `e.lruOrder = append(e.lruOrder[:i], e.lruOrder[i+1:]...)`. While this is done under `e.cacheMutex.Lock()`, removing elements from the middle of a slice causes memory shifting, which is inefficient. More importantly, if `len(e.lruOrder) >= 10`, it evicts `e.lruOrder[0]`. The slice manipulation is an O(N) operation inside a lock that guards the entire engine instantiation path.
Developer Impact / Risk: High contention on `e.cacheMutex` during heavy concurrent sandbox instantiations, leading to latency spikes.
Recommendation: Replace the slice-based LRU tracking with a doubly-linked list (`container/list`) combined with a map for O(1) eviction and updates, reducing time spent holding the global cache lock.

🔴 [Severity: DX-FRICTION]: Missing Context Cancellation Propagation to WaitGroups
Dimension: DX / Architecture
Target Area: `runtime/engine.go:compileGrp` (singleflight)
Finding Description: The engine uses `e.compileGrp.Do(compileKey, ...)` to deduplicate JIT compilation. `singleflight.Group.Do` does not take a `context.Context`. If multiple requests are waiting on the compilation of a massive Wasm module and their contexts timeout, they cannot abort waiting. They must block until the single compilation finishes or fails.
Developer Impact / Risk: A cancelled HTTP request on the host might leave a goroutine permanently blocked waiting for `singleflight` to finish compiling a Wasm module, consuming resources needlessly.
Recommendation: Upgrade to `singleflight.Group.DoChan` and use a `select` statement with `ctx.Done()` to allow immediate return upon context cancellation.

🔴 [Severity: DX-FRICTION]: Misleading Wasm Toolchain Instructions in README
Dimension: Documentation
Target Area: `README.md`
Finding Description: The `README.md` and standard usage imply `go generate ./...` is sufficient. However, it relies on having a specific Go version (1.21+) that supports `GOOS=wasip1`. Furthermore, if the user doesn't have `github.com/glassbox-go/generator` installed or reachable, `go run` fails silently or confusingly.
Developer Impact / Risk: New developers might encounter confusing "unknown GOOS" or module resolution errors if their environment isn't strictly prepared.
Recommendation: Update the README to explicitly document the `go 1.21+` requirement for `wasip1` and provide a troubleshooting section for `go generate` failures.

🔴 [Severity: DX-FRICTION]: Missing Method Validation for Pointer Receivers in Code Generator
Dimension: DX / Tooling
Target Area: `generator/generator.go`
Finding Description: The code generator searches for a struct implementing the user's interface (e.g., `PDFProcessorImpl`). It emits a global variable `var pdfprocessorImpl = &Package.PDFProcessorImpl{}`. If the user accidentally implemented the interface methods on value receivers instead of pointer receivers, or missed a method entirely, the guest Wasm compilation step (`make build-wasm`) will fail with a confusing Go compiler error deep inside the generated code.
Developer Impact / Risk: The developer receives a compile-time error from `go build` rather than a helpful, descriptive error from `gobox-gen` during the `go generate` phase.
Recommendation: The generator AST walker should verify that the target implementation struct actually implements the full interface before emitting code, and output a friendly warning if methods are missing.

🔴 [Severity: LOW]: Silent Overflow Risk in Wasm Memory Conversion
Dimension: Safety
Target Area: `binarybridge/wasm_mem.go:KeepAliveAndPack`
Finding Description: The `KeepAliveAndPack` function bitwise-shifts the pointer to the upper 32 bits and uses the lower 32 bits for the length: `(uint64(uint32(uintptr(unsafe.Pointer(ptr)))) << 32) | uint64(len(buf))`. This assumes the pointer can fit in 32 bits, which is true for `wasip1` (Wasm32). However, it does not check if `len(buf)` exceeds 32 bits. While unlikely for normal payloads, if `len(buf)` somehow exceeds `math.MaxUint32`, the length will be silently truncated.
Developer Impact / Risk: Data corruption or segmentation faults when reading the return payload on the host side.
Recommendation: Add a panic or explicit error check: `if uint64(len(buf)) > 0xFFFFFFFF { panic("payload too large") }`.

🔴 [Severity: HIGH]: `VirtualHTTPClient` Does Not Enforce Scheme Whitelist on Initial Request
Dimension: Safety / Configuration
Target Area: `api/http_client.go:VirtualHTTPClient.Fetch`
Finding Description: The redirect policy correctly checks for `http/https` only. However, the initial URL parsed in `Fetch` does not check the scheme before passing it to `http.NewRequestWithContext`. The standard `net/http` library will reject unknown schemes, but `file://` might behave unexpectedly depending on the environment, or bypass the intent of the sandbox.
Developer Impact / Risk: Potential SSRF or local file read vulnerabilities if the sandbox allows `file://` or other unexpected protocol handlers via standard Go HTTP clients.
Recommendation: Add an explicit scheme whitelist check (`u.Scheme == "http" || u.Scheme == "https"`) at the very beginning of the `Fetch` method before any processing.

🔴 [Severity: DX-FRICTION]: Unclear Testing Story for Guest Code
Dimension: DX / Documentation
Target Area: `README.md`, `demo/sandbox_test.go`
Finding Description: The current documentation explains how to run the host-side proxy and benchmark it. It does not explain how developers should write unit tests for their `Impl` structs. Since `gobox` creates a hard boundary, developers might think they need to compile Wasm to test their logic.
Developer Impact / Risk: Developers might skip unit testing their inner guest logic because they don't know they can simply instantiate their `Impl` struct directly in normal Go tests.
Recommendation: Add a section to the `README.md` explicitly demonstrating that `Impl` structs can and should be unit-tested using standard `go test` before being compiled into the sandbox.

🔴 [Severity: LOW]: Missing Explicit Timeout in `compileGrp.Do` Wasm Loading
Dimension: Safety / Resilience
Target Area: `runtime/engine.go:GetInstance`
Finding Description: The JIT compilation loads bytes via `loadWasmBytes(moduleName, limits)`, which uses `os.ReadFile`. This disk I/O operation is performed inside the singleflight block without any context cancellation or timeout wrappers. If the filesystem is mounted on a slow network drive or is stalling, it blocks the engine instantiation indefinitely.
Developer Impact / Risk: The host system could experience permanent goroutine hangs on sluggish file systems.
Recommendation: Plumb the `context.Context` through `loadWasmBytes` and use an I/O approach that respects deadlines, or load the bytes outside the singleflight compilation lock.

🔴 [Severity: MEDIUM]: Uncaught Panics in Wasm Instance Closure
Dimension: Maintainability / Code Health
Target Area: `runtime/engine.go:Close`, `ReleaseInstance`
Finding Description: Throughout the codebase, calls to `mod.Close(ctx)` and `compiled.Close(ctx)` are made. If a wazero module or compiled module encounters a corrupted state, `Close` could theoretically panic (though Wazero is generally stable). More practically, `ReleaseInstance` calls `mod.Close(context.Background())` concurrently. There's no recovery mechanism if `Close` fails or panics.
Developer Impact / Risk: A panic during cleanup could crash the entire host process.
Recommendation: Wrap critical cleanup routines in `defer func() { recover() }()` blocks where appropriate, or ensure Wazero's closure mechanisms are guaranteed panic-free under all conditions.

🔴 [Severity: DX-FRICTION]: Missing Wasm Size Warning in Code Generator
Dimension: Tooling / DX
Target Area: `generator/generator.go:compileWasm`
Finding Description: The generator compiles the Wasm binary automatically. However, standard Go `wasip1` binaries are often 2MB-10MB in size due to the inclusion of the garbage collector and runtime. Developers are completely blind to the size of the generated binary until runtime or deployment.
Developer Impact / Risk: Developers might accidentally pull in heavy dependencies (e.g., `net/http`) causing massive Wasm binary inflation, slowing down instantiation drastically.
Recommendation: Have `compileWasm` output the file size of the generated `.wasm` artifact to the console, and emit a warning if it exceeds a certain threshold (e.g., 5MB), guiding the developer to optimize imports.

🔴 [Severity: DX-FRICTION]: Unclear Error Mapping on Wasm Panics
Dimension: Architecture / DX
Target Area: `generator/generator.go:guestTemplateText`
Finding Description: Inside the guest template, panics are caught: `if r := recover(); r != nil { errOut = fmt.Sprintf("panic in wasm guest: %v", r) }`. However, this error string is then returned across the boundary as a standard string. When unpacked on the host side, it becomes a generic `fmt.Errorf`. The host loses the stack trace context from inside the Wasm execution.
Developer Impact / Risk: Debugging a panic inside the sandbox is extremely difficult because the developer only gets "panic in wasm guest: runtime error: index out of range", without knowing *where* it happened.
Recommendation: Capture the stack trace using `debug.Stack()` inside the guest's recovery block and append it to the error output string, or log it directly to the host's `SandboxLogger` before returning the error.

🔴 [Severity: LOW]: Inconsistent Error Handling for `SandboxLogger` Writes
Dimension: Code Health
Target Area: `runtime/engine.go:safeLogWriter`
Finding Description: `safeLogWriter.Write` implements `io.Writer` to capture Wasm standard output. It ignores errors from the user-provided `SandboxLogger` (which doesn't return errors anyway) but always returns `len(p), nil`. If the data stream contains invalid UTF-8 bytes, it blindly converts them to strings `string(p)`.
Developer Impact / Risk: Log corruption if binary data is accidentally written to stdout/stderr in the guest.
Recommendation: Add a check to sanitize or escape non-printable characters before passing the message to the logger.

🔴 [Severity: LOW]: Missing Explicit Cache Invalidation API for Wasm Updates
Dimension: Futureproofness / DX
Target Area: `runtime/engine.go`
Finding Description: The Wasm modules are loaded from disk via `loadWasmBytes` and compiled into the `moduleCache` or `limitedCache`. There is an `e.ClearCache()` function, but no targeted invalidation (e.g., `e.ReloadModule("demo")`). If a Wasm binary is updated on disk while the host is running, the engine continues to use the stale JIT-compiled version indefinitely.
Developer Impact / Risk: Hot-reloading of plugins during development requires a full server restart, which degrades the developer experience.
Recommendation: Implement an `e.InvalidateModule(moduleName string)` method that removes specific modules from the caches, allowing seamless live-reloading of sandboxed code.
