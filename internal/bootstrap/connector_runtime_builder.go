package bootstrap

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/oklog/ulid/v2"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/codearmy"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/memory"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

type connectorRuntimePaths struct {
	memoryDir           string
	promptDir           string
	resourceDir         string
	codeArmyStateDir    string
	automationStatePath string
	sessionStatePath    string
	runtimeStatePath    string
}

type connectorRuntimeBuilder struct {
	cfg       config.Config
	backend   llm.Backend
	paths     connectorRuntimePaths
	sender    *connector.LarkSender
	processor *connector.Processor
	app       *connector.App

	memoryManager   *memory.Manager
	automationStore *automation.Store
	promptLoader    *prompting.Loader
	apiServer       *runtimeapi.Server
	apiToken        string
}

func newConnectorRuntimeBuilder(cfg config.Config, provider llm.Provider) (*connectorRuntimeBuilder, error) {
	if provider == nil {
		return nil, errors.New("llm provider is nil")
	}
	backend := provider.Backend()
	if backend == nil {
		return nil, errors.New("llm backend is nil")
	}

	paths := newConnectorRuntimePaths(cfg)
	return &connectorRuntimeBuilder{
		cfg:     cfg,
		backend: backend,
		paths:   paths,
	}, nil
}

func newConnectorRuntimePaths(cfg config.Config) connectorRuntimePaths {
	memoryDir := ResolveMemoryDir(cfg.WorkspaceDir, cfg.MemoryDir)
	return connectorRuntimePaths{
		memoryDir:           memoryDir,
		promptDir:           ResolvePromptDir(cfg.WorkspaceDir, cfg.PromptDir),
		resourceDir:         filepath.Join(memoryDir, "resources"),
		codeArmyStateDir:    filepath.Join(memoryDir, "code_army"),
		automationStatePath: filepath.Join(memoryDir, "automation.db"),
		sessionStatePath:    filepath.Join(memoryDir, "session_state.json"),
		runtimeStatePath:    filepath.Join(memoryDir, "runtime_state.json"),
	}
}

func (b *connectorRuntimeBuilder) Build() (*ConnectorRuntime, error) {
	b.promptLoader = prompting.NewLoader(b.paths.promptDir)
	if err := b.prepareMemory(); err != nil {
		return nil, err
	}

	b.buildSender()
	b.buildAutomationStore()

	if err := b.buildProcessor(); err != nil {
		return nil, err
	}
	if err := b.buildApp(); err != nil {
		return nil, err
	}
	if err := b.buildAutomationEngine(); err != nil {
		return nil, err
	}
	b.buildRuntimeAPI()

	return &ConnectorRuntime{
		App:                 b.app,
		RuntimeAPI:          b.apiServer,
		RuntimeAPIBaseURL:   runtimeapi.BaseURL(b.cfg.RuntimeHTTPAddr),
		RuntimeAPIToken:     b.apiToken,
		MemoryDir:           b.paths.memoryDir,
		AutomationStatePath: b.paths.automationStatePath,
	}, nil
}

func (b *connectorRuntimeBuilder) prepareMemory() error {
	if err := memory.MigrateToScopedLayout(b.paths.memoryDir); err != nil {
		return err
	}

	memoryManager := memory.NewManager(b.paths.memoryDir, b.promptLoader)
	if err := memoryManager.Init(); err != nil {
		return err
	}
	b.memoryManager = memoryManager
	return nil
}

func (b *connectorRuntimeBuilder) buildSender() {
	botClient := lark.NewClient(
		b.cfg.FeishuAppID,
		b.cfg.FeishuAppSecret,
		lark.WithOpenBaseUrl(b.cfg.FeishuBaseURL),
	)
	b.sender = connector.NewLarkSender(botClient, b.paths.resourceDir)
}

func (b *connectorRuntimeBuilder) buildAutomationStore() {
	b.automationStore = automation.NewStore(b.paths.automationStatePath)
}

func (b *connectorRuntimeBuilder) buildProcessor() error {
	processor := connector.NewProcessorWithMemory(
		b.backend,
		b.sender,
		b.cfg.FailureMessage,
		b.cfg.ThinkingMessage,
		b.memoryManager,
	)
	processor.SetCodeArmyCommandDependencies(codearmy.NewInspector(b.paths.codeArmyStateDir), b.automationStore)
	processor.SetImmediateFeedback(b.cfg.ImmediateFeedbackMode, b.cfg.ImmediateFeedbackReaction)
	processor.SetRuntimeAPI(runtimeapi.BaseURL(b.cfg.RuntimeHTTPAddr), b.resolveRuntimeAPIToken())
	loadOptionalState("session state", b.paths.sessionStatePath, processor.LoadSessionState)
	b.processor = processor
	return nil
}

func (b *connectorRuntimeBuilder) buildApp() error {
	app := connector.NewApp(b.cfg, b.processor)
	loadOptionalState("runtime state", b.paths.runtimeStatePath, app.LoadRuntimeState)
	b.app = app
	return nil
}

func (b *connectorRuntimeBuilder) buildAutomationEngine() error {
	if err := b.automationStore.ResetRunningTasks(); err != nil {
		return err
	}
	automationEngine := automation.NewEngine(b.automationStore, b.sender)
	automationEngine.SetUserTaskTimeout(b.cfg.AutomationTaskTimeout)
	automationEngine.SetLLMRunner(b.backend)
	automationEngine.SetWorkflowRunner(codearmy.NewRunner(b.paths.codeArmyStateDir, b.backend, b.promptLoader))

	if err := automationEngine.RegisterSystemTask("system.idle_summary_scan", 60*time.Second, func(runCtx context.Context) {
		b.processor.RunIdleSummaryScan(runCtx, b.cfg.IdleSummaryIdle)
	}); err != nil {
		return err
	}
	if err := automationEngine.RegisterSystemTask("system.state_flush", 1*time.Second, func(context.Context) {
		if err := b.processor.FlushSessionStateIfDirty(); err != nil {
			log.Printf("flush session state failed: %v", err)
		}
		if err := b.app.FlushRuntimeStateIfDirty(); err != nil {
			log.Printf("flush runtime state failed: %v", err)
		}
	}); err != nil {
		return err
	}

	b.app.SetAutomationRunner(automationEngine)
	return nil
}

func (b *connectorRuntimeBuilder) buildRuntimeAPI() {
	b.apiServer = runtimeapi.NewServer(
		b.cfg.RuntimeHTTPAddr,
		b.resolveRuntimeAPIToken(),
		b.sender,
		b.memoryManager,
		b.automationStore,
		codearmy.NewInspector(b.paths.codeArmyStateDir),
	)
}

func (b *connectorRuntimeBuilder) resolveRuntimeAPIToken() string {
	if strings.TrimSpace(b.apiToken) != "" {
		return b.apiToken
	}
	if token := strings.TrimSpace(b.cfg.RuntimeHTTPToken); token != "" {
		b.apiToken = token
		return token
	}
	b.apiToken = strings.ToLower(ulid.Make().String())
	return b.apiToken
}

func loadOptionalState(label, path string, load func(string) error) {
	if load == nil {
		return
	}
	if err := load(path); err != nil {
		log.Printf("load %s failed file=%s err=%v", label, path, err)
		return
	}
	log.Printf("%s enabled file=%s", label, path)
}
