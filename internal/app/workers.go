package app

import (
	"context"
	"time"

	"alice/internal/mcp"
	"alice/internal/ops"
	"alice/internal/store"
)

func buildWorkers(scheduler *ops.Scheduler, outboxDispatcher *mcp.OutboxDispatcher, outboxReconciler *mcp.OutboxReconciler, st *store.Store) []ops.Worker {
	scheduleReconciler := ops.NewScheduleFireReconciler(scheduler, st.Indexes, time.Minute, 5)
	return []ops.Worker{
		ops.NewTickWorker("request-expirer", time.Minute, func(context.Context) error { return nil }),
		ops.NewTickWorker("step-ready-dispatcher", 5*time.Second, func(context.Context) error { return nil }),
		outboxReconciler,
		ops.NewTickWorker("approval-expirer", time.Minute, func(context.Context) error { return nil }),
		scheduler,
		scheduleReconciler,
		outboxDispatcher,
		ops.NewTickWorker("projection-rebuilder", 10*time.Minute, func(ctx context.Context) error {
			return st.RebuildIndexes(ctx)
		}),
		ops.NewTickWorker("notifier", 15*time.Second, func(context.Context) error { return nil }),
	}
}
