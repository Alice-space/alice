package feishu

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"alice/internal/domain"
	"alice/internal/notifier"
	"alice/internal/platform"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	deliveryScopeApproval = "approval"
	deliveryScopeWait     = "wait"
)

// OutboundChannel consumes durable events and delivers them through Feishu.
type OutboundChannel struct {
	service           *Service
	logger            platform.Logger
	humanActionSecret []byte
}

var _ notifier.Channel = (*OutboundChannel)(nil)

func NewOutboundChannel(service *Service, humanActionSecret []byte, logger platform.Logger) *OutboundChannel {
	if logger == nil {
		logger = platform.NewNoopLogger()
	}
	return &OutboundChannel{
		service:           service,
		logger:            logger.WithComponent("feishu-notifier"),
		humanActionSecret: append([]byte(nil), humanActionSecret...),
	}
}

func NewReplyChannel(service *Service, logger platform.Logger) *OutboundChannel {
	return NewOutboundChannel(service, nil, logger)
}

func (c *OutboundChannel) Name() string { return "feishu" }

func (c *OutboundChannel) Enabled() bool {
	return c != nil && c.service != nil && c.service.Enabled()
}

func (c *OutboundChannel) Cursor() (string, error) {
	if c == nil || c.service == nil || c.service.state == nil {
		return "", nil
	}
	return c.service.state.Cursor()
}

func (c *OutboundChannel) SaveCursor(cursor string) error {
	if c == nil || c.service == nil || c.service.state == nil {
		return nil
	}
	return c.service.state.SaveCursor(cursor)
}

func (c *OutboundChannel) HandleEvent(ctx context.Context, evt domain.EventEnvelope) error {
	switch evt.EventType {
	case domain.EventTypeExternalEventIngested:
		return c.recordTarget(evt)
	case domain.EventTypeRequestPromoted:
		return c.recordTaskTarget(evt)
	case domain.EventTypeReplyRecorded:
		return c.deliverReply(ctx, evt)
	case domain.EventTypeApprovalRequestOpened:
		return c.deliverApprovalCard(ctx, evt)
	case domain.EventTypeHumanWaitRecorded:
		return c.deliverHumanWaitCard(ctx, evt)
	default:
		return nil
	}
}

func (c *OutboundChannel) recordTarget(evt domain.EventEnvelope) error {
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
	target := meta.ReplyTarget(c.service.cfg.ReplyInThread)
	if !target.Valid() {
		return nil
	}
	eventID := strings.TrimSpace(payload.Event.EventID)
	if err := c.service.state.SaveTarget(eventID, target); err != nil {
		return err
	}
	if evt.AggregateKind == domain.AggregateKindRequest && strings.HasPrefix(strings.TrimSpace(evt.AggregateID), domain.IDPrefixRequest) {
		return c.service.state.SaveRequestTarget(strings.TrimSpace(evt.AggregateID), target)
	}
	return nil
}

func (c *OutboundChannel) recordTaskTarget(evt domain.EventEnvelope) error {
	var payload domain.RequestPromotedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	target, ok, err := c.service.state.RequestTarget(payload.RequestID)
	if err != nil {
		return err
	}
	if !ok || !target.Valid() {
		return nil
	}
	return c.service.state.SaveTaskTarget(payload.TaskID, target)
}

func (c *OutboundChannel) deliverReply(ctx context.Context, evt domain.EventEnvelope) error {
	var payload domain.ReplyRecordedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	replyID := strings.TrimSpace(payload.ReplyID)
	if replyID == "" || strings.TrimSpace(payload.ReplyToEventID) == "" {
		return nil
	}
	delivered, err := c.service.state.Delivered(replyID)
	if err != nil {
		return err
	}
	if delivered {
		return nil
	}
	target, ok, err := c.service.state.Target(payload.ReplyToEventID)
	if err != nil {
		return err
	}
	if !ok || !target.Valid() {
		return nil
	}
	text := renderReplyText(payload.PayloadRef)
	remoteID, err := c.service.ReplyText(ctx, target, text, replyID)
	if err != nil {
		c.logger.Error("feishu_reply_delivery_failed", "reply_id", replyID, "event_id", payload.ReplyToEventID, "error", err.Error())
		return err
	}
	return c.service.state.MarkDelivered(replyID, remoteID, time.Now().UTC())
}

func (c *OutboundChannel) deliverApprovalCard(ctx context.Context, evt domain.EventEnvelope) error {
	if len(c.humanActionSecret) == 0 {
		return nil
	}
	var payload domain.ApprovalRequestOpenedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	deliveryID := deliveryKey(deliveryScopeApproval, payload.ApprovalRequestID)
	return c.deliverTaskScopedMessage(ctx, strings.TrimSpace(payload.TaskID), deliveryID, evt, func() (OutboundMessage, error) {
		return buildApprovalCardMessage(c.humanActionSecret, payload, evt.ProducedAt)
	})
}

func (c *OutboundChannel) deliverHumanWaitCard(ctx context.Context, evt domain.EventEnvelope) error {
	if len(c.humanActionSecret) == 0 {
		return nil
	}
	var payload domain.HumanWaitRecordedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return err
	}
	deliveryID := deliveryKey(deliveryScopeWait, payload.HumanWaitID)
	return c.deliverTaskScopedMessage(ctx, strings.TrimSpace(payload.TaskID), deliveryID, evt, func() (OutboundMessage, error) {
		return buildHumanWaitMessage(c.humanActionSecret, payload, evt.ProducedAt)
	})
}

func (c *OutboundChannel) deliverTaskScopedMessage(ctx context.Context, taskID, deliveryID string, evt domain.EventEnvelope, build func() (OutboundMessage, error)) error {
	if taskID == "" || deliveryID == "" {
		return nil
	}
	delivered, err := c.service.state.Delivered(deliveryID)
	if err != nil {
		return err
	}
	if delivered {
		return nil
	}
	target, ok, err := c.service.state.TaskTarget(taskID)
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
	remoteID, err := c.sendOutboundMessage(ctx, target, msg, deliveryID)
	if err != nil {
		c.logger.Error("feishu_delivery_failed", "delivery_id", deliveryID, "task_id", taskID, "event_type", evt.EventType, "error", err.Error())
		return err
	}
	return c.service.state.MarkDelivered(deliveryID, remoteID, time.Now().UTC())
}

func (c *OutboundChannel) sendOutboundMessage(ctx context.Context, target ReplyTarget, msg OutboundMessage, deliveryID string) (string, error) {
	switch strings.TrimSpace(msg.MsgType) {
	case larkim.MsgTypeInteractive:
		return c.service.ReplyCard(ctx, target, msg.Content, deliveryID)
	case "", larkim.MsgTypeText:
		return c.service.ReplyText(ctx, target, renderReplyText(msg.Content), deliveryID)
	default:
		return c.service.replyMessage(ctx, target, msg.MsgType, msg.Content, deliveryID)
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
