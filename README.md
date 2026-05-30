# Glassbox-Go: Transparent WebAssembly Sandboxing for Go

**Glassbox-Go** is a framework that makes WebAssembly sandboxing invisible to Go developers. Define an interface, annotate it, and get a secure, JIT-compiled Wasm proxy automatically.

## 🚀 The Vision
Developers write standard Go code. The Wasm boundary, marshaling, and runtime management are completely automated. 

## 🛡️ Security First
- **Memory Isolation**: Guest code runs in a strict linear memory sandbox.
- **Capability-Based Security**: Explicit whitelisting for filesystem and network access.
- **Resource Limits**: Built-in support for timeouts and memory page limits.

## 📦 Supported Libraries
Glassbox-Go works with **pure Go libraries** that can be compiled to `GOOS=wasip1 GOARCH=wasm`.
- ✅ **Works**: Pure Go logic, standard library (mostly), `goldmark`, `yaml.v3`, etc.
- ❌ **Doesn't Work**: CGO, raw sockets, `os/exec`, non-standard OS features.

## 🛠️ Quickstart

### 1. Define and Implement your Interface
```go
//gobox:sandbox
type MarkdownParser interface {
    Render(ctx context.Context, markdown []byte) (string, error)
}

type MarkdownParserImpl struct{}
func (m *MarkdownParserImpl) Render(ctx context.Context, data []byte) (string, error) {
    // ... implementation ...
}
```

### 2. Generate the Wasm Seam
```bash
make gen
```
This runs `gobox-gen`, which produces:
- `markdownparser_proxy.go`: The host-side proxy.
- `guest/main.go`: The Wasm-side shim that delegates to your implementation.

### 3. Compile to WebAssembly
```bash
make build-wasm
```
Compiles your code into a single `wasm/pkg.wasm` binary using standard Go.

### 4. Use the Sandbox
```go
ctx := context.Background()
engine, _ := gruntime.NewEngine(ctx)
proxy, _ := demo.NewMarkdownParserWasmProxy(engine, nil)

html, err := proxy.Render(ctx, []byte("# Hello Sandbox"))
```

## 🏗️ Architecture

Interface → `gobox-gen` → [ Proxy (Host) + Guest Shim (Wasm) ] → `go build` → `.wasm` binary

When you call a proxy method:
1. Arguments are MessagePack serialized.
2. The Wasm `Engine` (powered by **wazero**) acquires a module instance from a pool.
3. The data is written to guest memory.
4. The guest shim executes your real implementation.
5. Results are serialized back and returned to the host.

## 📊 Performance
Powered by wazero's JIT compiler, Glassbox-Go achieves near-native execution speeds after the initial warm-up, with MessagePack serialization being the primary overhead for large payloads.
