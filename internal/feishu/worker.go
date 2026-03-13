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

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	deliveryScopeApproval = "approval"
	deliveryScopeWait     = "wait"
)

// OutboundWorker tails the event log and delivers Feishu replies and cards.
type OutboundWorker struct {
	store             *storepkg.Store
	service           *Service
	logger            platform.Logger
	interval          time.Duration
	humanActionSecret []byte
}

// ReplyWorker is kept as an alias for backward compatibility.
type ReplyWorker = OutboundWorker

func NewOutboundWorker(store *storepkg.Store, service *Service, humanActionSecret []byte, logger platform.Logger) *OutboundWorker {
	if logger == nil {
		logger = platform.NewNoopLogger()
	}
	return &OutboundWorker{
		store:             store,
		service:           service,
		logger:            logger.WithComponent("feishu-outbound-worker"),
		interval:          5 * time.Second,
		humanActionSecret: append([]byte(nil), humanActionSecret...),
	}
}

func NewReplyWorker(store *storepkg.Store, service *Service, logger platform.Logger) *OutboundWorker {
	return NewOutboundWorker(store, service, nil, logger)
}

func (w *OutboundWorker) Name() string { return "feishu-outbound-worker" }

func (w *OutboundWorker) Recover(ctx context.Context) error {
	return w.syncOnce(ctx)
}

func (w *OutboundWorker) Start(ctx context.Context) error {
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

func (w *OutboundWorker) syncOnce(ctx context.Context) error {
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

func (w *OutboundWorker) processEvent(ctx context.Context, evt domain.EventEnvelope) error {
	switch evt.EventType {
	case domain.EventTypeExternalEventIngested:
		return w.recordTarget(evt)
	case domain.EventTypeRequestPromoted:
		return w.recordTaskTarget(evt)
	case domain.EventTypeReplyRecorded:
		return w.deliverReply(ctx, evt)
	case domain.EventTypeApprovalRequestOpened:
		return w.deliverApprovalCard(ctx, evt)
	case domain.EventTypeHumanWaitRecorded:
		return w.deliverHumanWaitCard(ctx, evt)
	default:
		return nil
	}
}

func (w *OutboundWorker) recordTarget(evt domain.EventEnvelope) error {
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
	eventID := strings.TrimSpace(payload.Event.EventID)
	if err := w.service.state.SaveTarget(eventID, target); err != nil {
		return err
	}
	if evt.AggregateKind == domain.AggregateKindRequest && strings.HasPrefix(strings.TrimSpace(evt.AggregateID), domain.IDPrefixRequest) {
		return w.service.state.SaveRequestTarget(strings.TrimSpace(evt.AggregateID), target)
	}
	return nil
}

func (w *OutboundWorker) recordTaskTarget(evt domain.EventEnvelope) error {
	var payload domain.RequestPromotedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	target, ok, err := w.service.state.RequestTarget(payload.RequestID)
	if err != nil {
		return err
	}
	if !ok || !target.Valid() {
		return nil
	}
	return w.service.state.SaveTaskTarget(payload.TaskID, target)
}

func (w *OutboundWorker) deliverReply(ctx context.Context, evt domain.EventEnvelope) error {
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

func (w *OutboundWorker) deliverApprovalCard(ctx context.Context, evt domain.EventEnvelope) error {
	if len(w.humanActionSecret) == 0 {
		return nil
	}
	var payload domain.ApprovalRequestOpenedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	deliveryID := deliveryKey(deliveryScopeApproval, payload.ApprovalRequestID)
	return w.deliverTaskScopedMessage(ctx, strings.TrimSpace(payload.TaskID), deliveryID, evt, func() (OutboundMessage, error) {
		return buildApprovalCardMessage(w.humanActionSecret, payload, evt.ProducedAt)
	})
}

func (w *OutboundWorker) deliverHumanWaitCard(ctx context.Context, evt domain.EventEnvelope) error {
	if len(w.humanActionSecret) == 0 {
		return nil
	}
	var payload domain.HumanWaitRecordedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	deliveryID := deliveryKey(deliveryScopeWait, payload.HumanWaitID)
	return w.deliverTaskScopedMessage(ctx, strings.TrimSpace(payload.TaskID), deliveryID, evt, func() (OutboundMessage, error) {
		return buildHumanWaitMessage(w.humanActionSecret, payload, evt.ProducedAt)
	})
}

func (w *OutboundWorker) deliverTaskScopedMessage(ctx context.Context, taskID, deliveryID string, evt domain.EventEnvelope, build func() (OutboundMessage, error)) error {
	if taskID == "" || deliveryID == "" {
		return nil
	}
	delivered, err := w.service.state.Delivered(deliveryID)
	if err != nil {
		return err
	}
	if delivered {
		return nil
	}
	target, ok, err := w.service.state.TaskTarget(taskID)
	if err != nil {
		return err
	}
	if !ok || !target.Valid() {
		return nil
	}
	msg, err := build()
	if err != nil {
		return err
	}
	remoteID, err := w.sendOutboundMessage(ctx, target, msg, deliveryID)
	if err != nil {
		w.logger.Error("feishu_delivery_failed", "delivery_id", deliveryID, "task_id", taskID, "event_type", evt.EventType, "error", err.Error())
		return err
	}
	return w.service.state.MarkDelivered(deliveryID, remoteID, time.Now().UTC())
}

func (w *OutboundWorker) sendOutboundMessage(ctx context.Context, target ReplyTarget, msg OutboundMessage, deliveryID string) (string, error) {
	switch strings.TrimSpace(msg.MsgType) {
	case larkim.MsgTypeInteractive:
		return w.service.ReplyCard(ctx, target, msg.Content, deliveryID)
	case "", larkim.MsgTypeText:
		return w.service.ReplyText(ctx, target, renderReplyText(msg.Content), deliveryID)
	default:
		return w.service.replyMessage(ctx, target, msg.MsgType, msg.Content, deliveryID)
	}
}

func deliveryKey(scope, id string) string {
	scope = strings.TrimSpace(scope)
	id = strings.TrimSpace(id)
	if scope == "" || id == "" {
		return ""
	}
	return scope + ":" + id
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
