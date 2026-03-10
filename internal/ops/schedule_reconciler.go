package ops

import (
	"context"
	"time"

	"alice/internal/domain"
	"alice/internal/store"
)

type ScheduleFireReconciler struct {
	scheduler  *Scheduler
	indexes    *store.BoltIndexStore
	interval   time.Duration
	maxCatchup int
}

func NewScheduleFireReconciler(scheduler *Scheduler, indexes *store.BoltIndexStore, interval time.Duration, maxCatchup int) *ScheduleFireReconciler {
	if interval <= 0 {
		interval = time.Minute
	}
	if maxCatchup <= 0 {
		maxCatchup = 5
	}
	return &ScheduleFireReconciler{
		scheduler:  scheduler,
		indexes:    indexes,
		interval:   interval,
		maxCatchup: maxCatchup,
	}
}

func (r *ScheduleFireReconciler) Name() string { return "schedule-fire-reconciler" }

func (r *ScheduleFireReconciler) Recover(ctx context.Context) error {
	return r.Reconcile(ctx, time.Now().UTC())
}

func (r *ScheduleFireReconciler) Start(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if err := r.Reconcile(ctx, now.UTC()); err != nil {
				return err
			}
		}
	}
}

func (r *ScheduleFireReconciler) Reconcile(ctx context.Context, now time.Time) error {
	sources, err := r.indexes.ListScheduleSources(ctx)
	if err != nil {
		return err
	}
	for _, src := range sources {
		if !src.Enabled || src.NextFireAt.IsZero() || src.NextFireAt.After(now) {
			continue
		}
		next := src.NextFireAt
		catchups := 0
		for !next.After(now) && catchups < r.maxCatchup {
			fireID := domain.ComputeFireID(src.ScheduledTaskID, next)
			seen, err := r.indexes.DedupeSeen(ctx, fireID)
			if err != nil {
				return err
			}
			if !seen {
				if _, err := r.scheduler.bus.RecordScheduleFire(ctx, domain.RecordScheduleFireCommand{
					ScheduledTaskID:       src.ScheduledTaskID,
					ScheduledForWindowUTC: next.UTC(),
				}); err != nil {
					return err
				}
			}
			next, err = domain.NextCronFire(src.SpecText, src.Timezone, next.UTC())
			if err != nil {
				return err
			}
			catchups++
		}
	}
	return nil
}
