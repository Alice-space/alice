package feishu

import (
	"encoding/json"
	"testing"
	"time"

	"alice/internal/domain"
)

func TestBuildHumanWaitMessageIncludesTextareaAndProvideInputToken(t *testing.T) {
	secret := []byte("form-secret")
	msg, err := buildHumanWaitMessage(secret, domain.HumanWaitRecordedPayload{
		HumanWaitID:     "wait_input_1",
		TaskID:          "task_input_1",
		StepExecutionID: "exec_input_1",
		WaitingReason:   string(domain.WaitingReasonInput),
		InputSchemaID:   "recovery.schedule_trigger",
		InputDraft:      json.RawMessage(`{"reason":"Need operator confirmation"}`),
		ResumeOptions:   []string{string(domain.HumanActionProvideInput), string(domain.HumanActionCancel)},
		PromptRef:       "prompt:collect-reason",
		DeadlineAt:      time.Date(2026, 3, 13, 15, 0, 0, 0, time.UTC),
	}, time.Date(2026, 3, 13, 14, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if msg.MsgType != "interactive" {
		t.Fatalf("unexpected message type: %s", msg.MsgType)
	}

	var card map[string]any
	if err := json.Unmarshal([]byte(msg.Content), &card); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	elements, ok := card["elements"].([]any)
	if !ok {
		t.Fatalf("card elements missing: %v", card)
	}

	var foundTextarea bool
	var provideInputValue map[string]any
	for _, element := range elements {
		elemMap, ok := element.(map[string]any)
		if !ok {
			continue
		}
		switch elemMap["tag"] {
		case "textarea":
			if elemMap["name"] == "reason" {
				foundTextarea = true
			}
		case "action":
			actions, _ := elemMap["actions"].([]any)
			for _, action := range actions {
				actionMap, ok := action.(map[string]any)
				if !ok {
					continue
				}
				value, _ := actionMap["value"].(map[string]any)
				if value != nil && value["action_kind"] == string(domain.HumanActionProvideInput) {
					provideInputValue = value
				}
			}
		}
	}
	if !foundTextarea {
		t.Fatalf("textarea field not found in card: %s", msg.Content)
	}
	if provideInputValue == nil {
		t.Fatalf("provide_input action not found in card: %s", msg.Content)
	}

	token, _ := provideInputValue[ActionTokenValueKey].(string)
	claims, err := domain.VerifyHumanActionTokenV1(secret, token, time.Date(2026, 3, 13, 14, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims.ActionKind != string(domain.HumanActionProvideInput) {
		t.Fatalf("unexpected action kind: %s", claims.ActionKind)
	}
	if claims.WaitingReason != string(domain.WaitingReasonInput) {
		t.Fatalf("unexpected waiting reason: %s", claims.WaitingReason)
	}
	if claims.HumanWaitID != "wait_input_1" || claims.TaskID != "task_input_1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}
