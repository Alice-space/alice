package ops

import (
	"context"
	"time"
)

// Worker is the interface for background workers.
type Worker interface {
	Name() string
	Start(ctx context.Context) error
}

// TickWorker is a simple worker that runs a function at regular intervals.
type TickWorker struct {
	name     string
	interval time.Duration
	fn       func(context.Context) error
}

func NewTickWorker(name string, interval time.Duration, fn func(context.Context) error) *TickWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if fn == nil {
		fn = func(context.Context) error { return nil }
	}
	return &TickWorker{name: name, interval: interval, fn: fn}
}

func (w *TickWorker) Name() string { return w.name }

func (w *TickWorker) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.fn(ctx); err != nil {
				return err
			}
		}
	}
}
