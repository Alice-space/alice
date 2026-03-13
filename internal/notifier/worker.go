package notifier

import (
	"context"
	"fmt"
	"time"

	"alice/internal/domain"
	"alice/internal/platform"
	storepkg "alice/internal/store"
)

// Channel consumes durable events and delivers them to an outbound notification transport.
type Channel interface {
	Name() string
	Enabled() bool
	Cursor() (string, error)
	SaveCursor(string) error
	HandleEvent(context.Context, domain.EventEnvelope) error
}

// Worker replays the event log into one or more notification channels.
type Worker struct {
	store    *storepkg.Store
	logger   platform.Logger
	interval time.Duration
	channels []Channel
}

func NewWorker(store *storepkg.Store, interval time.Duration, logger platform.Logger, channels ...Channel) *Worker {
	if logger == nil {
		logger = platform.NewNoopLogger()
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	active := make([]Channel, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		active = append(active, channel)
	}
	return &Worker{
		store:    store,
		logger:   logger.WithComponent("notifier"),
		interval: interval,
		channels: active,
	}
}

func (w *Worker) Name() string { return "notifier" }

func (w *Worker) Recover(ctx context.Context) error {
	return w.syncOnce(ctx)
}

func (w *Worker) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.syncOnce(ctx); err != nil {
				return err
			}
		}
	}
}

func (w *Worker) syncOnce(ctx context.Context) error {
	if w == nil || w.store == nil || len(w.channels) == 0 {
		return nil
	}
	for _, channel := range w.channels {
		if channel == nil || !channel.Enabled() {
			continue
		}
		if err := w.syncChannel(ctx, channel); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) syncChannel(ctx context.Context, channel Channel) error {
	cursor, err := channel.Cursor()
	if err != nil {
		return fmt.Errorf("load %s notifier cursor: %w", channel.Name(), err)
	}
	return w.store.Replay(ctx, cursor, func(evt domain.EventEnvelope) error {
		if err := channel.HandleEvent(ctx, evt); err != nil {
			w.logger.Error("notifier_channel_event_failed", "channel", channel.Name(), "event_type", evt.EventType, "event_id", evt.EventID, "error", err.Error())
			return err
		}
		if err := channel.SaveCursor(evt.GlobalHLC); err != nil {
			return fmt.Errorf("save %s notifier cursor: %w", channel.Name(), err)
		}
		return nil
	})
}
