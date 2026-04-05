package connector

import (
	"context"
	"strconv"
	"strings"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	"github.com/Alice-space/alice/internal/logging"
)

const (
	CardActionDecisionApprove = "approve"
	CardActionDecisionReject  = "reject"
)

type CardActionRequest struct {
	Kind          string
	CampaignID    string
	PlanRound     int
	Decision      string
	ActorOpenID   string
	ActorUserID   string
	OpenMessageID string
}

type CardActionResult struct {
	Toast     string
	ToastType string
}

type CardActionHandler interface {
	HandleCardAction(ctx context.Context, req CardActionRequest) (CardActionResult, error)
}

func (a *App) SetCardActionHandler(handler CardActionHandler) {
	if a == nil {
		return
	}
	a.cardActionMu.Lock()
	a.cardAction = handler
	a.cardActionMu.Unlock()
}

func (a *App) cardActionHandlerValue() CardActionHandler {
	if a == nil {
		return nil
	}
	a.cardActionMu.RLock()
	defer a.cardActionMu.RUnlock()
	return a.cardAction
}

func (a *App) onCardActionTrigger(ctx context.Context, event *larkcallback.CardActionTriggerEvent) (*larkcallback.CardActionTriggerResponse, error) {
	req, err := buildCardActionRequest(event)
	if err != nil {
		logging.Warnf("invalid card action event: %v", err)
		return cardActionToastResponse("error", "卡片动作无效，请刷新后重试。"), nil
	}
	handler := a.cardActionHandlerValue()
	if handler == nil {
		return cardActionToastResponse("error", "当前实例未启用卡片动作处理。"), nil
	}
	result, err := handler.HandleCardAction(ctx, req)
	if err != nil {
		logging.Warnf("handle card action failed kind=%s decision=%s err=%v", req.Kind, req.Decision, err)
		return cardActionToastResponse("error", err.Error()), nil
	}
	return cardActionToastResponse(result.ToastType, result.Toast), nil
}

func buildCardActionRequest(event *larkcallback.CardActionTriggerEvent) (CardActionRequest, error) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return CardActionRequest{}, ErrIgnoreMessage
	}
	value := event.Event.Action.Value
	req := CardActionRequest{
		Kind:       strings.TrimSpace(valueString(value, "alice_action")),
		CampaignID: strings.TrimSpace(valueString(value, "campaign_id")),
		PlanRound:  valueInt(value, "plan_round"),
		Decision:   strings.ToLower(strings.TrimSpace(valueString(value, "decision"))),
	}
	if event.Event.Context != nil {
		req.OpenMessageID = strings.TrimSpace(event.Event.Context.OpenMessageID)
	}
	if event.Event.Operator != nil {
		req.ActorOpenID = strings.TrimSpace(event.Event.Operator.OpenID)
		if event.Event.Operator.UserID != nil {
			req.ActorUserID = strings.TrimSpace(*event.Event.Operator.UserID)
		}
	}
	if req.Kind == "" || req.CampaignID == "" || req.Decision == "" {
		return CardActionRequest{}, ErrIgnoreMessage
	}
	return req, nil
}

func cardActionToastResponse(toastType, content string) *larkcallback.CardActionTriggerResponse {
	content = strings.TrimSpace(content)
	if content == "" {
		content = "操作已处理。"
	}
	return &larkcallback.CardActionTriggerResponse{
		Toast: &larkcallback.Toast{
			Type:    normalizeCardActionToastType(toastType),
			Content: content,
		},
	}
}

func normalizeCardActionToastType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "success", "warning", "error", "info":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "info"
	}
}

func valueString(value map[string]any, key string) string {
	if len(value) == 0 {
		return ""
	}
	raw, ok := value[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func valueInt(value map[string]any, key string) int {
	raw := strings.TrimSpace(valueString(value, key))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}
