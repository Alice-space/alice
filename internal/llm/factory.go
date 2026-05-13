package llm

import (
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

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}
