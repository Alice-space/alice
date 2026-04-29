package bootstrap

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/oklog/ulid/v2"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/connector"
	"github.com/Alice-space/alice/internal/logging"
	feishu "github.com/Alice-space/alice/internal/platform/feishu"
	"github.com/Alice-space/alice/internal/prompting"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

type connectorRuntimePaths struct {
	stateRoot           string
	promptDir           string
	resourceDir         string
	automationStatePath string
	sessionStatePath    string
	runtimeStatePath    string
}

type connectorRuntimeBuilder struct {
	cfg       config.Config
	backend   agentbridge.Backend
	paths     connectorRuntimePaths
	sender    *feishu.FeishuSender
	processor *connector.Processor
	app       *connector.App
	botOpenID string

	automationStore  *automation.Store
	automationEngine *automation.Engine
	promptLoader     *prompting.Loader
	apiServer        *runtimeapi.Server
	apiToken         string
}

func newConnectorRuntimeBuilder(cfg config.Config, backend agentbridge.Backend) (*connectorRuntimeBuilder, error) {
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
		sessionStatePath:    filepath.Join(stateRoot, "session_state.json"),
		runtimeStatePath:    filepath.Join(stateRoot, "runtime_state.json"),
	}
}

func (b *connectorRuntimeBuilder) Build() (*ConnectorRuntime, error) {
	b.promptLoader = prompting.NewLoader(b.paths.promptDir)
	b.buildSender()
	b.fetchBotIdentity()
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
		Processor:           b.processor,
		AutomationEngine:    b.automationEngine,
		RuntimeAPI:          b.apiServer,
		RuntimeAPIBaseURL:   runtimeapi.BaseURL(b.cfg.RuntimeHTTPAddr),
		RuntimeAPIToken:     b.apiToken,
		AutomationStatePath: b.paths.automationStatePath,
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
	b.sender = feishu.NewFeishuSender(botClient, b.paths.resourceDir)
}

func (b *connectorRuntimeBuilder) fetchBotIdentity() {
	if b.sender == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := feishu.FetchBotSelfInfo(ctx, b.sender.Client())
	if err != nil {
		logging.Warnf("fetch bot identity failed (degraded mode): %v", err)
		return
	}
	b.botOpenID = info.OpenID
	if b.botOpenID != "" {
		logging.Infof("bot identity fetched open_id=%s", b.botOpenID)
	}
}

func (b *connectorRuntimeBuilder) buildAutomationStore() {
	b.automationStore = automation.NewStore(b.paths.automationStatePath)
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
	processor.SetWorkspaceDir(strings.TrimSpace(b.cfg.WorkspaceDir))
	processor.SetRuntimeAPI(
		runtimeapi.BaseURL(b.cfg.RuntimeHTTPAddr),
		b.resolveRuntimeAPIToken(),
		ResolveRuntimeBinary(b.cfg.WorkspaceDir),
	)
	processor.SetStatusStores(b.automationStore)
	processor.SetStatusIdentity(b.cfg.BotID, b.cfg.BotName)
	processor.SetStatusUsageSources([]connector.StatusUsageSource{{
		BotID:            b.cfg.BotID,
		BotName:          b.cfg.BotName,
		SessionStatePath: b.paths.sessionStatePath,
	}})
	loadOptionalState("session state", b.paths.sessionStatePath, processor.LoadSessionState)
	b.processor = processor
	return nil
}

func (b *connectorRuntimeBuilder) buildApp() error {
	app := connector.NewApp(b.cfg, b.processor)
	app.SetPromptLoader(b.promptLoader)
	app.SetBotOpenID(b.botOpenID)
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
	automationEngine.SetRunEnv(map[string]string{
		runtimeapi.EnvBaseURL: runtimeapi.BaseURL(b.cfg.RuntimeHTTPAddr),
		runtimeapi.EnvToken:   b.resolveRuntimeAPIToken(),
		runtimeapi.EnvBin:     ResolveRuntimeBinary(b.cfg.WorkspaceDir),
	})

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

	if err := automationEngine.RegisterSystemTask("system.scheduler_watchdog", time.Minute, func(ctx context.Context) {
		automationEngine.RunWatchdogOnce(ctx)
	}); err != nil {
		return err
	}

	b.app.SetAutomationRunner(automationEngine)
	automationEngine.SetSessionActivityChecker(b.app)
	b.automationEngine = automationEngine
	return nil
}

func (b *connectorRuntimeBuilder) buildRuntimeAPI() {
	b.apiServer = runtimeapi.NewServer(
		b.cfg.RuntimeHTTPAddr,
		b.resolveRuntimeAPIToken(),
		b.sender,
		b.automationStore,
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
