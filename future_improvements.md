# Future Improvements: Glassbox-Go

This document details the design and implementation blueprints for advanced capabilities planned for future phases of the **Glassbox-Go** transparent, JIT WebAssembly sandboxing framework.

---

## 1. Instruction Fuel Limiter (Strict Gas Preemption)

### The Vision
While cooperative context timeout halts runaway infinite loops, it still allows a malicious plugin to consume 100% CPU on a core for the duration of the timeout (e.g., 1 second). We want a **precise instruction quota (fuel/gas limit)** that preempts execution after executing exactly $N$ instructions, even if the timeout duration is not yet reached.

### Implementation Blueprint
Wazero compiles modules into in-memory machine instructions. We can hook a custom compilation compiler listener or register an experimental JIT block listener (`experimental.FunctionListener`) that increments a thread-safe counter.

```go
package runtime

import (
	"context"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

type InstructionLimiter struct {
	MaxInstructions int64
	CurrentCount    int64
}

// FunctionListener implementation
func (l *InstructionLimiter) Before(ctx context.Context, mod api.Module, def api.FunctionDefinition, params []uint64, stack experimental.StackIterator) {
	l.CurrentCount++
	if l.CurrentCount > l.MaxInstructions {
		// Panic automatically halts JIT compilation or execution and traps in wazero safely
		panic(gapi.NewSandboxSecurityError("CPU gas limit exceeded"))
	}
}

func (l *InstructionLimiter) After(ctx context.Context, mod api.Module, def api.FunctionDefinition, results []uint64) {}
```

To register this, we use wazero's JIT listener factory during compilation configuration, keeping the guest completely oblivious to the CPU gas tracking overhead.

---

## 2. Multi-Module Sandbox Orchestration (Micro-Plugin Mesh)

### The Vision
Enterprise architectures frequently require executing multiple isolated pipelines sequentially (e.g., executing an Authentication check, evaluating Business Rules, and executing a Logger). Routing data `Wasm -> Go Host -> Wasm -> Go Host` incurs high boundary-crossing latency. We want to orchestrate multiple Wasm modules inside the **same Runtime Namespace** and link their exports together directly.

### The Architecture

```
[Go Host Application]
       │ (Single JIT Call)
       ▼
┌────────────────── Wazero Namespace ──────────────────┐
│                                                      │
│  [AuthModule] ──► [RuleModule] ──► [LoggerModule]    │
│    (Wasm)           (Wasm)            (Wasm)         │
│                                                      │
└──────────────────────────────────────────────────────┘
```

### Implementation Blueprint
We can link modules dynamically using wazero's Namespace APIs:
1.  **Instantiate Dependencies**: Instantiate the utility modules first in the namespace:
    ```go
    loggerModule, _ := rt.InstantiateModule(ctx, compiledLogger, config.WithName("logger"))
    ```
2.  **Bind Linkage**: When instantiating the main orchestrator, wazero automatically resolves any imports matching `"logger"` inside the same namespace, binding the calls directly at compilation time.
3.  **Perform Execution**: The Go host makes a single JIT invocation to the orchestrator, which executes the entire pipeline natively in WebAssembly JIT space at near-native speeds.
