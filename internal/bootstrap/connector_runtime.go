package bootstrap

import (
	"context"
	"errors"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/prompting"
)

type ConnectorRuntime struct {
	App                 *connector.App
	MemoryDir           string
	AutomationStatePath string
}

func buildFactoryConfig(cfg config.Config, prompts *prompting.Loader) llm.FactoryConfig {
	return llm.FactoryConfig{
		Provider: cfg.LLMProvider,
		Prompts:  prompts,
		Codex: llm.CodexConfig{
			Command:      cfg.CodexCommand,
			Timeout:      cfg.CodexTimeout,
			Env:          cfg.CodexEnv,
			PromptPrefix: cfg.CodexPromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
		},
		Claude: llm.ClaudeConfig{
			Command:      cfg.ClaudeCommand,
			Timeout:      cfg.ClaudeTimeout,
			Env:          cfg.CodexEnv,
			PromptPrefix: cfg.ClaudePromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
		},
		Kimi: llm.KimiConfig{
			Command:      cfg.KimiCommand,
			Timeout:      cfg.KimiTimeout,
			Env:          cfg.CodexEnv,
			PromptPrefix: cfg.KimiPromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
		},
	}
}

func NewLLMProvider(cfg config.Config) (llm.Provider, error) {
	promptDir := ResolvePromptDir(cfg.WorkspaceDir, cfg.PromptDir)
	return llm.NewProvider(buildFactoryConfig(cfg, prompting.NewLoader(promptDir)))
}

func RegisterMCPServer(ctx context.Context, provider llm.Provider, cfg config.Config, configPath string) error {
	if provider == nil {
		return errors.New("llm provider is nil")
	}
	registrar := provider.MCPRegistrar()
	if registrar == nil {
		return nil
	}
	configAbsPath := ResolveConfigPath(configPath)
	return registrar.EnsureMCPServerRegistered(ctx, llm.MCPRegistration{
		ServerName:    cfg.CodexMCPServerName,
		ServerCommand: ResolveMCPServerCommand(configAbsPath),
		ServerArgs:    []string{"-c", configAbsPath},
	})
}

func BuildConnectorRuntime(cfg config.Config, provider llm.Provider) (*ConnectorRuntime, error) {
	builder, err := newConnectorRuntimeBuilder(cfg, provider)
	if err != nil {
		return nil, err
	}
	return builder.Build()
}
