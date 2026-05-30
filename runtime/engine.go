package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"golang.org/x/sync/singleflight"
	gapi "github.com/glassbox-go/api"
)

type Engine struct {
	rt           wazero.Runtime
	moduleCache  map[string]wazero.CompiledModule
	limitedRTs   map[uint32]*limitedRuntime
	limitedCache map[uint32]map[string]wazero.CompiledModule
	cacheMutex   sync.RWMutex
	compileGrp   singleflight.Group
	instancePool map[string]chan api.Module
	lruOrder     []uint32
}

type limitedRuntime struct {
	rt         wazero.Runtime
	activeReqs int32
	evicted    int32
	closeOnce  sync.Once
}

func (l *limitedRuntime) closeSafe() {
	l.closeOnce.Do(func() {
		go l.rt.Close(context.Background())
	})
}

type wrappedModule struct {
	api.Module
	lrt *limitedRuntime
}

func (w *wrappedModule) Close(ctx context.Context) error {
	err := w.Module.Close(ctx)
	if w.lrt != nil {
		if atomic.AddInt32(&w.lrt.activeReqs, -1) == 0 && atomic.LoadInt32(&w.lrt.evicted) == 1 {
			w.lrt.closeSafe()
		}
	}
	return err
}

var instanceCounter uint64

func registerHostFunctions(ctx context.Context, rt wazero.Runtime) error {
	_, err := rt.NewHostModuleBuilder("gobox_host").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, urlPtr, urlLen uint32) uint64 {
			if gapi.GetActiveLimits(ctx) == nil {
				return 0xFFFFFFFFFFFFFFFF
			}

			urlBytes, ok := mod.Memory().Read(urlPtr, urlLen)
			if !ok {
				return 0xFFFFFFFFFFFFFFFF
			}
			urlString := string(urlBytes)

			client := gapi.NewVirtualHTTPClient()
			resp, fetchErr := client.Fetch(ctx, urlString)
			if fetchErr != nil {
				return 0xFFFFFFFFFFFFFFFF
			}

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

			return (uint64(respPtr) << 32) | uint64(len(resp))
		}).
		Export("fetch_http").
		Instantiate(ctx)

	return err
}

// NewEngine initializes a new wazero runtime environment and registers host functions.
func NewEngine(ctx context.Context) (*Engine, error) {
	rConfig := wazero.NewRuntimeConfigCompiler().
		WithCloseOnContextDone(true)

	rt := wazero.NewRuntimeWithConfig(ctx, rConfig)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	if err := registerHostFunctions(ctx, rt); err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("failed to register gobox_host module: %w", err)
	}

	return &Engine{
		rt:           rt,
		moduleCache:  make(map[string]wazero.CompiledModule),
		limitedRTs:   make(map[uint32]*limitedRuntime),
		limitedCache: make(map[uint32]map[string]wazero.CompiledModule),
		instancePool: make(map[string]chan api.Module),
		lruOrder:     make([]uint32, 0),
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
	for _, lrt := range e.limitedRTs {
		_ = lrt.rt.Close(ctx)
	}
	for _, pool := range e.instancePool {
		for {
			select {
			case mod := <-pool:
				_ = mod.Close(ctx)
			default:
				goto nextPool
			}
		}
	nextPool:
	}
	e.moduleCache = make(map[string]wazero.CompiledModule)
	e.limitedRTs = make(map[uint32]*limitedRuntime)
	e.limitedCache = make(map[uint32]map[string]wazero.CompiledModule)
	e.instancePool = make(map[string]chan api.Module)
	e.lruOrder = make([]uint32, 0)
	e.cacheMutex.Unlock()
}

// GetInstance loads, compiles (with AOT caching), and instantiates a Wasm module under explicit limits.
func (e *Engine) GetInstance(ctx context.Context, moduleName string, limits *gapi.SandboxLimits) (api.Module, error) {
	var rt wazero.Runtime
	var compiled wazero.CompiledModule
	var err error

	var currentLRT *limitedRuntime

	if limits != nil && limits.MaxMemoryPages() != nil {
		memLimit := uint32(*limits.MaxMemoryPages())
		
		e.cacheMutex.Lock()
		if e.limitedRTs[memLimit] == nil {
			if len(e.lruOrder) >= 10 {
				evictKey := e.lruOrder[0]
				e.lruOrder = e.lruOrder[1:]
				if oldLRT := e.limitedRTs[evictKey]; oldLRT != nil {
					atomic.StoreInt32(&oldLRT.evicted, 1)

					suffix := fmt.Sprintf("-%d", evictKey)
					for poolKey, pool := range e.instancePool {
						if strings.HasSuffix(poolKey, suffix) {
							for {
								select {
								case mod := <-pool:
									mod.Close(context.Background())
								default:
									goto donePool
								}
							}
						donePool:
							delete(e.instancePool, poolKey)
						}
					}

					delete(e.limitedRTs, evictKey)
					delete(e.limitedCache, evictKey)

					if atomic.LoadInt32(&oldLRT.activeReqs) == 0 {
						oldLRT.closeSafe()
					}
				}
			}
			e.lruOrder = append(e.lruOrder, memLimit)

			rConfig := wazero.NewRuntimeConfigCompiler().
				WithMemoryLimitPages(memLimit).
				WithCloseOnContextDone(true)
			newRT := wazero.NewRuntimeWithConfig(context.Background(), rConfig)
			wasi_snapshot_preview1.MustInstantiate(context.Background(), newRT)
			if err := registerHostFunctions(context.Background(), newRT); err != nil {
				newRT.Close(ctx)
				e.cacheMutex.Unlock()
				return nil, fmt.Errorf("failed to register gobox_host module for limited rt: %w", err)
			}
			e.limitedRTs[memLimit] = &limitedRuntime{rt: newRT}
			e.limitedCache[memLimit] = make(map[string]wazero.CompiledModule)
		} else {
			// Update LRU
			for i, v := range e.lruOrder {
				if v == memLimit {
					e.lruOrder = append(e.lruOrder[:i], e.lruOrder[i+1:]...)
					e.lruOrder = append(e.lruOrder, memLimit)
					break
				}
			}
		}
		currentLRT = e.limitedRTs[memLimit]
		rt = currentLRT.rt
		atomic.AddInt32(&currentLRT.activeReqs, 1) // Hold the runtime
		e.cacheMutex.Unlock()

		e.cacheMutex.RLock()
		if e.limitedRTs[memLimit] == currentLRT && e.limitedCache[memLimit] != nil {
			compiled = e.limitedCache[memLimit][moduleName]
		}
		e.cacheMutex.RUnlock()

		if compiled == nil {
			compileKey := fmt.Sprintf("%s-limit-%d-%p", moduleName, memLimit, currentLRT)
			res, errSf, _ := e.compileGrp.Do(compileKey, func() (interface{}, error) {
				wasmBytes, loadErr := loadWasmBytes(moduleName, limits)
				if loadErr != nil {
					return nil, fmt.Errorf("failed to load wasm binary for %s: %w", moduleName, loadErr)
				}

				compiledMod, compileErr := rt.CompileModule(ctx, wasmBytes)
				if compileErr != nil {
					return nil, fmt.Errorf("failed to JIT compile wasm module %s: %w", moduleName, compileErr)
				}

				e.cacheMutex.Lock()
				if e.limitedRTs[memLimit] == currentLRT && e.limitedCache[memLimit] != nil {
					e.limitedCache[memLimit][moduleName] = compiledMod
				}
				e.cacheMutex.Unlock()

				return compiledMod, nil
			})
			if errSf != nil {
				if atomic.AddInt32(&currentLRT.activeReqs, -1) == 0 && atomic.LoadInt32(&currentLRT.evicted) == 1 {
					currentLRT.closeSafe()
				}
				return nil, errSf
			}
			compiled = res.(wazero.CompiledModule)
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
			res, errSf, _ := e.compileGrp.Do(moduleName, func() (interface{}, error) {
				e.cacheMutex.RLock()
				cached, exists := e.moduleCache[moduleName]
				e.cacheMutex.RUnlock()
				if exists {
					return cached, nil
				}

				wasmBytes, loadErr := loadWasmBytes(moduleName, limits)
				if loadErr != nil {
					return nil, fmt.Errorf("failed to load wasm binary for %s: %w", moduleName, loadErr)
				}

				compiledMod, compileErr := rt.CompileModule(ctx, wasmBytes)
				if compileErr != nil {
					return nil, fmt.Errorf("failed to JIT compile wasm module %s: %w", moduleName, compileErr)
				}

				e.cacheMutex.Lock()
				e.moduleCache[moduleName] = compiledMod
				e.cacheMutex.Unlock()

				return compiledMod, nil
			})
			if errSf != nil {
				return nil, errSf
			}
			compiled = res.(wazero.CompiledModule)
		}
	}

	// Check Pool
	if limits != nil && limits.PoolInstances() {
		poolKey := moduleName
		if limits.MaxMemoryPages() != nil {
			poolKey = fmt.Sprintf("%s-%d", moduleName, *limits.MaxMemoryPages())
		}
		e.cacheMutex.RLock()
		pool, exists := e.instancePool[poolKey]
		e.cacheMutex.RUnlock()

		if exists {
			select {
			case mod := <-pool:
				return mod, nil
			default:
			}
		} else {
			e.cacheMutex.Lock()
			if e.instancePool[poolKey] == nil {
				e.instancePool[poolKey] = make(chan api.Module, 50)
			}
			e.cacheMutex.Unlock()
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
			stdoutWriter := &safeLogWriter{logger: limits.Logger(), level: gapi.LevelInfo}
			stderrWriter := &safeLogWriter{logger: limits.Logger(), level: gapi.LevelWarn}
			modConfig = modConfig.WithStdout(stdoutWriter).WithStderr(stderrWriter)
		}
	}

	// SECURITY vs PERFORMANCE TRADEOFF: 
	// We instantiate the module fresh on every call and immediately destroy it after execution.
	// This cleanly wipes the sandboxed memory space, preventing data leakage between guest runs.
	// Reusing/pooling instances would improve performance (less allocation and startup cost)
	// but introduces higher security risks such as residual state contamination.
	mod, err := rt.InstantiateModule(ctx, compiled, modConfig)
	if err != nil {
		if currentLRT != nil {
			if atomic.AddInt32(&currentLRT.activeReqs, -1) == 0 && atomic.LoadInt32(&currentLRT.evicted) == 1 {
				currentLRT.closeSafe()
			}
		}
		return nil, fmt.Errorf("failed to instantiate wasm module: %w", err)
	}

	if currentLRT != nil {
		mod = &wrappedModule{
			Module: mod,
			lrt:    currentLRT,
		}
	}

	// For WASIP1 reactor module (c-shared), we must call _initialize before calling other exports
	initFunc := mod.ExportedFunction("_initialize")
	if initFunc != nil {
		_, err = initFunc.Call(ctx)
		if err != nil {
			mod.Close(ctx)
			return nil, fmt.Errorf("failed to initialize wasm guest runtime: %w", err)
		}
	}

	return mod, nil
}

// ReleaseInstance returns the module to the pool if enabled and successful, otherwise closes it.
func (e *Engine) ReleaseInstance(ctx context.Context, mod api.Module, limits *gapi.SandboxLimits, success bool) {
	// If context was cancelled or the execution trapped/failed (success == false), the sandbox memory
	// might be corrupted or leaked (e.g. timeout during malloc).
	// We MUST discard the instance to wipe the memory, ignoring the PoolInstances flag.
	if limits != nil && limits.PoolInstances() && ctx.Err() == nil && success {
		name := mod.Name()
		idx := strings.LastIndex(name, "-")
		moduleName := name
		if idx != -1 {
			moduleName = name[:idx]
		}
		poolKey := moduleName
		if limits.MaxMemoryPages() != nil {
			poolKey = fmt.Sprintf("%s-%d", moduleName, *limits.MaxMemoryPages())
		}

		e.cacheMutex.RLock()
		pool := e.instancePool[poolKey]
		if pool != nil {
			select {
			case pool <- mod:
				e.cacheMutex.RUnlock()
				return
			default:
			}
		}
		e.cacheMutex.RUnlock()
	}
	mod.Close(context.Background())
}

// safeLogWriter converts standard stream bytes into formatted sandbox logger callbacks.
type safeLogWriter struct {
	logger gapi.SandboxLogger
	level  gapi.LogLevel
}

func (w *safeLogWriter) Write(p []byte) (n int, err error) {
	if len(p) > 0 {
		// Strip trailing newline/whitespace to avoid log clutter from fmt.Println etc.
		msg := strings.TrimRight(string(p), "\r\n")
		if msg != "" {
			w.logger(w.level, msg)
		}
	}
	return len(p), nil
}

// loadWasmBytes seeks a Wasm binary on disk inside the project resources.
func loadWasmBytes(moduleName string, limits *gapi.SandboxLimits) ([]byte, error) {
	moduleName = filepath.Base(moduleName)
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
		data, err := os.ReadFile(path)
		if err == nil {
			// Limit Wasm binary size to 50MB to prevent memory exhaustion DoS
			if int64(len(data)) > 50*1024*1024 {
				return nil, fmt.Errorf("wasm binary for %s exceeds 50MB size limit", moduleName)
			}
			return data, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("could not find wasm binary for %s: %w", moduleName, lastErr)
}
