# Canonical ABI Transition Exploration

This document outlines the scope, technical requirements, and potential path forward for transitioning Glassbox-Go's `binarybridge` serialization from `MessagePack` to the `Canonical ABI` encoding standard.

## 1. Background & Motivation
Currently, data crosses the WebAssembly host/guest boundary via `MessagePack` serialization. While robust, MessagePack is a general-purpose serialization format. It embeds type information into the payload (header bytes, length-prefixes), leading to unnecessary allocations and CPU overhead during encoding and decoding.
The **Canonical ABI** (Component Model) defines a highly optimized, flat binary encoding specifically for WebAssembly host/guest communication. It eliminates serialization headers by passing data structurally based on a known schema. For instance, a `[]byte` is passed simply as a pointer and length `(ptr, len)` without encoding the bytes themselves.
Transitioning to Canonical ABI would significantly improve `BenchmarkGlassboxed` performance by avoiding MessagePack encoding on both the host (`binarybridge`) and guest (`gobox-gen`).

## 2. Technical Scope
The transition would touch two primary components:
1.  **`binarybridge`:** We need a custom set of encoders and decoders that implement the Canonical ABI "lifting" (deserialization) and "lowering" (serialization) rules for Go types. MessagePack (`github.com/vmihailenco/msgpack/v5`) will be removed.
2.  **`generator/generator.go`:** The code generator must be updated to output proxy and guest templates that call the new Canonical ABI packing functions rather than `SerializeAsBytes`/`DeserializeFromBytes`.

## 3. Implementation Challenges
### Low-Level Packing
We need to manually pack and align types in memory.
*   **Strings and Slices:** Lowered to `(ptr, len)`.
*   **Structs:** Fields are laid out sequentially according to Canonical ABI alignment rules. This requires complex reflection logic or deep AST inspection in the generator to calculate field offsets correctly.
*   **Maps:** The Canonical ABI does not natively support map structures natively. We would need to define a standard lowering rule (e.g., encoding maps as `[]K` and `[]V` pairs or `[](K, V)` tuples).

### The Generator
Currently, the generator relies on `interface{}` slices for varargs serialization: `SerializeAsBytes([]interface{}{arg1, arg2})`. With the Canonical ABI, the generator must output exact byte-offset writes or call strongly-typed generated lowering functions per argument.

### Error Serialization
Currently, errors can be complex maps or strings inside MessagePack. The Canonical ABI handles `Result<T, E>` types explicitly. We need a way to encode standard Go `error` interfaces (typically as a boolean discriminator + string pointer).

## 4. Proposed Strategy
Given the complexity, a full transition should be done iteratively:
1.  **Phase 1 (Proof of Concept):** Create a standalone package `cabibridge` that implements lowering/lifting for basic primitives (`int32`, `int64`, `float32`, `float64`, `[]byte`, `string`).
2.  **Phase 2 (AST Updates):** Modify the `generator` to use `cabibridge` for simple interfaces that only accept and return primitives or slices.
3.  **Phase 3 (Structs/Maps):** Implement complex structural alignment logic for structs and fallback encodings for maps.
4.  **Phase 4 (Deprecation):** Switch `binarybridge` to use `cabibridge` logic internally and drop `msgpack`.

## 5. Alternative Path (wasi-preview2 / TinyGo)
If `wasi-preview2` (Component Model) becomes standard in the mainline Go compiler (without relying heavily on TinyGo limits), the Canonical ABI binding will be handled natively by the compiler (`wit-bindgen-go`). Glassbox-Go could act purely as a DX orchestrator without needing to implement a custom Canonical ABI packer. This is monitored as Option C in `future_improvements.md`.

## 6. Industry Context & Learnings
Tools like **`wit-bindgen`** (Rust, C, Java, Go bindings), **`wasmtime`** (Rust host), and **`jco`** (JS wrapper) heavily utilize the Canonical ABI. They provide valuable blueprints for how Glassbox-Go can structure its own implementation:

*   **Code Generation over Reflection:** `wit-bindgen` avoids runtime reflection entirely. Instead of generic serialization functions, it parses interface definitions (`.wit` files) to generate raw memory offset writes. Glassbox-Go's generator should adopt this pattern, calculating struct alignments during AST generation rather than at runtime.
*   **Handling Maps:** The Canonical ABI intentionally lacks a map type due to cross-language memory layout differences. Maps are lowered to a list of key-value tuples. Glassbox-Go's generator should translate `map[K]V` into an intermediate `[]struct{ K; V }` prior to packing.
*   **Variant Type Errors:** Errors are packed as `result<T, E>`, starting with a 1-byte discriminator tag (0=OK, 1=Error) followed by the padded payload. Glassbox-Go can adopt this layout for Go's standard `func() (T, error)` signatures.

### Why not use `wit-bindgen` directly?
Glassbox-Go's core value proposition is **zero-config, interface-driven sandboxing**. It operates directly on standard Go code (`type MyInterface interface { ... }`).
Using `wit-bindgen` natively would require a fundamental shift in Developer Experience (DX):
1.  Developers would need to manually write intermediate `.wit` (WebAssembly Interface Type) schema files alongside their Go code.
2.  The build process would require an external Rust-based dependency (`wit-bindgen` CLI) to generate bindings.

To preserve the seamless Go-native experience, Glassbox-Go must either implement Canonical ABI packing in its own code generator (building the binary layouts internally) or eventually leverage standard Go compiler features if `wit-bindgen-go` logic is fully integrated into standard `go build` for WASIP1.

### Why not use `wit-bindgen-go` right now?
While `wit-bindgen-go` exists, it still requires developers to write `.wit` files to define the interfaces and use external tooling. Glassbox-Go aims to auto-generate everything directly from the standard Go AST, hiding the complexity of Wasm and Component Models from the developer entirely. Wrapping `wit-bindgen-go` under the hood could be an option, but it would require dynamically generating `.wit` files from Go AST and shelling out to external Rust CLI tools during the build process, which complicates the tooling significantly.
