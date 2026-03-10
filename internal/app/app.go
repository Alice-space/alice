package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/mcp"
	"alice/internal/ops"
	"alice/internal/platform"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"
)

type App struct {
	Config          *platform.Config
	Logger          *slog.Logger
	Clock           platform.Clock
	IDGen           domain.IDGenerator
	HTTPServer      *http.Server
	Store           *store.Store
	Bus             *bus.Runtime
	Policy          *policy.Engine
	WorkflowRuntime *workflow.Runtime
	MCPRegistry     *mcp.Registry
	OpsHTTP         *ops.HTTPManager
	Workers         []ops.Worker

	ready atomic.Bool
	wg    sync.WaitGroup
	stop  context.CancelFunc
}

func (a *App) Start(ctx context.Context) error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}
	a.ready.Store(false)
	if err := a.Store.RebuildIndexes(ctx); err != nil {
		return fmt.Errorf("startup rebuild indexes: %w", err)
	}
	if err := a.Bus.RestoreStateFromLog(ctx); err != nil {
		return fmt.Errorf("startup restore runtime state: %w", err)
	}
	for _, worker := range a.Workers {
		if recoverer, ok := worker.(interface{ Recover(context.Context) error }); ok {
			if err := recoverer.Recover(ctx); err != nil {
				return fmt.Errorf("startup recover by %s failed: %w", worker.Name(), err)
			}
		}
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	a.stop = cancel
	for _, worker := range a.Workers {
		w := worker
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					a.Logger.Error("worker panic", "worker", w.Name(), "panic", r)
				}
			}()
			if err := w.Start(workerCtx); err != nil {
				a.Logger.Error("worker exited", "worker", w.Name(), "error", err)
			}
		}()
	}

	go func() {
		err := a.HTTPServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			a.Logger.Error("http server exited", "error", err)
		}
	}()
	a.ready.Store(true)
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	a.ready.Store(false)
	if a.stop != nil {
		a.stop()
	}
	wait := make(chan struct{})
	go func() {
		defer close(wait)
		a.wg.Wait()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wait:
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.HTTPServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return a.Store.Close()
}

func (a *App) Ready() bool { return a.ready.Load() }
