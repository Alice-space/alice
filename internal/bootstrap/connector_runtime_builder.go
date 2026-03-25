package bootstrap

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/oklog/ulid/v2"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

type connectorRuntimePaths struct {
	stateRoot           string
	promptDir           string
	resourceDir         string
	automationStatePath string
	campaignStatePath   string
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

	automationStore  *automation.Store
	campaignStore    *campaign.Store
	automationEngine *automation.Engine
	campaignRepoMu   sync.Mutex
	promptLoader     *prompting.Loader
	apiServer        *runtimeapi.Server
	apiToken         string
}

func newConnectorRuntimeBuilder(cfg config.Config, backend llm.Backend) (*connectorRuntimeBuilder, error) {
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
	stateRoot := ResolveRuntimeStateRoot(cfg.AliceHome)
	return connectorRuntimePaths{
		stateRoot:           stateRoot,
		promptDir:           ResolvePromptDir(cfg.WorkspaceDir, cfg.PromptDir),
		resourceDir:         filepath.Join(stateRoot, "resources"),
		automationStatePath: filepath.Join(stateRoot, "automation.db"),
		campaignStatePath:   filepath.Join(stateRoot, "campaigns.db"),
		sessionStatePath:    filepath.Join(stateRoot, "session_state.json"),
		runtimeStatePath:    filepath.Join(stateRoot, "runtime_state.json"),
	}
}

func (b *connectorRuntimeBuilder) Build() (*ConnectorRuntime, error) {
	b.promptLoader = prompting.NewLoader(b.paths.promptDir)
	b.buildSender()
	b.buildAutomationStore()
	b.buildCampaignStore()

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
		Processor:           b.processor,
		AutomationEngine:    b.automationEngine,
		RuntimeAPI:          b.apiServer,
		RuntimeAPIBaseURL:   runtimeapi.BaseURL(b.cfg.RuntimeHTTPAddr),
		RuntimeAPIToken:     b.apiToken,
		AutomationStatePath: b.paths.automationStatePath,
		CampaignStatePath:   b.paths.campaignStatePath,
		SessionStatePath:    b.paths.sessionStatePath,
		PromptLoader:        b.promptLoader,
		Config:              b.cfg,
	}, nil
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

func (b *connectorRuntimeBuilder) buildCampaignStore() {
	b.campaignStore = campaign.NewStore(b.paths.campaignStatePath)
}

func (b *connectorRuntimeBuilder) buildProcessor() error {
	processor := connector.NewProcessor(
		b.backend,
		b.sender,
		b.cfg.FailureMessage,
		b.cfg.ThinkingMessage,
	)
	processor.SetPromptLoader(b.promptLoader)
	processor.SetImmediateFeedback(b.cfg.ImmediateFeedbackMode, b.cfg.ImmediateFeedbackReaction)
	processor.SetRuntimeAPI(
		runtimeapi.BaseURL(b.cfg.RuntimeHTTPAddr),
		b.resolveRuntimeAPIToken(),
		ResolveRuntimeBinary(b.cfg.WorkspaceDir),
	)
	processor.SetStatusStores(b.automationStore, b.campaignStore)
	processor.SetStatusIdentity(b.cfg.BotID, b.cfg.BotName)
	processor.SetStatusUsageSources([]connector.StatusUsageSource{{
		BotID:            b.cfg.BotID,
		BotName:          b.cfg.BotName,
		SessionStatePath: b.paths.sessionStatePath,
	}})
	if err := processor.SetImageGeneration(b.cfg.ImageGeneration, b.cfg.CodexEnv); err != nil {
		return err
	}
	loadOptionalState("session state", b.paths.sessionStatePath, processor.LoadSessionState)
	b.processor = processor
	return nil
}

func (b *connectorRuntimeBuilder) buildApp() error {
	app := connector.NewApp(b.cfg, b.processor)
	app.SetPromptLoader(b.promptLoader)
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
	automationEngine.SetWorkflowRunner(automation.NewPromptWorkflowRunner(b.backend))
	automationEngine.SetRunEnv(map[string]string{
		runtimeapi.EnvBaseURL: runtimeapi.BaseURL(b.cfg.RuntimeHTTPAddr),
		runtimeapi.EnvToken:   b.resolveRuntimeAPIToken(),
		runtimeapi.EnvBin:     ResolveRuntimeBinary(b.cfg.WorkspaceDir),
	})
	automationEngine.SetUserTaskCompletionHook(b.handleCampaignRepoAutomationTaskCompletion)

	if err := automationEngine.RegisterSystemTask("system.state_flush", 1*time.Second, func(context.Context) {
		if err := b.processor.FlushSessionStateIfDirty(); err != nil {
			logging.Warnf("flush session state failed: %v", err)
		}
		if err := b.app.FlushRuntimeStateIfDirty(); err != nil {
			logging.Warnf("flush runtime state failed: %v", err)
		}
	}); err != nil {
		return err
	}
	if err := b.registerCampaignRepoSystemTask(); err != nil {
		return err
	}

	b.app.SetAutomationRunner(automationEngine)
	b.automationEngine = automationEngine
	return nil
}

func (b *connectorRuntimeBuilder) buildRuntimeAPI() {
	b.apiServer = runtimeapi.NewServer(
		b.cfg.RuntimeHTTPAddr,
		b.resolveRuntimeAPIToken(),
		b.sender,
		b.automationStore,
		b.campaignStore,
		b.cfg,
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
		logging.Warnf("load %s failed file=%s err=%v", label, path, err)
		return
	}
	logging.Infof("%s enabled file=%s", label, path)
}
