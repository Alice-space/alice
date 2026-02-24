package llm

import (
	"fmt"
	"strings"
	"time"
)

const ProviderCodex = "codex"

type FactoryConfig struct {
	Provider string
	Codex    CodexConfig
}

type CodexConfig struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
}

func NewBackend(cfg FactoryConfig) (Backend, error) {
	provider := normalizeProvider(cfg.Provider)

	switch provider {
	case ProviderCodex:
		return newCodexBackend(cfg.Codex), nil
	default:
		return nil, fmt.Errorf("unsupported llm_provider %q", provider)
	}
}

func NewMCPRegistrar(cfg FactoryConfig) (MCPRegistrar, error) {
	provider := normalizeProvider(cfg.Provider)

	switch provider {
	case ProviderCodex:
		return newCodexMCPRegistrar(cfg.Codex), nil
	default:
		return nil, fmt.Errorf("unsupported llm_provider %q", provider)
	}
}

func normalizeProvider(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return ProviderCodex
	}
	return normalized
}
