package llm

import (
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/prompting"
)

const ProviderCodex = "codex"
const ProviderClaude = "claude"
const ProviderKimi = "kimi"

type FactoryConfig struct {
	Provider string
	Prompts  *prompting.Loader
	Codex    CodexConfig
	Claude   ClaudeConfig
	Kimi     KimiConfig
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

type KimiConfig struct {
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
			backend:      newCodexBackend(cfg.Codex, cfg.Prompts),
			mcpRegistrar: newCodexMCPRegistrar(cfg.Codex),
		}, nil
	case ProviderClaude:
		return providerBundle{
			backend:      newClaudeBackend(cfg.Claude, cfg.Prompts),
			mcpRegistrar: newClaudeMCPRegistrar(cfg.Claude),
		}, nil
	case ProviderKimi:
		return providerBundle{
			backend:      newKimiBackend(cfg.Kimi, cfg.Prompts),
			mcpRegistrar: nil,
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
