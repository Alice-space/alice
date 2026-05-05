package llm

import (
	"fmt"
	"strings"
	"time"
)

const (
	ProviderCodex    = "codex"
	ProviderClaude   = "claude"
	ProviderGemini   = "gemini"
	ProviderKimi     = "kimi"
	ProviderOpenCode = "opencode"
)

// FactoryConfig holds configuration for all supported backends. Only the
// fields relevant to the selected Provider need to be populated.
type FactoryConfig struct {
	Provider string
	Codex    CodexConfig
	Claude   ClaudeConfig
	Gemini   GeminiConfig
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
	// DisableStreamJSON makes NewInteractiveProviderSession fall back to the
	// one-shot claude runner. It is intended for experimental rollback only.
	DisableStreamJSON bool
	// ProfileOverrides maps profile name → per-profile runner overrides.
	ProfileOverrides map[string]ProfileRunnerConfig
}

// GeminiConfig configures the gemini CLI backend.
type GeminiConfig struct {
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
	// DisableAppServer makes NewInteractiveProviderSession fall back to the
	// one-shot `opencode run` wrapper. It is intended for experimental rollback.
	DisableAppServer bool
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
// An empty Provider defaults to codex.
func NewProvider(cfg FactoryConfig) (Provider, error) {
	provider := normalizeProvider(cfg.Provider)
	if provider == "" {
		provider = ProviderCodex
	}

	switch provider {
	case ProviderCodex:
		return providerBundle{backend: newCodexBackend(cfg.Codex)}, nil
	case ProviderClaude:
		return providerBundle{backend: newClaudeBackend(cfg.Claude)}, nil
	case ProviderGemini:
		return providerBundle{backend: newGeminiBackend(cfg.Gemini)}, nil
	case ProviderKimi:
		return providerBundle{backend: newKimiBackend(cfg.Kimi)}, nil
	case ProviderOpenCode:
		return providerBundle{backend: newOpenCodeBackend(cfg.OpenCode)}, nil
	default:
		return nil, fmt.Errorf("unsupported llm_provider %q", provider)
	}
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
