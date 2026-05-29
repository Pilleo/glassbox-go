package api

import (
	"log"
	"time"
)

// SandboxLogger represents a callback type for routing logging outputs.
type SandboxLogger func(level string, msg string)

// SandboxLimits governs the boundary constraints of Wasm execution.
type SandboxLimits struct {
	maxInstructions         int64
	maxMemoryPages          int32
	strict                  bool
	logger                  SandboxLogger
	allowedDirectories      []string
	allowedNetworkAddresses []string
	timeout                 time.Duration
}

func (s *SandboxLimits) MaxInstructions() int64 {
	return s.maxInstructions
}

func (s *SandboxLimits) MaxMemoryPages() int32 {
	return s.maxMemoryPages
}

func (s *SandboxLimits) IsStrict() bool {
	return s.strict
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

// SandboxLimitsBuilder is a fluent builder for SandboxLimits.
type SandboxLimitsBuilder struct {
	limits *SandboxLimits
}

func NewBuilder() *SandboxLimitsBuilder {
	return &SandboxLimitsBuilder{
		limits: &SandboxLimits{
			maxInstructions:         -1,
			maxMemoryPages:          -1,
			strict:                  true,
			logger:                  func(lvl, msg string) { log.Printf("[GUEST][%s] %s", lvl, msg) },
			allowedDirectories:      make([]string, 0),
			allowedNetworkAddresses: make([]string, 0),
			timeout:                 0, // 0 means no timeout
		},
	}
}

func (b *SandboxLimitsBuilder) MaxInstructions(max int64) *SandboxLimitsBuilder {
	b.limits.maxInstructions = max
	return b
}

func (b *SandboxLimitsBuilder) MaxMemoryPages(max int32) *SandboxLimitsBuilder {
	b.limits.maxMemoryPages = max
	return b
}

func (b *SandboxLimitsBuilder) Strict(strict bool) *SandboxLimitsBuilder {
	b.limits.strict = strict
	return b
}

func (b *SandboxLimitsBuilder) Logger(logger SandboxLogger) *SandboxLimitsBuilder {
	b.limits.logger = logger
	return b
}

func (b *SandboxLimitsBuilder) AllowFileSystemAccess(path string) *SandboxLimitsBuilder {
	if path != "" {
		b.limits.allowedDirectories = append(b.limits.allowedDirectories, path)
	}
	return b
}

func (b *SandboxLimitsBuilder) AllowNetworkAddresses(addresses []string) *SandboxLimitsBuilder {
	if addresses != nil {
		b.limits.allowedNetworkAddresses = append(b.limits.allowedNetworkAddresses, addresses...)
	}
	return b
}

func (b *SandboxLimitsBuilder) Timeout(d time.Duration) *SandboxLimitsBuilder {
	b.limits.timeout = d
	return b
}

func (b *SandboxLimitsBuilder) Build() *SandboxLimits {
	return b.limits
}
