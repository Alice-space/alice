package bootstrap

import (
	"context"
	"log"
	"path/filepath"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"gitee.com/alicespace/alice/internal/automation"
	"gitee.com/alicespace/alice/internal/codearmy"
	"gitee.com/alicespace/alice/internal/config"
	"gitee.com/alicespace/alice/internal/connector"
	"gitee.com/alicespace/alice/internal/llm"
	"gitee.com/alicespace/alice/internal/memory"
)

type ConnectorRuntime struct {
	App                 *connector.App
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
	}
}

func RegisterMCPServer(ctx context.Context, cfg config.Config, configPath string) error {
	configAbsPath := ResolveConfigPath(configPath)
	registrar, err := llm.NewMCPRegistrar(buildFactoryConfig(cfg))
	if err != nil {
		return err
	}
	return registrar.EnsureMCPServerRegistered(ctx, llm.MCPRegistration{
		ServerName:    cfg.CodexMCPServerName,
		ServerCommand: ResolveMCPServerCommand(configAbsPath),
		ServerArgs:    []string{"-c", configAbsPath},
	})
}

func BuildConnectorRuntime(cfg config.Config) (*ConnectorRuntime, error) {
	botClient := lark.NewClient(
		cfg.FeishuAppID,
		cfg.FeishuAppSecret,
		lark.WithOpenBaseUrl(cfg.FeishuBaseURL),
	)

	backend, err := llm.NewBackend(buildFactoryConfig(cfg))
	if err != nil {
		return nil, err
	}

	memoryDir := ResolveMemoryDir(cfg.WorkspaceDir, cfg.MemoryDir)
	memoryManager := memory.NewManager(memoryDir)
	if err := memoryManager.Init(); err != nil {
		return nil, err
	}
	resourceDir := filepath.Join(memoryDir, "resources")
	sender := connector.NewLarkSender(botClient, resourceDir)

	processor := connector.NewProcessorWithMemory(
		backend,
		sender,
		cfg.FailureMessage,
		cfg.ThinkingMessage,
		memoryManager,
	)
	sessionStatePath := filepath.Join(memoryDir, "session_state.json")
	if err := processor.LoadSessionState(sessionStatePath); err != nil {
		log.Printf("load session state failed file=%s err=%v", sessionStatePath, err)
	} else {
		log.Printf("session state enabled file=%s", sessionStatePath)
	}

	app := connector.NewApp(cfg, processor)
	runtimeStatePath := filepath.Join(memoryDir, "runtime_state.json")
	if err := app.LoadRuntimeState(runtimeStatePath); err != nil {
		log.Printf("load runtime state failed file=%s err=%v", runtimeStatePath, err)
	} else {
		log.Printf("runtime state enabled file=%s", runtimeStatePath)
	}

	automationStatePath := filepath.Join(memoryDir, "automation_state.json")
	automationEngine := automation.NewEngine(automation.NewStore(automationStatePath), sender)
	automationEngine.SetUserTaskTimeout(cfg.AutomationTaskTimeout)
	automationEngine.SetLLMRunner(backend)
	automationEngine.SetWorkflowRunner(codearmy.NewRunner(filepath.Join(memoryDir, "code_army"), backend))

	if err := automationEngine.RegisterSystemTask("system.idle_summary_scan", 60*time.Second, func(runCtx context.Context) {
		processor.RunIdleSummaryScan(runCtx, cfg.IdleSummaryIdle)
	}); err != nil {
		return nil, err
	}
	if err := automationEngine.RegisterSystemTask("system.state_flush", 1*time.Second, func(context.Context) {
		if err := processor.FlushSessionStateIfDirty(); err != nil {
			log.Printf("flush session state failed: %v", err)
		}
		if err := app.FlushRuntimeStateIfDirty(); err != nil {
			log.Printf("flush runtime state failed: %v", err)
		}
	}); err != nil {
		return nil, err
	}

	app.SetAutomationRunner(automationEngine)

	return &ConnectorRuntime{
		App:                 app,
		MemoryDir:           memoryDir,
		AutomationStatePath: automationStatePath,
	}, nil
}
