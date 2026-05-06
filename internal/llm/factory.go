package llm

import (
	"fmt"
	"strings"
	"time"
)

const (
	ProviderCodex    = "codex"
	ProviderClaude   = "claude"
	ProviderKimi     = "kimi"
	ProviderOpenCode = "opencode"
)

// FactoryConfig holds configuration for all supported backends. Only the
// fields relevant to the selected Provider need to be populated.
type FactoryConfig struct {
	Provider string
	Codex    CodexConfig
	Claude   ClaudeConfig
	Kimi     KimiConfig
	OpenCode OpenCodeConfig
}

// ProfileRunnerConfig carries per-profile overrides keyed by the profile name
// (e.g. "executor", "reviewer").
type ProfileRunnerConfig struct {
	Command         string
	Timeout         time.Duration
	ProviderProfile string
	ExecPolicy      ExecPolicyConfig
}

// CodexConfig configures the codex CLI backend.
type CodexConfig struct {
	Command            string
	Timeout            time.Duration
	DefaultIdleTimeout time.Duration
	HighIdleTimeout    time.Duration
	XHighIdleTimeout   time.Duration
	Model              string
	ReasoningEffort    string
	Env                map[string]string
	WorkspaceDir       string
	DefaultExecPolicy  ExecPolicyConfig
	// ProfileOverrides maps profile name → per-profile runner overrides.
	ProfileOverrides map[string]ProfileRunnerConfig
}

// ClaudeConfig configures the claude CLI backend.
type ClaudeConfig struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	WorkspaceDir string
	// ProfileOverrides maps profile name → per-profile runner overrides.
	ProfileOverrides map[string]ProfileRunnerConfig
}

// KimiConfig configures the kimi CLI backend.
type KimiConfig struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	WorkspaceDir string
	// ProfileOverrides maps profile name → per-profile runner overrides.
	ProfileOverrides map[string]ProfileRunnerConfig
}

// OpenCodeConfig configures the opencode CLI backend.
type OpenCodeConfig struct {
	Command      string
	Timeout      time.Duration
	Model        string
	Variant      string
	Env          map[string]string
	WorkspaceDir string
	// ServerURL connects to an already-running opencode server instead of
	// spawning `opencode serve`.
	ServerURL string
	// ProfileOverrides maps profile name → per-profile runner overrides.
	ProfileOverrides map[string]ProfileRunnerConfig
}

type providerBundle struct {
	backend Backend
}

func (p providerBundle) Backend() Backend {
	return p.backend
}

// NewProvider constructs a Provider for the backend specified by cfg.Provider.
// For streaming providers, use NewInteractiveProviderSession or buildLLMBackend.
func NewProvider(cfg FactoryConfig) (Provider, error) {
	return nil, fmt.Errorf("all llm providers now require streaming backend (use NewInteractiveProviderSession or buildLLMBackend)")
}

// NewBackend is a convenience wrapper around NewProvider that returns the
// Backend directly.
func NewBackend(cfg FactoryConfig) (Backend, error) {
	provider, err := NewProvider(cfg)
	if err != nil {
		return nil, err
	}
	return provider.Backend(), nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
