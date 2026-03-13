package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/oklog/run"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

type ConnectorRuntime struct {
	App                 *connector.App
	RuntimeAPI          *runtimeapi.Server
	RuntimeAPIBaseURL   string
	RuntimeAPIToken     string
	MemoryDir           string
	AutomationStatePath string
}

func buildFactoryConfig(cfg config.Config) llm.FactoryConfig {
	return llm.FactoryConfig{
		Provider: cfg.LLMProvider,
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
	}
}

func NewLLMProvider(cfg config.Config) (llm.Provider, error) {
	return llm.NewProvider(buildFactoryConfig(cfg))
}

func RegisterMCPServer(ctx context.Context, provider llm.Provider, cfg config.Config, configPath string) error {
	if provider == nil {
		return errors.New("llm provider is nil")
	}
	registrar := provider.MCPRegistrar()
	if registrar == nil {
		return fmt.Errorf("llm_provider %q does not support mcp registration", cfg.LLMProvider)
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

func (r *ConnectorRuntime) Run(ctx context.Context) error {
	if r == nil || r.App == nil {
		return errors.New("connector runtime is nil")
	}

	var group run.Group
	appCtx, cancelApp := context.WithCancel(ctx)
	group.Add(func() error {
		return r.App.Run(appCtx)
	}, func(error) {
		cancelApp()
	})
	if r.RuntimeAPI != nil {
		apiCtx, cancelAPI := context.WithCancel(ctx)
		group.Add(func() error {
			return r.RuntimeAPI.Run(apiCtx)
		}, func(error) {
			cancelAPI()
		})
	}
	return group.Run()
}
