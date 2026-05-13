// Package shared provides scanner buffer constants used across LLM providers.
package shared

import "time"

// Default scanner buffer and token size constants used across providers.
const (
	DefaultScannerBuf       = 64 * 1024
	MaxScannerTokenSize2MB  = 2 * 1024 * 1024
	MaxScannerTokenSize10MB = 10 * 1024 * 1024
)

// AuthCheckTimeout is the default timeout for CLI login/auth status checks.
const AuthCheckTimeout = 15 * time.Second
