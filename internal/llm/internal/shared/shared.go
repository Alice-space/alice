// Package shared provides utilities reused across LLM provider packages
// (MergeEnv, EnvKey, ExtractString) and scanner buffer constants.
package shared

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrPromptEmpty = errors.New("empty prompt")
	ErrLLMTimeout  = errors.New("llm timeout")
)

// MergeEnv merges environment variable overrides into a copy of base,
// sorting keys for determinism. If overrides is empty, base is returned as-is.
func MergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	env := make([]string, len(base))
	copy(env, base)
	indexByKey := make(map[string]int, len(env))
	for i, item := range env {
		key := EnvKey(item)
		if key == "" {
			continue
		}
		indexByKey[key] = i
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pair := key + "=" + overrides[key]
		if idx, ok := indexByKey[key]; ok {
			env[idx] = pair
			continue
		}
		env = append(env, pair)
	}
	return env
}

// EnvKey returns the key portion of a "KEY=VALUE" string, or "" when empty.
func EnvKey(item string) string {
	idx := strings.Index(item, "=")
	if idx <= 0 {
		return ""
	}
	return item[:idx]
}

// ExtractString iterates over keys in the given map and returns the first
// non-empty trimmed string value. Returns "" when no key matches.
func ExtractString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Default scanner buffer and token size constants used across providers.
const (
	DefaultScannerBuf       = 64 * 1024
	MaxScannerTokenSize     = 5 * 1024 * 1024
	MaxScannerTokenSize2MB  = 2 * 1024 * 1024
	MaxScannerTokenSize10MB = 10 * 1024 * 1024
)

// DefaultLLMTimeout is the fallback timeout (48 hours) used by all LLM
// provider runners when no explicit timeout is configured.
const DefaultLLMTimeout = 172800 * time.Second

// AuthCheckTimeout is the default timeout for CLI login/auth status checks.
const AuthCheckTimeout = 15 * time.Second

// RunnerBase holds fields common to all LLM provider runners.
type RunnerBase struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	WorkspaceDir string
}
