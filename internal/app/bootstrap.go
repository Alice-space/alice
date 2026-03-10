package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/ingress"
	"alice/internal/mcp"
	"alice/internal/ops"
	"alice/internal/platform"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"
)

func Bootstrap(cfg *platform.Config) (*App, error) {
	logger := platform.NewLogger()
	idgen := domain.NewULIDGenerator()

	st, err := store.Open(store.Config{
		RootDir:          cfg.Storage.RootDir,
		SnapshotInterval: cfg.Storage.SnapshotInterval,
	})
	if err != nil {
		return nil, err
	}

	registry := workflow.NewRegistry(nil)
	if err := registry.LoadRoots(context.Background(), cfg.Workflow.ManifestRoots); err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("load workflows: %w", err)
	}
	workflowRuntime := workflow.NewRuntime(registry)
	policyEngine := policy.NewEngine(policy.Config{
		MinConfidence: cfg.Promotion.MinConfidence,
		DirectAllowlist: []string{
			"direct_query",
			"weather_query",
			"cluster_readonly_query",
			"general_query",
		},
	})
	busRuntime := bus.NewRuntime(st, policyEngine, workflowRuntime, idgen, bus.Config{ShardCount: cfg.Runtime.ShardCount})
	reception := policy.NewStaticReception(idgen)

	mcpRegistry := mcp.NewRegistry()
	for domainName, domainCfg := range cfg.MCP.Domains {
		if domainCfg.BaseURL == "" {
			continue
		}
		mcpRegistry.Register(domainName, mcp.NewHTTPClient(domainCfg.BaseURL))
	}

	schedulerPoll, _ := time.ParseDuration(cfg.Scheduler.PollInterval)
	schedulerWorker := ops.NewScheduler(busRuntime, st.Indexes, schedulerPoll)
	outboxReconciler := mcp.NewOutboxReconciler(st.Indexes, mcpRegistry, busRuntime)
	outboxDispatcher := mcp.NewOutboxDispatcher(st.Indexes, mcpRegistry, busRuntime)

	mux := http.NewServeMux()
	app := &App{
		Config:          cfg,
		Logger:          logger,
		Clock:           platform.RealClock{},
		IDGen:           idgen,
		Store:           st,
		Bus:             busRuntime,
		Policy:          policyEngine,
		WorkflowRuntime: workflowRuntime,
		MCPRegistry:     mcpRegistry,
	}
	busRuntime.SetCriticalFailureHandler(func(err error) {
		app.ready.Store(false)
		app.Logger.Error("critical index failure; fail-fast", "error", err)
		if app.stop != nil {
			app.stop()
		}
		if app.HTTPServer != nil {
			_ = app.HTTPServer.Close()
		}
	})
	registerHealthRoutes(mux, app)

	humanActionSecret := cfg.Auth.HumanActionSecret
	if strings.TrimSpace(humanActionSecret) == "" {
		humanActionSecret = cfg.Auth.AdminToken
	}
	schedulerIngressSecret := cfg.Auth.SchedulerIngressSecret
	if strings.TrimSpace(schedulerIngressSecret) == "" {
		schedulerIngressSecret = cfg.Auth.AdminToken
	}
	ing := ingress.NewHTTPIngress(busRuntime, reception, humanActionSecret, ingress.WebhookAuthConfig{
		GitHubSecret:    cfg.Auth.GitHubWebhookSecret,
		GitLabSecret:    cfg.Auth.GitLabWebhookSecret,
		SchedulerSecret: schedulerIngressSecret,
	})
	ing.RegisterRoutes(mux)

	opsHTTP := ops.NewHTTPManager(st, busRuntime, reception, ops.AdminHooks{
		ReconcileOutbox: func(_ *http.Request) error {
			return outboxReconciler.Reconcile(context.Background())
		},
		ReconcileSchedules: func(r *http.Request) error {
			return schedulerWorker.Tick(r.Context(), time.Now().UTC())
		},
		RebuildIndexes: func(r *http.Request) error {
			return st.RebuildIndexes(r.Context())
		},
		ReplayFromHLC: func(r *http.Request, hlc string) error {
			return st.Replay(r.Context(), hlc, func(domain.EventEnvelope) error { return nil })
		},
	}, ops.SurfaceConfig{
		AdminEventInjectionEnabled:     cfg.Ops.AdminEventInjectionEnabled,
		AdminScheduleFireReplayEnabled: cfg.Ops.AdminScheduleFireReplayEnabled,
	})
	app.OpsHTTP = opsHTTP
	opsHTTP.RegisterRoutes(mux)

	handler := withAdminAuth(mux, cfg.Auth.AdminToken)
	app.HTTPServer = &http.Server{
		Addr:    cfg.HTTP.ListenAddr,
		Handler: handler,
	}
	app.Workers = buildWorkers(schedulerWorker, outboxDispatcher, outboxReconciler, st)
	return app, nil
}

func withAdminAuth(next http.Handler, token string) http.Handler {
	trimmedToken := strings.TrimSpace(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/admin/") {
			if trimmedToken == "" {
				http.Error(w, "admin token is not configured", http.StatusServiceUnavailable)
				return
			}
			if trimmedToken != "" && r.Header.Get("X-Admin-Token") != trimmedToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func registerHealthRoutes(mux *http.ServeMux, app *App) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if app.Ready() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready"))
			return
		}
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		overview, err := app.Store.Indexes.Overview(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintf(w, "alice_request_open_total %d\n", overview.OpenRequests)
		_, _ = fmt.Fprintf(w, "alice_task_active_total %d\n", overview.ActiveTasks)
		_, _ = fmt.Fprintf(w, "alice_outbox_pending_total %d\n", overview.PendingOutbox)
		_, _ = fmt.Fprintf(w, "alice_gate_open_total %d\n", overview.ApprovalQueue)
	})
}
