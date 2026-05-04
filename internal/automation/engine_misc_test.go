package automation

import (
	"strings"
	"testing"
	"time"
)

func TestEngine_SetUserTaskTimeout_NonPositiveFallsBackToDefault(t *testing.T) {
	engine := NewEngine(nil, nil)
	engine.SetUserTaskTimeout(0)
	if got := engine.userTaskTimeoutDuration(); got != defaultUserTaskTimeout {
		t.Fatalf("unexpected default timeout: %s", got)
	}
}

func TestRenderActionTemplate_InvalidTemplateReturnsError(t *testing.T) {
	_, err := renderActionTemplate("{{ if }}", time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected renderActionTemplate to return error for invalid template")
	}
	if !strings.Contains(err.Error(), "render action template failed") {
		t.Fatalf("unexpected template error: %v", err)
	}
}

func TestRenderActionTemplate_EmptyInputReturnsEmpty(t *testing.T) {
	got, err := renderActionTemplate("   ", time.Time{})
	if err != nil {
		t.Fatalf("expected nil error for empty template, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty rendered text, got %q", got)
	}
}

func TestEngine_BuildTaskRunEnv_SourceMessageIDRouteUsesScopeID(t *testing.T) {
	engine := NewEngine(nil, nil)
	task := Task{
		Scope:      Scope{Kind: ScopeKindChat, ID: "oc_chatid"},
		Route:      Route{ReceiveIDType: "source_message_id", ReceiveID: "om_messageid"},
		Creator:    Actor{OpenID: "ou_actor"},
		Schedule:   Schedule{EverySeconds: 60},
		Prompt:     "test",
		SessionKey: "chat_id:oc_chatid|scene:work|seed:om_messageid",
	}
	env := engine.buildTaskRunEnv(task)
	if env == nil {
		t.Fatal("expected non-nil env")
	}
	gotType := env["ALICE_RECEIVE_ID_TYPE"]
	gotID := env["ALICE_RECEIVE_ID"]
	if gotType != "chat_id" {
		t.Errorf("ALICE_RECEIVE_ID_TYPE: got %q, want %q", gotType, "chat_id")
	}
	if gotID != "oc_chatid" {
		t.Errorf("ALICE_RECEIVE_ID: got %q, want %q (must be chat_id, not message ID)", gotID, "oc_chatid")
	}
}

func TestEngine_BuildTaskRunEnv_ChatIDRoutePassedThrough(t *testing.T) {
	engine := NewEngine(nil, nil)
	task := Task{
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_chatid"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chatid"},
		Creator:  Actor{OpenID: "ou_actor"},
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "test",
	}
	env := engine.buildTaskRunEnv(task)
	if env["ALICE_RECEIVE_ID_TYPE"] != "chat_id" {
		t.Errorf("unexpected ALICE_RECEIVE_ID_TYPE: %s", env["ALICE_RECEIVE_ID_TYPE"])
	}
	if env["ALICE_RECEIVE_ID"] != "oc_chatid" {
		t.Errorf("unexpected ALICE_RECEIVE_ID: %s", env["ALICE_RECEIVE_ID"])
	}
}
