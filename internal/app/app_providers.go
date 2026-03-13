package app

import (
	"net/http"
	"time"

	"alice/internal/agent"
	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/feishu"
	"alice/internal/mcp"
	"alice/internal/ops"
	"alice/internal/platform"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"

	"github.com/gin-gonic/gin"
)

// Provider functions for dependency injection

func provideLogger(cfg *platform.Config) (platform.Logger, error) {
	logger, err := platform.NewLoggerFromConfig(cfg.Logging)
	if err != nil {
		return platform.NewDefaultLogger(), nil
	}
	return logger, nil
}

func provideClock() platform.Clock {
	return platform.RealClock{}
}

func provideIDGenerator() domain.IDGenerator {
	return domain.NewULIDGenerator()
}

func provideStore(cfg *platform.Config) (*store.Store, error) {
	return store.Open(store.Config{
		RootDir:          cfg.Storage.RootDir,
		SnapshotInterval: cfg.Storage.SnapshotInterval,
	})
}

func providePolicyEngine(cfg *platform.Config) *policy.Engine {
	return policy.NewEngine(policy.Config{
		MinConfidence: cfg.Promotion.MinConfidence,
		DirectAllowlist: []string{
			"direct_query",
			"weather_query",
			"cluster_readonly_query",
			"general_query",
		},
	})
}

func provideWorkflowRegistry() *workflow.Registry {
	return workflow.NewRegistry(nil)
}

func provideWorkflowRuntime(registry *workflow.Registry) *workflow.Runtime {
	return workflow.NewRuntime(registry)
}

func provideBusRuntime(
	st *store.Store,
	policyEngine *policy.Engine,
	workflowRuntime *workflow.Runtime,
	idGen domain.IDGenerator,
	cfg *platform.Config,
	logger platform.Logger,
) *bus.Runtime {
	return bus.NewRuntime(st, policyEngine, workflowRuntime, idGen, bus.Config{ShardCount: cfg.Runtime.ShardCount}, logger)
}

func provideMCPRegistry(cfg *platform.Config) *mcp.Registry {
	registry := mcp.NewRegistry()
	for domainName, domainCfg := range cfg.MCP.Domains {
		if domainCfg.BaseURL == "" {
			continue
		}
		registry.Register(domainName, mcp.NewHTTPClient(domainCfg.BaseURL))
	}
	return registry
}

func provideFeishuService(cfg *platform.Config, logger platform.Logger) (*feishu.Service, error) {
	return feishu.NewService(feishu.Config{
		Enabled:           cfg.Feishu.Enabled,
		AppID:             cfg.Feishu.AppID,
		AppSecret:         cfg.Feishu.AppSecret,
		VerificationToken: cfg.Feishu.VerificationToken,
		EncryptKey:        cfg.Feishu.EncryptKey,
		ReplyInThread:     cfg.Feishu.ReplyInThread,
	}, cfg.Storage.RootDir, logger)
}

func provideHTTPManager(
	st *store.Store,
	busRuntime *bus.Runtime,
	reception bus.Reception,
	cfg *platform.Config,
) *ops.HTTPManager {
	return ops.NewHTTPManager(st, busRuntime, reception, ops.AdminHooks{}, ops.SurfaceConfig{
		AdminEventInjectionEnabled:     cfg.Ops.AdminEventInjectionEnabled,
		AdminScheduleFireReplayEnabled: cfg.Ops.AdminScheduleFireReplayEnabled,
	})
}

func provideLocalAgent(cfg *platform.Config, logger platform.Logger) *agent.LocalAgent {
	timeout, _ := time.ParseDuration(cfg.Agent.Timeout)
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return agent.NewLocalAgent(agent.Config{
		KimiExecutable: cfg.Agent.KimiExecutable,
		WorkDir:        cfg.Agent.WorkDir,
		Timeout:        timeout,
		MaxSteps:       cfg.Agent.MaxSteps,
		SkillsDir:      cfg.Agent.SkillsDir,
		Logger:         logger,
	})
}

func provideReception(
	cfg *platform.Config,
	localAgent *agent.LocalAgent,
	idGen domain.IDGenerator,
	logger platform.Logger,
) bus.Reception {
	if cfg.Agent.EnableDirectAnswer {
		return policy.NewLLMReception(localAgent, idGen, logger)
	}
	return policy.NewStaticReception(idGen)
}

func provideHTTPServer(cfg *platform.Config, engine *gin.Engine) *http.Server {
	return &http.Server{
		Addr:    cfg.HTTP.ListenAddr,
		Handler: engine,
	}
}
