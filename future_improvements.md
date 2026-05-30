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

---

## 🚀 Future Roadmap

### 1. Function-Call Fuel Limiter (Gas Instrumentation)

#### The Vision
While cooperative context timeouts halt runaway loops, a malicious plugin can still consume 100% CPU for the duration of that timeout. We want a **precise instruction quota** that preempts execution.

#### The Challenge
Requires pre-processing Wasm bytecode to inject decrement-fuel opcodes into basic blocks.

### 2. Multi-Module Sandbox Orchestration (Micro-Plugin Mesh)

#### The Vision
Enable multiple isolated plugins to communicate directly within the **same Wazero Namespace** without bouncing back to the Go host.

### 3. TinyGo Compatibility
**Status:** Researching
**Impact:** Investigate support for compiling guests with TinyGo for 100x smaller binaries (\~50KB vs 6MB). This requires ensuring compatibility with the generator's emitted code and types.

### 4. Zero-Copy Shared Memory (Large Data Optimization)
Utilize shared memory and atomics to avoid serialization overhead for multi-megabyte payloads.
