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
