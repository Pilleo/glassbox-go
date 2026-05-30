package api

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type SandboxPath string

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

func resolvePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("must be absolute path")
	}

	current := filepath.Clean(path)
	var parts []string
	for {
		evaluated, err := filepath.EvalSymlinks(current)
		if err == nil {
			if len(parts) > 0 {
				for i := len(parts)/2 - 1; i >= 0; i-- {
					opp := len(parts) - 1 - i
					parts[i], parts[opp] = parts[opp], parts[i]
				}
				return filepath.Join(append([]string{evaluated}, parts...)...), nil
			}
			return evaluated, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		parts = append(parts, filepath.Base(current))
		current = parent
	}
	return filepath.Clean(path), nil
}

// CheckFileAccess asserts that the requested absolute path resides within the allowed folder list.
func (s *SecurityGate) CheckFileAccess(ctx context.Context, path string) error {
	limits := GetActiveLimits(ctx)
	if limits == nil {
		return NewSandboxSecurityError("Security Sandbox Violation: Default-Deny blocks file access when limits are not configured")
	}

	if !limits.IsPermissive() {
		if !filepath.IsAbs(path) {
			return NewSandboxSecurityError(fmt.Sprintf("Security Sandbox Violation: Relative paths are not allowed: %s", path))
		}

		allowed := false
		targetResolved, err := resolvePath(path)
		if err != nil {
			return NewSandboxSecurityError(fmt.Sprintf("Security Sandbox Violation: Invalid path resolution for %s", path))
		}

		for _, allowedDir := range limits.AllowedDirectories() {
			dirAbs, err := filepath.Abs(allowedDir)
			if err != nil {
				continue
			}
			dirResolved, err := filepath.EvalSymlinks(dirAbs)
			if err != nil {
				dirResolved = filepath.Clean(dirAbs)
			}

			separator := string(filepath.Separator)
			prefix := dirResolved
			if !strings.HasSuffix(prefix, separator) {
				prefix += separator
			}

			// Match prefix to assert parent directory alignment securely
			if targetResolved == dirResolved || strings.HasPrefix(targetResolved, prefix) {
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
		return NewSandboxSecurityError("Security Sandbox Violation: Default-Deny blocks network egress when limits are not configured")
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
