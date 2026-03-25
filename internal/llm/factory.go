package llm

import (
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/prompting"
)

const ProviderCodex = "codex"
const ProviderClaude = "claude"
const ProviderGemini = "gemini"
const ProviderKimi = "kimi"

type FactoryConfig struct {
	Provider string
	Prompts  *prompting.Loader
	Codex    CodexConfig
	Claude   ClaudeConfig
	Gemini   GeminiConfig
	Kimi     KimiConfig
}

// ProfileRunnerConfig carries per-profile overrides for command, timeout, and prompt prefix.
// Keyed by the outer llm_profiles map name (e.g. "executor", "reviewer").
type ProfileRunnerConfig struct {
	Command      string
	Timeout      time.Duration
	PromptPrefix string
}

type CodexConfig struct {
	Command         string
	Timeout         time.Duration
	Model           string
	ReasoningEffort string
	Env             map[string]string
	PromptPrefix    string
	WorkspaceDir    string
	// ProfileOverrides maps outer profile name -> per-profile command/timeout/prompt_prefix.
	ProfileOverrides map[string]ProfileRunnerConfig
}

type ClaudeConfig struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
	// ProfileOverrides maps outer profile name -> per-profile command/timeout/prompt_prefix.
	ProfileOverrides map[string]ProfileRunnerConfig
}

type GeminiConfig struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
	// ProfileOverrides maps outer profile name -> per-profile command/timeout/prompt_prefix.
	ProfileOverrides map[string]ProfileRunnerConfig
}

type KimiConfig struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
	// ProfileOverrides maps outer profile name -> per-profile command/timeout/prompt_prefix.
	ProfileOverrides map[string]ProfileRunnerConfig
}

type providerBundle struct {
	backend Backend
}

func (p providerBundle) Backend() Backend {
	return p.backend
}

func NewProvider(cfg FactoryConfig) (Provider, error) {
	provider := normalizeProvider(cfg.Provider)

	switch provider {
	case ProviderCodex:
		return providerBundle{
			backend: newCodexBackend(cfg.Codex, cfg.Prompts),
		}, nil
	case ProviderClaude:
		return providerBundle{
			backend: newClaudeBackend(cfg.Claude, cfg.Prompts),
		}, nil
	case ProviderGemini:
		return providerBundle{
			backend: newGeminiBackend(cfg.Gemini, cfg.Prompts),
		}, nil
	case ProviderKimi:
		return providerBundle{
			backend: newKimiBackend(cfg.Kimi, cfg.Prompts),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported llm_provider %q", provider)
	}
}

func NewBackend(cfg FactoryConfig) (Backend, error) {
	provider, err := NewProvider(cfg)
	if err != nil {
		return nil, err
	}
	return provider.Backend(), nil
}

func normalizeProvider(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return ProviderCodex
	}
	return normalized
}
