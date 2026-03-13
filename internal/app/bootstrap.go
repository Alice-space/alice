package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"alice/internal/agent"
	"alice/internal/bus"
	"alice/internal/feishu"
	"alice/internal/ingress"
	"alice/internal/mcp"
	"alice/internal/ops"
	"alice/internal/platform"
	"alice/internal/store"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

// Bootstrap creates and initializes the application with all dependencies.
// Uses uber-go/fx for dependency injection.
func Bootstrap(cfg *platform.Config) (*App, error) {
	var app *App

	err := fx.New(
		fx.Supply(cfg),
		fx.Provide(
			// Logger
			provideLogger,
			// Clock
			provideClock,
			// ID Generator
			provideIDGenerator,
			// Store
			provideStore,
			// Policy Engine
			providePolicyEngine,
			// Workflow Registry
			provideWorkflowRegistry,
			// Workflow Runtime
			provideWorkflowRuntime,
			// Bus Runtime
			provideBusRuntime,
			// MCP Registry
			provideMCPRegistry,
			// Feishu
			provideFeishuService,
			// HTTP Manager
			provideHTTPManager,
			// Local Agent
			provideLocalAgent,
			// Reception
			provideReception,
			// Ingress
			provideIngress,
			// Workers
			provideWorkers,
			// Gin Engine
			provideGinEngineWithIngress,
			// HTTP Server
			provideHTTPServer,
			// App
			NewApp,
		),
		fx.Invoke(func(a *App, w []ops.Worker) {
			app = a
			app.Workers = w
		}),
	).Start(context.Background())

	if err != nil {
		return nil, fmt.Errorf("fx bootstrap: %w", err)
	}

	// Load workflows after registry is created
	if err := app.WorkflowRuntime.Registry().LoadRoots(context.Background(), cfg.Workflow.ManifestRoots); err != nil {
		_ = app.Store.Close()
		return nil, fmt.Errorf("load workflows: %w", err)
	}

	// Setup hooks
	app.Bus.SetCriticalFailureHandler(func(err error) {
		app.ready = false
		app.Logger.Error("critical index failure; fail-fast", "error", err.Error())
	})

	// Setup direct answer executor if enabled
	if cfg.Agent.EnableDirectAnswer {
		timeout, _ := time.ParseDuration(cfg.Agent.Timeout)
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		localAgent := agent.NewLocalAgent(agent.Config{
			KimiExecutable: cfg.Agent.KimiExecutable,
			WorkDir:        cfg.Agent.WorkDir,
			Timeout:        timeout,
			MaxSteps:       cfg.Agent.MaxSteps,
			SkillsDir:      cfg.Agent.SkillsDir,
			Logger:         app.Logger,
		})
		directExecutor := agent.NewDirectAnswerExecutor(localAgent, app.Logger)
		app.Bus.SetDirectAnswerExecutor(directExecutor)
	}

	return app, nil
}

func provideIngress(
	busRuntime *bus.Runtime,
	reception bus.Reception,
	cfg *platform.Config,
	feishuService *feishu.Service,
) *ingress.HTTPIngress {
	humanActionSecret := cfg.Auth.HumanActionSecret
	if humanActionSecret == "" {
		humanActionSecret = cfg.Auth.AdminToken
	}
	schedulerIngressSecret := cfg.Auth.SchedulerIngressSecret
	if schedulerIngressSecret == "" {
		schedulerIngressSecret = cfg.Auth.AdminToken
	}

	return ingress.NewHTTPIngress(busRuntime, reception, humanActionSecret, feishuService, ingress.WebhookAuthConfig{
		GitHubSecret:    cfg.Auth.GitHubWebhookSecret,
		GitLabSecret:    cfg.Auth.GitLabWebhookSecret,
		SchedulerSecret: schedulerIngressSecret,
	})
}

func provideWorkers(
	cfg *platform.Config,
	busRuntime *bus.Runtime,
	st *store.Store,
	mcpRegistry *mcp.Registry,
	feishuService *feishu.Service,
	logger platform.Logger,
) []ops.Worker {
	schedulerPoll, _ := time.ParseDuration(cfg.Scheduler.PollInterval)
	schedulerWorker := ops.NewScheduler(busRuntime, st.Indexes, schedulerPoll)
	outboxReconciler := mcp.NewOutboxReconciler(st.Indexes, mcpRegistry, busRuntime)
	outboxDispatcher := mcp.NewOutboxDispatcher(st.Indexes, mcpRegistry, busRuntime)
	scheduleReconciler := ops.NewScheduleFireReconciler(schedulerWorker, st.Indexes, time.Minute, 5)
	workers := []ops.Worker{
		ops.NewTickWorker("request-expirer", time.Minute, func(context.Context) error { return nil }),
		ops.NewTickWorker("step-ready-dispatcher", 5*time.Second, func(context.Context) error { return nil }),
		outboxReconciler,
		ops.NewTickWorker("approval-expirer", time.Minute, func(context.Context) error { return nil }),
		schedulerWorker,
		scheduleReconciler,
		outboxDispatcher,
		ops.NewTickWorker("projection-rebuilder", 10*time.Minute, func(ctx context.Context) error {
			return st.RebuildIndexes(ctx)
		}),
	}
	if feishuService != nil && feishuService.Enabled() {
		workers = append(workers, feishu.NewReplyWorker(st, feishuService, logger))
	} else {
		workers = append(workers, ops.NewTickWorker("notifier", 15*time.Second, func(context.Context) error { return nil }))
	}
	return workers
}

func provideGinEngineWithIngress(
	cfg *platform.Config,
	opsHTTP *ops.HTTPManager,
	ing *ingress.HTTPIngress,
	busRuntime *bus.Runtime,
) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(adminTokenMiddleware(cfg.Auth.AdminToken))

	// Health routes
	r.GET("/healthz", func(c *gin.Context) {
		c.String(200, "ok")
	})
	r.GET("/readyz", func(c *gin.Context) {
		c.String(200, "ready")
	})
	r.GET("/metrics", func(c *gin.Context) {
		overview, err := busRuntime.Indexes().Overview(c.Request.Context())
		if err != nil {
			c.String(500, err.Error())
			return
		}
		c.String(200,
			"alice_request_open_total %d\nalice_task_active_total %d\nalice_outbox_pending_total %d\nalice_gate_open_total %d",
			overview.OpenRequests, overview.ActiveTasks, overview.PendingOutbox, overview.ApprovalQueue,
		)
	})

	// Register routes
	opsHTTP.RegisterRoutesGin(r)
	v1 := r.Group("/v1")
	ing.RegisterRoutes(v1)

	return r
}

func adminTokenMiddleware(token string) gin.HandlerFunc {
	expected := strings.TrimSpace(token)
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/v1/admin/") {
			c.Next()
			return
		}
		if expected == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "admin token is not configured"})
			return
		}

		got := strings.TrimSpace(c.GetHeader("X-Admin-Token"))
		if got == "" {
			auth := strings.TrimSpace(c.GetHeader("Authorization"))
			if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
				got = strings.TrimSpace(auth[len("Bearer "):])
			}
		}
		if got != expected {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
