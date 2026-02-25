package llm

import (
	"fmt"
	"strings"
	"time"
)

const ProviderCodex = "codex"
const ProviderClaude = "claude"

type FactoryConfig struct {
	Provider string
	Codex    CodexConfig
	Claude   ClaudeConfig
}

type CodexConfig struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
}

type ClaudeConfig struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
}

type providerBundle struct {
	backend      Backend
	mcpRegistrar MCPRegistrar
}

func (p providerBundle) Backend() Backend {
	return p.backend
}

func (p providerBundle) MCPRegistrar() MCPRegistrar {
	return p.mcpRegistrar
}

func NewProvider(cfg FactoryConfig) (Provider, error) {
	provider := normalizeProvider(cfg.Provider)

	switch provider {
	case ProviderCodex:
		return providerBundle{
			backend:      newCodexBackend(cfg.Codex),
			mcpRegistrar: newCodexMCPRegistrar(cfg.Codex),
		}, nil
	case ProviderClaude:
		return providerBundle{
			backend:      newClaudeBackend(cfg.Claude),
			mcpRegistrar: newClaudeMCPRegistrar(cfg.Claude),
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

func NewMCPRegistrar(cfg FactoryConfig) (MCPRegistrar, error) {
	provider, err := NewProvider(cfg)
	if err != nil {
		return nil, err
	}
	registrar := provider.MCPRegistrar()
	if registrar == nil {
		return nil, fmt.Errorf("llm_provider %q does not support mcp registration", normalizeProvider(cfg.Provider))
	}
	return registrar, nil
}

func normalizeProvider(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return ProviderCodex
	}
	return normalized
}
