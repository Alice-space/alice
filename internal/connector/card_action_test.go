package connector

import (
	"context"
	"testing"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	"github.com/Alice-space/alice/internal/config"
)

type cardActionHandlerStub struct {
	calls   int
	lastReq CardActionRequest
	result  CardActionResult
	err     error
}

func (s *cardActionHandlerStub) HandleCardAction(_ context.Context, req CardActionRequest) (CardActionResult, error) {
	s.calls++
	s.lastReq = req
	return s.result, s.err
}

func TestApp_OnCardActionTrigger_DelegatesToHandler(t *testing.T) {
	app := NewApp(config.Config{}, nil)
	handler := &cardActionHandlerStub{
		result: CardActionResult{
			ToastType: "success",
			Toast:     "已批准",
		},
	}
	app.SetCardActionHandler(handler)

	userID := "u_actor"
	resp, err := app.onCardActionTrigger(context.Background(), &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{
				OpenID: "ou_actor",
				UserID: &userID,
			},
			Action: &larkcallback.CallBackAction{
				Value: map[string]any{
					"alice_action": "plan_approval",
					"campaign_id":  "camp_demo",
					"plan_round":   2,
					"decision":     "approve",
				},
			},
			Context: &larkcallback.Context{
				OpenMessageID: "om_card",
			},
		},
	})
	if err != nil {
		t.Fatalf("onCardActionTrigger returned error: %v", err)
	}
	if handler.calls != 1 {
		t.Fatalf("expected handler call, got %d", handler.calls)
	}
	if handler.lastReq.Kind != "plan_approval" {
		t.Fatalf("unexpected kind: %q", handler.lastReq.Kind)
	}
	if handler.lastReq.CampaignID != "camp_demo" {
		t.Fatalf("unexpected campaign id: %q", handler.lastReq.CampaignID)
	}
	if handler.lastReq.PlanRound != 2 {
		t.Fatalf("unexpected plan round: %d", handler.lastReq.PlanRound)
	}
	if handler.lastReq.Decision != CardActionDecisionApprove {
		t.Fatalf("unexpected decision: %q", handler.lastReq.Decision)
	}
	if handler.lastReq.ActorOpenID != "ou_actor" || handler.lastReq.ActorUserID != "u_actor" {
		t.Fatalf("unexpected actor ids: %+v", handler.lastReq)
	}
	if handler.lastReq.OpenMessageID != "om_card" {
		t.Fatalf("unexpected open message id: %q", handler.lastReq.OpenMessageID)
	}
	if resp == nil || resp.Toast == nil || resp.Toast.Content != "已批准" {
		t.Fatalf("unexpected toast response: %#v", resp)
	}
}

func TestApp_OnCardActionTrigger_InvalidPayloadReturnsErrorToast(t *testing.T) {
	app := NewApp(config.Config{}, nil)

	resp, err := app.onCardActionTrigger(context.Background(), &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Action: &larkcallback.CallBackAction{
				Value: map[string]any{
					"campaign_id": "camp_demo",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("onCardActionTrigger returned error: %v", err)
	}
	if resp == nil || resp.Toast == nil {
		t.Fatalf("expected error toast, got %#v", resp)
	}
	if resp.Toast.Type != "error" {
		t.Fatalf("expected error toast type, got %#v", resp.Toast)
	}
}
