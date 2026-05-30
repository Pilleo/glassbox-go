# Glassbox-Go

**Glassbox-Go** makes WebAssembly sandboxing invisible to Go developers. Define a Go interface, add one annotation, run `go generate` — and get a sandboxed Wasm proxy that implements your interface exactly.

```go
//gobox:sandbox
type YAMLParser interface {
    Parse(ctx context.Context, data []byte) (map[string]interface{}, error)
}

// Use it like any other Go value:
proxy, _ := NewYAMLParserWasmProxy(engine, limits)
result, err := proxy.Parse(ctx, untrustedYAML) // runs inside Wasm sandbox
```

> [!WARNING]
> **Default-Deny Security**: By default, sandboxed code has **zero** access to the host filesystem and network. Explicitly whitelisting HTTP egress or directory mounts introduces real risk. Grant the minimum required capabilities only.

> [!CAUTION]
> **Memory Limits**: Without `MaxMemoryPages()`, wazero allows up to 4GB of linear memory per sandbox. Always set this in production to prevent memory exhaustion. If you enable `PoolInstances(true)`, linear memory is **not** wiped between calls — residual state can leak across invocations.

---

## How it works

```
Go interface + //gobox:sandbox
        │
        ▼
   go generate (gobox-gen)
        │
   ┌────┴────────────────────┐
   │                         │
   ▼                         ▼
Host proxy              Guest shim
(implements your       (compiled to
 interface)              .wasm binary)
        │
        ▼
 wazero JIT runtime
```

When you call a proxy method:
1. Arguments are MessagePack serialized.
2. The `Engine` acquires a compiled module instance (from a pool or fresh instantiation).
3. Data is written to guest linear memory via the shared-memory boundary.
4. The guest shim executes your real implementation inside the Wasm sandbox.
5. Results are serialized back and returned to the host.

---

## Quick start

### 1. Annotate your interface

```go
package parsing

import "context"

//gobox:sandbox
type YAMLParser interface {
    Parse(ctx context.Context, data []byte) (map[string]interface{}, error)
}

type YAMLParserImpl struct{}

func (y *YAMLParserImpl) Parse(ctx context.Context, data []byte) (map[string]interface{}, error) {
    // your real implementation
}
```

Every sandboxed method **must** return `error` as its last return value.

### 2. Add code generation

```go
// generate.go (or any file in your package)
//go:generate go run github.com/glassbox-go/generator -dir . -build
```

### 3. Run the generator

```sh
go generate ./...
```

This produces:
- `yamlparser_proxy.go` — host-side proxy implementing `YAMLParser`
- `guest/main.go` — guest shim compiled to Wasm
- `generate.go` — records the `go generate` command for reproducibility
- `wasm/yourpackage.wasm` — the compiled Wasm binary (if `-build` is passed)

### 4. Use the proxy

```go
import (
    gapi    "github.com/glassbox-go/api"
    gruntime "github.com/glassbox-go/runtime"
)

engine, _ := gruntime.NewEngine(ctx)
defer engine.Close(ctx)

limits := gapi.NewBuilder().
    MaxMemoryPages(50).       // 50 × 64KB = 3.2MB hard memory cap
    Timeout(2 * time.Second). // preempt execution after 2s
    Build()

proxy, err := parsing.NewYAMLParserWasmProxy(engine, limits)
if err != nil {
    log.Fatal(err)
}

result, err := proxy.Parse(ctx, untrustedInput)
```

---

## Annotations

| Annotation | Where | Effect |
|---|---|---|
| `//gobox:sandbox` | Above an interface type | Marks the interface for proxy + guest generation |
| `//gobox:impl MyStruct` | Same comment block as `//gobox:sandbox` | Use `MyStruct` as the guest implementation instead of `InterfaceNameImpl` |

### Path safety

Parameters of type `gapi.SandboxPath` receive automatic host-side path validation before the Wasm boundary is crossed:

```go
//gobox:sandbox
type FileProcessor interface {
    ReadFile(ctx context.Context, path gapi.SandboxPath) (string, error)
}
```

The generator also emits a compile-time warning if a `string` parameter is named `path`, `file`, or `dir` — suggesting you use `SandboxPath` instead.

---

## Sandbox configuration

```go
limits := gapi.NewBuilder().
    MaxMemoryPages(50).                           // memory cap (pages × 64KB)
    Timeout(2 * time.Second).                     // execution timeout
    AllowFileSystemAccess("/data/inputs").         // whitelist a directory
    AllowNetworkAddresses("api.example.com:443"). // whitelist an egress endpoint
    WasmPath("./wasm").                           // custom wasm binary directory
    PoolInstances(true).                          // reuse instances (performance ↑, isolation ↓)
    Logger(func(lvl gapi.LogLevel, msg string) { // receive guest stdout/stderr
        log.Printf("[sandbox][%s] %s", lvl, msg)
    }).
    Build()
```

### Permissive mode (development only)

```go
limits := gapi.NewBuilder().PermissiveMode().Build()
```

Disables all security checks. **Never use in production.**

---

## Security features

### Network egress firewall

`VirtualHTTPClient` (used by guest HTTP calls via `gapi.NewVirtualHTTPClient()`) enforces:
- Scheme whitelist — only `http` and `https` allowed.
- Outbound address whitelist — checked against `AllowNetworkAddresses`.
- **DNS rebinding protection** — the resolved IP is validated, not just the hostname. Connecting to `127.0.0.1` via a DNS rebind is blocked even if the hostname is whitelisted.
- **Carrier-Grade NAT blocking** — `100.64.0.0/10` (Tailscale, Kubernetes pod CIDRs) is treated as internal and blocked.
- Response body size cap — capped at 50% of `MaxMemoryPages` to prevent guest OOM.

### Filesystem access control

`SecurityGate.CheckFileAccess` (used automatically for `gapi.SandboxPath` parameters):
- Resolves symlinks before comparison — a symlink inside an allowed directory that points outside is **blocked**.
- Requires absolute paths.
- Strict prefix matching after resolution.

### Context-scoped limits

Capabilities flow through `context.Context`:

```go
ctx = gapi.WithActiveLimits(ctx, limits)
// now any host function call checks limits from ctx
```

This means limits are per-call, not per-engine. Different callers can share one engine with different policies.

---

## What is and isn't supported in the sandbox

✅ **Supported:**
- Pure Go code (no CGO)
- Filesystem access (sandboxed via WASI virtual mount)
- Outbound HTTP (proxied to host via `gapi.NewVirtualHTTPClient()`)
- Goroutines (scheduled differently inside Wasm, but functional)
- Generic types in interface signatures

❌ **Not supported:**
- CGO-dependent libraries (SQLite, libvips, FFmpeg, etc.)
- Raw network sockets (`net.Listen`, `pgx`, `mysql`, etc.)
- OS process spawning (`os/exec`)
- Inbound network servers

⚠️ **Pass-by-value boundary:**
Arguments cross the host/guest boundary via MessagePack serialization. **Pointers and slices are passed by value** — modifications inside the sandbox are not reflected on the host. Return modified state explicitly.

---

## Package structure

```
glassbox-go/
├── api/                    Public API surface
│   ├── limits.go           SandboxLimits builder — configure memory, timeouts, capabilities
│   ├── security_gate.go    SecurityGate — filesystem and network access checks
│   └── http_client.go      VirtualHTTPClient — capability-scoped outbound HTTP
│
├── binarybridge/           Host↔guest data transport
│   ├── marshaller.go       MessagePack serialization helpers + UnmarshalError
│   └── wasm_mem.go         Guest-side malloc/free/KeepAliveAndPack (wasip1 only)
│
├── runtime/                Wasm runtime management
│   └── engine.go           Engine — JIT compilation, LRU cache, instance pooling
│
├── generator/              Code generator (standalone binary)
│   └── generator.go        AST walker → host proxy + guest shim templates
│
├── demo/                   Reference implementation
│   └── yaml_parser.go      YAMLParser sandboxed interface
│
├── examples/
│   ├── markdown/           MarkdownParser — render Markdown to HTML in a sandbox
│   └── pdf/                PDFProcessor — read files via SandboxPath
│
├── scripts/
│   └── gobox-build.sh      Full build automation script
│
├── Makefile                Common development commands
└── future_improvements.md  Roadmap and architectural notes
```

---

## Development commands

```sh
# Full build: tidy → generate proxies → compile wasm → run tests
make build

# Run all tests with coverage
make test

# Run code generator for all examples (no wasm compilation)
make gen

# Compile all guest packages to .wasm binaries
make build-wasm

# Remove all generated files (proxies, guest dirs, wasm binaries)
make clean
```

---

## Performance

The benchmark below measures the overhead of the **host-side proxy boundary only**. Actual guest execution inside wazero is typically 2–5× slower than native Go for compute-heavy workloads (wazero JIT-compiles the `.wasm` bytecode at first use and caches the result, but the Wasm execution model has intrinsic overhead).

```
BenchmarkNativeYAMLParse-12       159,844     7,352 ns/op
BenchmarkGlassboxedYAMLParse-12   153,406     7,798 ns/op
```

The 6% overhead comes from MessagePack serialization and memory allocation, not from wazero's JIT execution of Go code.

---

## Examples

### Markdown renderer (pure compute)

```go
// examples/markdown/markdown_parser.go
//gobox:sandbox
type MarkdownParser interface {
    Render(ctx context.Context, markdown []byte) (string, error)
}
```

### File processor (filesystem capability)

```go
// examples/pdf/pdf_processor.go
//gobox:sandbox
type PDFProcessor interface {
    ExtractTextFromFile(ctx context.Context, path gapi.SandboxPath) (string, error)
}

// Usage:
limits := gapi.NewBuilder().
    AllowFileSystemAccess("/var/uploads/pdfs").
    Build()
proxy, _ := pdf.NewPDFProcessorWasmProxy(engine, limits)
text, err := proxy.ExtractTextFromFile(ctx, "/var/uploads/pdfs/document.pdf")
```

---

## Roadmap

See [`future_improvements.md`](future_improvements.md) for the full roadmap, including:
- **Fuel limiter** — precise per-instruction CPU quotas
- **Wasmtime backend** — opt-in Component Model support and multi-language guests
- **Canonical ABI transport** — replace MessagePack with the Component Model's own encoding
- **GOOS=wasip2** — migration path when standard Go supports the Component Model
