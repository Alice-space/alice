package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alice/internal/domain"
	"alice/internal/platform"
	storepkg "alice/internal/store"
)

// ReplyWorker tails the event log and delivers replies back to Feishu.
type ReplyWorker struct {
	store    *storepkg.Store
	service  *Service
	logger   platform.Logger
	interval time.Duration
}

func NewReplyWorker(store *storepkg.Store, service *Service, logger platform.Logger) *ReplyWorker {
	if logger == nil {
		logger = platform.NewNoopLogger()
	}
	return &ReplyWorker{
		store:    store,
		service:  service,
		logger:   logger.WithComponent("feishu-reply-worker"),
		interval: 5 * time.Second,
	}
}

func (w *ReplyWorker) Name() string { return "feishu-reply-worker" }

func (w *ReplyWorker) Recover(ctx context.Context) error {
	return w.syncOnce(ctx)
}

func (w *ReplyWorker) Start(ctx context.Context) error {
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

func (w *ReplyWorker) syncOnce(ctx context.Context) error {
	if w == nil || w.store == nil || w.service == nil || !w.service.Enabled() {
		return nil
	}
	cursor, err := w.service.state.Cursor()
	if err != nil {
		return fmt.Errorf("load feishu cursor: %w", err)
	}
	return w.store.Replay(ctx, cursor, func(evt domain.EventEnvelope) error {
		if err := w.processEvent(ctx, evt); err != nil {
			return err
		}
		return w.service.state.SaveCursor(evt.GlobalHLC)
	})
}

func (w *ReplyWorker) processEvent(ctx context.Context, evt domain.EventEnvelope) error {
	switch evt.EventType {
	case domain.EventTypeExternalEventIngested:
		return w.recordTarget(evt)
	case domain.EventTypeReplyRecorded:
		return w.deliverReply(ctx, evt)
	default:
		return nil
	}
}

func (w *ReplyWorker) recordTarget(evt domain.EventEnvelope) error {
	var payload domain.ExternalEventIngestedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(payload.Event.TransportKind) != TransportKind {
		return nil
	}
	meta, err := DecodeMetadataPatch(payload.Event.InputPatch)
	if err != nil {
		return err
	}
	target := meta.ReplyTarget(w.service.cfg.ReplyInThread)
	if !target.Valid() {
		return nil
	}
	return w.service.state.SaveTarget(strings.TrimSpace(payload.Event.EventID), target)
}

func (w *ReplyWorker) deliverReply(ctx context.Context, evt domain.EventEnvelope) error {
	var payload domain.ReplyRecordedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	replyID := strings.TrimSpace(payload.ReplyID)
	if replyID == "" || strings.TrimSpace(payload.ReplyToEventID) == "" {
		return nil
	}
	delivered, err := w.service.state.Delivered(replyID)
	if err != nil {
		return err
	}
	if delivered {
		return nil
	}
	target, ok, err := w.service.state.Target(payload.ReplyToEventID)
	if err != nil {
		return err
	}
	if !ok || !target.Valid() {
		return nil
	}
	text := renderReplyText(payload.PayloadRef)
	remoteID, err := w.service.ReplyText(ctx, target, text, replyID)
	if err != nil {
		w.logger.Error("feishu_reply_delivery_failed", "reply_id", replyID, "event_id", payload.ReplyToEventID, "error", err.Error())
		return err
	}
	return w.service.state.MarkDelivered(replyID, remoteID, time.Now().UTC())
}

func renderReplyText(payloadRef string) string {
	payloadRef = strings.TrimSpace(payloadRef)
	switch {
	case strings.HasPrefix(payloadRef, "reply://"):
		return strings.TrimPrefix(payloadRef, "reply://")
	case strings.HasPrefix(payloadRef, "text://"):
		return strings.TrimPrefix(payloadRef, "text://")
	case payloadRef == "":
		return ""
	default:
		return payloadRef
	}
}
