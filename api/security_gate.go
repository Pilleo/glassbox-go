package api

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type contextKey string

const activeLimitsKey contextKey = "gobox:limits"

// WithActiveLimits attaches SandboxLimits to the context for goroutine-scoped capability evaluation.
func WithActiveLimits(ctx context.Context, limits *SandboxLimits) context.Context {
	return context.WithValue(ctx, activeLimitsKey, limits)
}

// GetActiveLimits retrieves any bound SandboxLimits from the context.
func GetActiveLimits(ctx context.Context) *SandboxLimits {
	if val := ctx.Value(activeLimitsKey); val != nil {
		if limits, ok := val.(*SandboxLimits); ok {
			return limits
		}
	}
	return nil
}

// SandboxSecurityError represents a boundary violation inside the sandboxed execution framework.
type SandboxSecurityError struct {
	Message string
}

func (e *SandboxSecurityError) Error() string {
	return e.Message
}

func NewSandboxSecurityError(msg string) error {
	return &SandboxSecurityError{Message: msg}
}

// SecurityGate filters OS resource accesses against active Context boundaries.
type SecurityGate struct{}

// CheckFileAccess asserts that the requested path resides within the allowed folder list.
func (s *SecurityGate) CheckFileAccess(ctx context.Context, path string) error {
	limits := GetActiveLimits(ctx)
	if limits == nil {
		return nil // No bounds set, allow execution
	}

	if !limits.IsPermissive() {
		allowed := false
		targetAbs, err := filepath.Abs(path)
		if err != nil {
			return NewSandboxSecurityError(fmt.Sprintf("Security Sandbox Violation: Invalid file path: %s", path))
		}
		targetClean := filepath.Clean(targetAbs)

		for _, allowedDir := range limits.AllowedDirectories() {
			dirAbs, err := filepath.Abs(allowedDir)
			if err != nil {
				continue
			}
			dirClean := filepath.Clean(dirAbs)

			separator := string(filepath.Separator)
			prefix := dirClean
			if !strings.HasSuffix(prefix, separator) {
				prefix += separator
			}

			// Match prefix to assert parent directory alignment securely
			if targetClean == dirClean || strings.HasPrefix(targetClean, prefix) {
				allowed = true
				break
			}
		}

		if !allowed {
			return NewSandboxSecurityError(fmt.Sprintf("Security Sandbox Violation: Unauthorized filesystem access to %s", path))
		}
	}
	return nil
}

// CheckNetworkAccess asserts that the requested target address aligns with the allowed network egress firewall bounds.
func (s *SecurityGate) CheckNetworkAccess(ctx context.Context, hostAndPort string) error {
	limits := GetActiveLimits(ctx)
	if limits == nil {
		return nil
	}

	if !limits.IsPermissive() {
		allowed := false
		targetNorm := strings.ToLower(strings.TrimSpace(hostAndPort))

		for _, allowedAddr := range limits.AllowedNetworkAddresses() {
			allowedNorm := strings.ToLower(strings.TrimSpace(allowedAddr))
			if targetNorm == allowedNorm || strings.HasPrefix(targetNorm, allowedNorm+":") {
				allowed = true
				break
			}
		}

		if !allowed {
			return NewSandboxSecurityError(fmt.Sprintf("Security Sandbox Violation: Unauthorized network egress to %s", hostAndPort))
		}
	}
	return nil
}
