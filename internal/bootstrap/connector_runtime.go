package bootstrap

import (
	"context"
	"errors"

	"github.com/oklog/run"

	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/prompting"
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
