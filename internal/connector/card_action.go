package connector

import (
	"context"
	"strconv"
	"strings"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	llm "github.com/Alice-space/alice/internal/llm"
	"github.com/Alice-space/alice/internal/logging"
)

const (
	CardActionDecisionApprove = "approve"
	CardActionDecisionReject  = "reject"
	CardActionKindQuestion    = "question"
)

type CardActionRequest struct {
	Kind              string
	CampaignID        string
	PlanRound         int
	Decision          string
	ActorOpenID       string
	ActorUserID       string
	OpenMessageID     string
	QuestionRequestID string
	QuestionCount     int
	FormValue         map[string]any
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
	if req.Kind == CardActionKindQuestion {
		return a.handleQuestionCardAction(ctx, req)
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

func (a *App) handleQuestionCardAction(ctx context.Context, req CardActionRequest) (*larkcallback.CardActionTriggerResponse, error) {
	requestID := strings.TrimSpace(req.QuestionRequestID)
	if requestID == "" {
		return cardActionToastResponse("error", "请求ID缺失。"), nil
	}
	qs, ok := loadQuestionState(requestID)
	if !ok {
		return cardActionToastResponse("error", "该问题已过期，请刷新后重试。"), nil
	}
	answers, err := parseQuestionAnswers(req.FormValue, qs.Questions)
	if err != nil {
		return cardActionToastResponse("warning", err.Error()), nil
	}
	if err := llm.ReplyQuestion(ctx, requestID, answers); err != nil {
		logging.Warnf("reply question failed requestID=%s: %v", requestID, err)
		return cardActionToastResponse("error", "提交答案失败，请重试。"), nil
	}
	pendingQuestionStates.Delete(requestID)
	a.patchQuestionCardAnswered(ctx, req.OpenMessageID)
	return cardActionToastResponse("success", "答案已提交。"), nil
}

func buildCardActionRequest(event *larkcallback.CardActionTriggerEvent) (CardActionRequest, error) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return CardActionRequest{}, ErrIgnoreMessage
	}
	value := event.Event.Action.Value
	req := CardActionRequest{
		Kind:              strings.TrimSpace(valueString(value, "alice_action")),
		CampaignID:        strings.TrimSpace(valueString(value, "campaign_id")),
		PlanRound:         valueInt(value, "plan_round"),
		Decision:          strings.ToLower(strings.TrimSpace(valueString(value, "decision"))),
		QuestionRequestID: strings.TrimSpace(valueString(value, "request_id")),
		QuestionCount:     valueInt(value, "question_count"),
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
	if req.Kind == "" {
		return CardActionRequest{}, ErrIgnoreMessage
	}
	if req.Kind == CardActionKindQuestion {
		if req.QuestionRequestID == "" {
			return CardActionRequest{}, ErrIgnoreMessage
		}
		req.FormValue = event.Event.Action.FormValue
		return req, nil
	}
	if req.CampaignID == "" || req.Decision == "" {
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
