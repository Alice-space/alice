package ops

import (
	"context"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/store"
)

type Scheduler struct {
	bus          *bus.Runtime
	indexes      *store.BoltIndexStore
	pollInterval time.Duration
}

func NewScheduler(busRuntime *bus.Runtime, indexes *store.BoltIndexStore, pollInterval time.Duration) *Scheduler {
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	return &Scheduler{
		bus:          busRuntime,
		indexes:      indexes,
		pollInterval: pollInterval,
	}
}

func (s *Scheduler) Name() string { return "scheduler" }

func (s *Scheduler) Recover(ctx context.Context) error {
	return s.Tick(ctx, time.Now().UTC())
}

func (s *Scheduler) Start(ctx context.Context) error {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if err := s.Tick(ctx, now.UTC()); err != nil {
				return err
			}
		}
	}
}

func (s *Scheduler) Tick(ctx context.Context, now time.Time) error {
	schedules, err := s.indexes.ListScheduleSources(ctx)
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		if !schedule.Enabled {
			continue
		}
		scheduledForWindow := schedule.NextFireAt
		if scheduledForWindow.IsZero() {
			first, err := domain.NextCronFire(schedule.SpecText, schedule.Timezone, now.UTC().Add(-time.Second))
			if err != nil {
				return err
			}
			scheduledForWindow = first
		}
		if scheduledForWindow.After(now) {
			continue
		}

		fireID := domain.ComputeFireID(schedule.ScheduledTaskID, scheduledForWindow)
		seen, err := s.indexes.DedupeSeen(ctx, fireID)
		if err != nil {
			return err
		}
		if seen {
			continue
		}
		_, err = s.bus.RecordScheduleFire(ctx, domain.RecordScheduleFireCommand{
			ScheduledTaskID:       schedule.ScheduledTaskID,
			ScheduledForWindowUTC: scheduledForWindow.UTC(),
		})
		if err != nil {
			return err
		}
	}
	return nil
}
