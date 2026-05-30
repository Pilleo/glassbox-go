package api

import (
	"log"
	"time"
)

// LogLevel defines the severity of a sandbox log message.
type LogLevel int

const (
	LevelInfo LogLevel = iota
	LevelWarn
	LevelError
)

func (l LogLevel) String() string {
	switch l {
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// SandboxLogger represents a callback type for routing logging outputs.
type SandboxLogger func(level LogLevel, msg string)

// SandboxLimits governs the boundary constraints of Wasm execution.
type SandboxLimits struct {
	maxMemoryPages          *int32
	permissive              bool
	logger                  SandboxLogger
	allowedDirectories      []string
	allowedNetworkAddresses []string
	timeout                 time.Duration
	wasmPath                string
	poolInstances           bool
}

func (s *SandboxLimits) MaxMemoryPages() *int32 {
	return s.maxMemoryPages
}

func (s *SandboxLimits) IsPermissive() bool {
	return s.permissive
}

func (s *SandboxLimits) Logger() SandboxLogger {
	return s.logger
}

func (s *SandboxLimits) AllowedDirectories() []string {
	return s.allowedDirectories
}

func (s *SandboxLimits) AllowedNetworkAddresses() []string {
	return s.allowedNetworkAddresses
}

func (s *SandboxLimits) Timeout() time.Duration {
	return s.timeout
}

func (s *SandboxLimits) WasmPath() string {
	return s.wasmPath
}

func (s *SandboxLimits) PoolInstances() bool {
	return s.poolInstances
}

// SandboxLimitsBuilder is a fluent builder for SandboxLimits.
type SandboxLimitsBuilder struct {
	limits *SandboxLimits
}

func NewBuilder() *SandboxLimitsBuilder {
	return &SandboxLimitsBuilder{
		limits: &SandboxLimits{
			maxMemoryPages:          nil,
			permissive:              false,
			logger:                  func(lvl LogLevel, msg string) { log.Printf("[GUEST][%s] %s", lvl, msg) },
			allowedDirectories:      make([]string, 0),
			allowedNetworkAddresses: make([]string, 0),
			timeout:                 0, // 0 means no timeout
		},
	}
}

func (b *SandboxLimitsBuilder) MaxMemoryPages(max int32) *SandboxLimitsBuilder {
	if max <= 0 {
		max = 1
	} else if max > 65536 {
		max = 65536
	}
	b.limits.maxMemoryPages = &max
	return b
}

func (b *SandboxLimitsBuilder) PermissiveMode() *SandboxLimitsBuilder {
	b.limits.permissive = true
	return b
}

func (b *SandboxLimitsBuilder) Logger(logger SandboxLogger) *SandboxLimitsBuilder {
	b.limits.logger = logger
	return b
}

func (b *SandboxLimitsBuilder) AllowFileSystemAccess(paths ...string) *SandboxLimitsBuilder {
	for _, p := range paths {
		if p != "" {
			b.limits.allowedDirectories = append(b.limits.allowedDirectories, p)
		}
	}
	return b
}

func (b *SandboxLimitsBuilder) AllowNetworkAddresses(addresses ...string) *SandboxLimitsBuilder {
	for _, a := range addresses {
		if a != "" {
			b.limits.allowedNetworkAddresses = append(b.limits.allowedNetworkAddresses, a)
		}
	}
	return b
}

func (b *SandboxLimitsBuilder) Timeout(d time.Duration) *SandboxLimitsBuilder {
	b.limits.timeout = d
	return b
}

func (b *SandboxLimitsBuilder) WasmPath(path string) *SandboxLimitsBuilder {
	b.limits.wasmPath = path
	return b
}

// PoolInstances toggles whether guest modules are reused across invocations.
// WARNING: Enabling this significantly improves performance but introduces severe
// security risks. Reusing instances means linear memory is NOT wiped between calls,
// allowing residual data to leak across sandboxed invocations.
func (b *SandboxLimitsBuilder) PoolInstances(pool bool) *SandboxLimitsBuilder {
	b.limits.poolInstances = pool
	return b
}

func (b *SandboxLimitsBuilder) Build() *SandboxLimits {
	return b.limits
}
