package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	gapi "github.com/glassbox-go/api"
)

// Engine encapsulates the WebAssembly runtime and cached modules.
type Engine struct {
	rt          wazero.Runtime
	moduleCache map[string]wazero.CompiledModule
	cacheMutex  sync.RWMutex
}

var instanceCounter uint64

// NewEngine initializes a new wazero runtime environment and registers host functions.
func NewEngine(ctx context.Context) (*Engine, error) {
	// Use compiler-based JIT runtime configuration with cooperative context cancellation enabled
	rConfig := wazero.NewRuntimeConfigCompiler().
		WithCloseOnContextDone(true)

	rt := wazero.NewRuntimeWithConfig(ctx, rConfig)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	// Register a high-performance Host Function module to serve guest network RPC requests
	_, err := rt.NewHostModuleBuilder("gobox_host").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, urlPtr, urlLen uint32) uint64 {
			// Security Fix: Fail closed if no limits are active on the context
			if gapi.GetActiveLimits(ctx) == nil {
				return 0xFFFFFFFFFFFFFFFF
			}

			urlBytes, ok := mod.Memory().Read(urlPtr, urlLen)
			if !ok {
				return 0xFFFFFFFFFFFFFFFF
			}
			urlString := string(urlBytes)

			// Delegate out to the virtual capability HTTP client
			client := gapi.NewVirtualHTTPClient()
			resp, err := client.Fetch(ctx, urlString)
			if err != nil {
				panic(err) // Immediately abort guest execution and propagate the security error
			}

			// Allocate scratch memory on the guest to write the response back
			malloc := mod.ExportedFunction("malloc")
			if malloc == nil {
				return 0xFFFFFFFFFFFFFFFF
			}
			res, err := malloc.Call(ctx, uint64(len(resp)))
			if err != nil {
				return 0xFFFFFFFFFFFFFFFF
			}
			respPtr := uint32(res[0])
			mod.Memory().Write(respPtr, []byte(resp))

			// Return a packed pointer and length (upper 32 bits = pointer, lower 32 bits = length)
			return (uint64(respPtr) << 32) | uint64(len(resp))
		}).
		Export("fetch_http").
		Instantiate(ctx)

	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("failed to register gobox_host module: %w", err)
	}

	return &Engine{
		rt:          rt,
		moduleCache: make(map[string]wazero.CompiledModule),
	}, nil
}

// Close gracefully shuts down the engine and releases cached JIT modules.
func (e *Engine) Close(ctx context.Context) error {
	e.ClearCache()
	return e.rt.Close(ctx)
}

// ClearCache resets the compiled modules cache.
func (e *Engine) ClearCache() {
	e.cacheMutex.Lock()
	ctx := context.Background()
	for _, compiled := range e.moduleCache {
		_ = compiled.Close(ctx)
	}
	e.moduleCache = make(map[string]wazero.CompiledModule)
	e.cacheMutex.Unlock()
}

// GetInstance loads, compiles (with AOT caching), and instantiates a Wasm module under explicit limits.
func (e *Engine) GetInstance(ctx context.Context, moduleName string, limits *gapi.SandboxLimits) (api.Module, error) {
	var rt wazero.Runtime
	var compiled wazero.CompiledModule
	var err error

	// If a custom heap page limit is defined, instantiate a dedicated isolated wazero JIT runtime
	if limits != nil && limits.MaxMemoryPages() != nil {
		rConfig := wazero.NewRuntimeConfigCompiler().
			WithCloseOnContextDone(true).
			WithMemoryLimitPages(uint32(*limits.MaxMemoryPages()))

		rt = wazero.NewRuntimeWithConfig(ctx, rConfig)
		wasi_snapshot_preview1.MustInstantiate(ctx, rt)

		wasmBytes, err := loadWasmBytes(moduleName, limits)
		if err != nil {
			return nil, fmt.Errorf("failed to load wasm binary for %s: %w", moduleName, err)
		}

		compiled, err = rt.CompileModule(ctx, wasmBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to JIT compile wasm module %s: %w", moduleName, err)
		}
	} else {
		// Use the engine's cached JIT runtime
		rt = e.rt

		e.cacheMutex.RLock()
		cached, exists := e.moduleCache[moduleName]
		e.cacheMutex.RUnlock()

		if exists {
			compiled = cached
		} else {
			e.cacheMutex.Lock()
			// Double check under write lock
			cached, exists = e.moduleCache[moduleName]
			if exists {
				compiled = cached
				e.cacheMutex.Unlock()
			} else {
				wasmBytes, err := loadWasmBytes(moduleName, limits)
				if err != nil {
					e.cacheMutex.Unlock()
					return nil, fmt.Errorf("failed to load wasm binary for %s: %w", moduleName, err)
				}

				compiled, err = rt.CompileModule(ctx, wasmBytes)
				if err != nil {
					e.cacheMutex.Unlock()
					return nil, fmt.Errorf("failed to JIT compile wasm module %s: %w", moduleName, err)
				}

				e.moduleCache[moduleName] = compiled
				e.cacheMutex.Unlock()
			}
		}
	}

	// Mount Virtual Chroot directories via WASI
	instanceID := atomic.AddUint64(&instanceCounter, 1)
	instanceName := fmt.Sprintf("%s-%d", moduleName, instanceID)
	modConfig := wazero.NewModuleConfig().WithName(instanceName)
	if limits != nil {
		fsConfig := wazero.NewFSConfig()
		for _, dir := range limits.AllowedDirectories() {
			absDir, err := filepath.Abs(dir)
			if err == nil {
				fsConfig = fsConfig.WithDirMount(absDir, absDir)
			}
		}
		modConfig = modConfig.WithFSConfig(fsConfig)

		// Connect guest standard logs redirection
		if limits.Logger() != nil {
			logWriter := &safeLogWriter{logger: limits.Logger()}
			modConfig = modConfig.WithStdout(logWriter).WithStderr(logWriter)
		}
	}

	// Instantiate the module securely
	mod, err := rt.InstantiateModule(ctx, compiled, modConfig)
	if err != nil {
		if limits != nil && limits.MaxMemoryPages() != nil {
			_ = rt.Close(ctx) // Clean up isolated runtime if compilation succeeds but instantiation fails
		}
		return nil, fmt.Errorf("failed to instantiate wasm module: %w", err)
	}

	// For WASIP1 reactor module (c-shared), we must call _initialize before calling other exports
	initFunc := mod.ExportedFunction("_initialize")
	if initFunc != nil {
		_, err = initFunc.Call(ctx)
		if err != nil {
			mod.Close(ctx)
			if limits != nil && limits.MaxMemoryPages() != nil {
				_ = rt.Close(ctx)
			}
			return nil, fmt.Errorf("failed to initialize wasm guest runtime: %w", err)
		}
	}

	if limits != nil && limits.MaxMemoryPages() != nil {
		return &runtimeCloserModule{Module: mod, rt: rt}, nil
	}
	return mod, nil
}

// runtimeCloserModule wraps wazero api.Module and its parent wazero.Runtime to prevent dynamic off-heap leaks
type runtimeCloserModule struct {
	api.Module
	rt wazero.Runtime
}

func (m *runtimeCloserModule) Close(ctx context.Context) error {
	var errs []error
	if err := m.Module.Close(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := m.rt.Close(ctx); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing sandboxed runtime: %v", errs)
	}
	return nil
}

// safeLogWriter converts standard stream bytes into formatted sandbox logger callbacks.
type safeLogWriter struct {
	logger gapi.SandboxLogger
}

func (w *safeLogWriter) Write(p []byte) (n int, err error) {
	if len(p) > 0 {
		// Strip trailing newlines to avoid log clutter
		msg := string(p)
		if msg[len(msg)-1] == '\n' {
			msg = msg[:len(msg)-1]
		}
		w.logger(gapi.LevelInfo, msg)
	}
	return len(p), nil
}

// loadWasmBytes seeks a Wasm binary on disk inside the project resources.
func loadWasmBytes(moduleName string, limits *gapi.SandboxLimits) ([]byte, error) {
	var paths []string

	if limits != nil && limits.WasmPath() != "" {
		paths = append(paths, filepath.Join(limits.WasmPath(), moduleName+".wasm"))
	}

	// Default fallback paths
	paths = append(paths,
		filepath.Join("wasm", moduleName+".wasm"),
		filepath.Join("..", "wasm", moduleName+".wasm"),
	)

	var lastErr error
	for _, path := range paths {
		f, err := os.Open(path)
		if err == nil {
			defer f.Close()
			return io.ReadAll(f)
		}
		lastErr = err
	}

	return nil, fmt.Errorf("could not find wasm binary for %s: %w", moduleName, lastErr)
}
