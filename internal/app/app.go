package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/feishu"
	"alice/internal/mcp"
	"alice/internal/ops"
	"alice/internal/platform"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"

	"github.com/oklog/run"
)

// App holds all application components.
type App struct {
	Config          *platform.Config
	Logger          platform.Logger
	Clock           platform.Clock
	IDGen           domain.IDGenerator
	HTTPServer      *http.Server
	Store           *store.Store
	Bus             *bus.Runtime
	Policy          *policy.Engine
	WorkflowRuntime *workflow.Runtime
	MCPRegistry     *mcp.Registry
	AgentMCPServer  *mcp.Server
	OpsHTTP         *ops.HTTPManager
	Workers         []ops.Worker
	Feishu          *feishu.Service

	ready    bool
	runGroup *run.Group
	runDone  chan error
	runStop  context.CancelFunc
}

// NewApp creates a new App with all dependencies injected.
func NewApp(
	cfg *platform.Config,
	logger platform.Logger,
	clock platform.Clock,
	idGen domain.IDGenerator,
	st *store.Store,
	busRuntime *bus.Runtime,
	policyEngine *policy.Engine,
	workflowRuntime *workflow.Runtime,
	mcpRegistry *mcp.Registry,
	agentMCPServer *mcp.Server,
	opsHTTP *ops.HTTPManager,
	feishuService *feishu.Service,
	httpServer *http.Server,
	workers []ops.Worker,
) *App {
	return &App{
		Config:          cfg,
		Logger:          logger,
		Clock:           clock,
		IDGen:           idGen,
		Store:           st,
		Bus:             busRuntime,
		Policy:          policyEngine,
		WorkflowRuntime: workflowRuntime,
		MCPRegistry:     mcpRegistry,
		AgentMCPServer:  agentMCPServer,
		OpsHTTP:         opsHTTP,
		Feishu:          feishuService,
		HTTPServer:      httpServer,
		Workers:         workers,
		runGroup:        &run.Group{},
	}
}

// Start starts the application and all its components.
func (a *App) Start(ctx context.Context) (err error) {
	a.ready = false
	a.runGroup = &run.Group{}
	a.runDone = make(chan error, 1)
	a.runStop = nil
	mcpStarted := false
	defer func() {
		if err != nil && mcpStarted && a.AgentMCPServer != nil {
			_ = a.AgentMCPServer.Stop(context.Background())
		}
	}()

	// Initialize store
	if err := a.Store.RebuildIndexes(ctx); err != nil {
		return fmt.Errorf("startup rebuild indexes: %w", err)
	}

	if a.AgentMCPServer != nil {
		if err := a.AgentMCPServer.Start(); err != nil {
			return fmt.Errorf("startup start agent mcp server: %w", err)
		}
		mcpStarted = true
	}

	// Restore bus state
	if err := a.Bus.RestoreStateFromLog(ctx); err != nil {
		return fmt.Errorf("startup restore runtime state: %w", err)
	}

	// Recover workers
	for _, worker := range a.Workers {
		if recoverer, ok := worker.(interface{ Recover(context.Context) error }); ok {
			if err := recoverer.Recover(ctx); err != nil {
				return fmt.Errorf("startup recover by %s failed: %w", worker.Name(), err)
			}
		}
	}

	// Setup oklog/run group for component lifecycle management

	// HTTP server actor
	a.runGroup.Add(func() error {
		a.Logger.Info("http_server_starting", "addr", a.HTTPServer.Addr)
		err := a.HTTPServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}, func(err error) {
		a.Logger.Info("http_server_stopping")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.HTTPServer.Shutdown(shutdownCtx)
	})

	// Workers actor
	workerCtx, workerCancel := context.WithCancel(context.Background())
	a.runGroup.Add(func() error {
		a.Logger.Info("workers_starting", "count", len(a.Workers))
		// Run all workers and wait for any to exit
		var g run.Group
		for _, w := range a.Workers {
			worker := w // capture range variable
			g.Add(func() error {
				return worker.Start(workerCtx)
			}, func(err error) {
				// Individual worker stop is handled by context cancellation
			})
		}
		return g.Run()
	}, func(err error) {
		a.Logger.Info("workers_stopping")
		workerCancel()
	})

	// Stop actor: external shutdown triggers this context cancellation.
	runCtx, runCancel := context.WithCancel(context.Background())
	a.runStop = runCancel
	a.runGroup.Add(func() error {
		<-runCtx.Done()
		return nil
	}, func(err error) {})

	go func() {
		a.runDone <- a.runGroup.Run()
	}()

	a.ready = true
	a.Logger.Info("app_started")

	return nil
}

// Shutdown gracefully shuts down the application.
func (a *App) Shutdown(ctx context.Context) error {
	a.ready = false

	if a.runStop != nil {
		a.runStop()
	}

	var runErr error
	select {
	case <-ctx.Done():
		return ctx.Err()
	case runErr = <-a.runDone:
	}
	if runErr != nil {
		a.Logger.Error("shutdown_error", "error", runErr)
	}

	// Close store
	if a.AgentMCPServer != nil {
		if err := a.AgentMCPServer.Stop(ctx); err != nil {
			return err
		}
	}
	if err := a.Store.Close(); err != nil {
		return err
	}
	if a.Feishu != nil {
		if err := a.Feishu.Close(); err != nil {
			return err
		}
	}

	a.Logger.Info("app_shutdown_complete")
	return nil
}

// Ready returns true if the app is ready to serve requests.
func (a *App) Ready() bool { return a.ready }
