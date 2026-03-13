package ingress

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/feishu"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers routes on a gin RouterGroup.
func (h *HTTPIngress) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/ingress/cli/messages", h.handleCLIMessage)
	if h.feishu != nil && h.feishu.Enabled() {
		rg.POST("/ingress/im/feishu", h.handleFeishuMessage())
		rg.POST("/ingress/im/feishu/cards", h.handleFeishuCardAction())
	} else {
		rg.POST("/ingress/im/feishu", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "feishu is not configured"})
		})
		rg.POST("/ingress/im/feishu/cards", func(c *gin.Context) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "feishu is not configured"})
		})
	}
	rg.POST("/ingress/web/messages", h.handleIngress("direct_input", "web"))
	rg.POST("/webhooks/github", h.handleWebhook("repo_comment", "github"))
	rg.POST("/webhooks/gitlab", h.handleWebhook("repo_comment", "gitlab"))
	rg.POST("/human-actions/*token", h.handleHumanAction)
	rg.POST("/scheduler/fires", h.handleSchedulerFire)
}

func (h *HTTPIngress) handleFeishuMessage() gin.HandlerFunc {
	return h.feishu.EventWebhookHandler(func(ctx context.Context, in feishu.InboundMessage) error {
		_, err := h.ingestNormalizedEvent(ctx, h.normalizeFeishuMessage(in))
		return err
	})
}

func (h *HTTPIngress) handleFeishuCardAction() gin.HandlerFunc {
	return h.feishu.CardActionWebhookHandler(func(ctx context.Context, action feishu.CardAction) (*feishu.CardActionResult, error) {
		token := strings.TrimSpace(action.HumanActionToken())
		if token == "" {
			return &feishu.CardActionResult{ToastType: "error", ToastContent: "missing human action token"}, nil
		}
		_, err := h.ingestHumanAction(ctx, token, h.normalizeFeishuCardAction(action), feishu.CardActionTransportKind)
		if err != nil {
			return &feishu.CardActionResult{ToastType: "error", ToastContent: err.Error()}, nil
		}
		return &feishu.CardActionResult{ToastType: "success", ToastContent: "submitted"}, nil
	})
}

func (h *HTTPIngress) handleIngress(sourceKind, transportKind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in NormalizedEvent
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		in = h.sanitizeIngressInput(sourceKind, transportKind, in)
		result, err := h.ingestNormalizedEvent(c.Request.Context(), in)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, writeAcceptedFromResult(result, ""))
	}
}

func (h *HTTPIngress) handleCLIMessage(c *gin.Context) {
	var in domain.CLIMessageSubmitRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text is required"})
		return
	}

	threadID := strings.TrimSpace(in.ThreadID)
	if threadID == "" {
		threadID = "root"
	}
	sourceRef := strings.TrimSpace(in.SourceRef)
	if sourceRef == "" {
		sourceRef = strings.TrimSpace(in.Text)
	}

	result, err := h.ingestNormalizedEvent(c.Request.Context(), NormalizedEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "direct_input",
		TransportKind:  "cli",
		SourceRef:      sourceRef,
		ActorRef:       strings.TrimSpace(in.ActorRef),
		ConversationID: strings.TrimSpace(in.ConversationID),
		ThreadID:       threadID,
		RepoRef:        strings.TrimSpace(in.RepoRef),
		ReplyToEventID: strings.TrimSpace(in.ReplyToEventID),
		IdempotencyKey: strings.TrimSpace(in.IdempotencyKey),
		TraceID:        strings.TrimSpace(in.TraceID),
		PayloadRef:     "cli-message:text",
		Verified:       true,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, writeAcceptedFromResult(result, ""))
}

func (h *HTTPIngress) handleSchedulerFire(c *gin.Context) {
	if err := h.authorizeSchedulerFire(c); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	var in SchedulerFireRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.ScheduledTaskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scheduled_task_id is required"})
		return
	}

	if _, err := h.runtime.RequireEnabledScheduleSource(c.Request.Context(), in.ScheduledTaskID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	window := in.ScheduledForWindow.UTC()
	if window.IsZero() {
		window = time.Now().UTC().Truncate(time.Minute)
	}

	payload, err := h.runtime.RecordScheduleFire(c.Request.Context(), domain.RecordScheduleFireCommand{
		ScheduledTaskID:       in.ScheduledTaskID,
		ScheduledForWindowUTC: window,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, payload)
}

func (h *HTTPIngress) authorizeSchedulerFire(c *gin.Context) error {
	secret := strings.TrimSpace(h.schedulerSecret)
	if secret == "" {
		return domain.ErrUnauthorized
	}

	token := strings.TrimSpace(c.GetHeader("X-Scheduler-Token"))
	if token == "" {
		auth := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			token = strings.TrimSpace(auth[len("Bearer "):])
		}
	}

	if token == "" || token != secret {
		return domain.ErrUnauthorized
	}
	return nil
}

func (h *HTTPIngress) sanitizeIngressInput(sourceKind, transportKind string, in NormalizedEvent) NormalizedEvent {
	in.SourceKind = sourceKind
	in.TransportKind = transportKind
	switch sourceKind {
	case "direct_input":
		in.EventType = domain.EventTypeExternalEventIngested
		in.RequestID = ""
		in.TaskID = ""
		in.ActionKind = ""
		in.ApprovalRequestID = ""
		in.HumanWaitID = ""
		in.StepExecutionID = ""
		in.TargetStepID = ""
		in.WaitingReason = ""
		in.DecisionHash = ""
		in.Verified = false
	}
	return in
}

func (h *HTTPIngress) normalizeFeishuMessage(in feishu.InboundMessage) NormalizedEvent {
	sourceRef := strings.TrimSpace(in.Text)
	if sourceRef == "" {
		sourceRef = strings.TrimSpace(in.Metadata.RawContent)
	}
	return NormalizedEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "direct_input",
		TransportKind:  feishu.TransportKind,
		SourceRef:      sourceRef,
		ActorRef:       in.Metadata.ActorRef(),
		ConversationID: in.Metadata.ConversationID(),
		ThreadID:       in.Metadata.ThreadKey(),
		PayloadRef:     "feishu-message:" + strings.TrimSpace(in.Metadata.MessageType),
		Verified:       true,
		IdempotencyKey: in.Metadata.IdempotencyKey(),
		InputSchemaID:  feishu.MessageInputSchemaID,
		InputPatch:     feishu.EncodeMetadataPatch(in.Metadata),
	}
}

func (h *HTTPIngress) normalizeFeishuCardAction(action feishu.CardAction) NormalizedEvent {
	return NormalizedEvent{
		EventType:      domain.EventTypeExternalEventIngested,
		SourceKind:     "human_action",
		TransportKind:  feishu.CardActionTransportKind,
		SourceRef:      action.SourceRef(),
		ActorRef:       action.ActorRef(),
		ReplyToEventID: stringFromMap(action.Value, "reply_to_event_id"),
		DecisionHash:   stringFromMap(action.Value, "decision_hash"),
		ActionKind:     stringFromMap(action.Value, "action_kind"),
		TraceID:        stringFromMap(action.Value, "trace_id"),
		InputSchemaID:  chooseNonEmpty(stringFromMap(action.Value, "input_schema_id"), feishu.CardActionInputSchemaID),
		InputPatch:     action.InputPatch(),
	}
}

func (h *HTTPIngress) ingestNormalizedEvent(ctx context.Context, in NormalizedEvent) (*bus.ProcessResult, error) {
	evt := toExternalEvent(in)
	return h.runtime.IngestExternalEvent(ctx, evt, h.reception)
}

func stringFromMap(m map[string]interface{}, key string) string {
	if len(m) == 0 {
		return ""
	}
	return strings.TrimSpace(feishuString(m[key]))
}

func feishuString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		raw, _ := json.Marshal(x)
		return string(raw)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func toExternalEvent(in NormalizedEvent) domain.ExternalEvent {
	return domain.ExternalEvent{
		EventType:         in.EventType,
		SourceKind:        in.SourceKind,
		TransportKind:     in.TransportKind,
		SourceRef:         in.SourceRef,
		ActorRef:          in.ActorRef,
		ActionKind:        string(domain.NormalizeHumanActionKind(in.ActionKind)),
		RequestID:         in.RequestID,
		TaskID:            in.TaskID,
		ApprovalRequestID: in.ApprovalRequestID,
		HumanWaitID:       in.HumanWaitID,
		StepExecutionID:   in.StepExecutionID,
		TargetStepID:      in.TargetStepID,
		WaitingReason:     in.WaitingReason,
		DecisionHash:      in.DecisionHash,
		ReplyToEventID:    in.ReplyToEventID,
		ConversationID:    in.ConversationID,
		ThreadID:          in.ThreadID,
		RepoRef:           in.RepoRef,
		IssueRef:          in.IssueRef,
		PRRef:             in.PRRef,
		CommentRef:        in.CommentRef,
		ScheduledTaskID:   in.ScheduledTaskID,
		ControlObjectRef:  in.ControlObjectRef,
		WorkflowObjectRef: in.WorkflowObjectRef,
		CoalescingKey:     in.CoalescingKey,
		Verified:          in.Verified,
		PayloadRef:        in.PayloadRef,
		IdempotencyKey:    in.IdempotencyKey,
		TraceID:           in.TraceID,
		InputSchemaID:     in.InputSchemaID,
		InputPatch:        in.InputPatch,
		ReceivedAt:        time.Now().UTC(),
	}
}

func writeAcceptedFromResult(result *bus.ProcessResult, adminActionID string) domain.WriteAcceptedResponse {
	resp := domain.WriteAcceptedResponse{
		Accepted:      true,
		AdminActionID: strings.TrimSpace(adminActionID),
	}
	if result == nil {
		return resp
	}
	resp.EventID = strings.TrimSpace(result.EventID)
	resp.RequestID = strings.TrimSpace(result.RequestID)
	resp.TaskID = strings.TrimSpace(result.TaskID)
	resp.RouteTargetKind = strings.TrimSpace(result.RouteTargetKind)
	resp.RouteTargetID = strings.TrimSpace(result.RouteTargetID)
	if resp.RouteTargetKind == "" {
		if resp.TaskID != "" {
			resp.RouteTargetKind = string(domain.RouteTargetTask)
			resp.RouteTargetID = resp.TaskID
		} else if resp.RequestID != "" {
			resp.RouteTargetKind = string(domain.RouteTargetRequest)
			resp.RouteTargetID = resp.RequestID
		}
	}
	resp.CommitHLC = strings.TrimSpace(result.CommitHLC)
	return resp
}
