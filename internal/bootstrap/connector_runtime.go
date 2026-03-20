package bootstrap

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/oklog/run"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

type ConnectorRuntime struct {
	App                 *connector.App
	Processor           *connector.Processor
	AutomationEngine    *automation.Engine
	RuntimeAPI          *runtimeapi.Server
	RuntimeAPIBaseURL   string
	RuntimeAPIToken     string
	MemoryDir           string
	AutomationStatePath string
	CampaignStatePath   string
	PromptLoader        *prompting.Loader
	Config              config.Config
	mu                  sync.Mutex
}

func buildFactoryConfig(cfg config.Config, prompts *prompting.Loader) llm.FactoryConfig {
	defaultEnv := applyLLMProcessEnvDefaults(cfg.CodexEnv)
	return llm.FactoryConfig{
		Provider: cfg.LLMProvider,
		Prompts:  prompts,
		Codex: llm.CodexConfig{
			Command:         cfg.CodexCommand,
			Timeout:         cfg.CodexTimeout,
			Model:           cfg.CodexModel,
			ReasoningEffort: cfg.CodexReasoningEffort,
			Env:             defaultEnv,
			PromptPrefix:    cfg.CodexPromptPrefix,
			WorkspaceDir:    cfg.WorkspaceDir,
		},
		Claude: llm.ClaudeConfig{
			Command:      cfg.ClaudeCommand,
			Timeout:      cfg.ClaudeTimeout,
			Env:          defaultEnv,
			PromptPrefix: cfg.ClaudePromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
		},
		Kimi: llm.KimiConfig{
			Command:      cfg.KimiCommand,
			Timeout:      cfg.KimiTimeout,
			Env:          defaultEnv,
			PromptPrefix: cfg.KimiPromptPrefix,
			WorkspaceDir: cfg.WorkspaceDir,
		},
	}
}

func applyLLMProcessEnvDefaults(raw map[string]string) map[string]string {
	out := make(map[string]string, len(raw)+1)
	for key, value := range raw {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		out[trimmedKey] = trimmedValue
	}
	if strings.TrimSpace(out[config.EnvCodexHome]) == "" {
		out[config.EnvCodexHome] = config.DefaultCodexHome()
	}
	return out
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
