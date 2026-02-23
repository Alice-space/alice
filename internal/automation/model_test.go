package automation

import "testing"

func TestBuildDispatchText(t *testing.T) {
	text, err := BuildDispatchText(Action{
		Type:           ActionTypeSendText,
		Text:           "请处理",
		MentionUserIDs: []string{"ou_1", "ou_2", "ou_1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `<at user_id="ou_1">ou_1</at> <at user_id="ou_2">ou_2</at> 请处理`
	if text != want {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestBuildDispatchText_EmptyRejected(t *testing.T) {
	if _, err := BuildDispatchText(Action{}); err == nil {
		t.Fatal("expected empty action error")
	}
}

func TestParseStatusFilter(t *testing.T) {
	status, all, err := ParseStatusFilter("active")
	if err != nil || all || status != TaskStatusActive {
		t.Fatalf("unexpected active parse result status=%s all=%t err=%v", status, all, err)
	}
	status, all, err = ParseStatusFilter("all")
	if err != nil || !all || status != "" {
		t.Fatalf("unexpected all parse result status=%s all=%t err=%v", status, all, err)
	}
	if _, _, err := ParseStatusFilter("x"); err == nil {
		t.Fatal("expected invalid status filter error")
	}
}

func TestValidateTask_RunLLM(t *testing.T) {
	task := Task{
		ID:       "task_run_llm",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type:           ActionTypeRunLLM,
			Prompt:         "请输出当前时间 {{now}}",
			MentionUserIDs: []string{"ou_actor"},
		},
		Status: TaskStatusActive,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected run_llm task to be valid, got err=%v", err)
	}
}

func TestValidateTask_RunLLMEmptyPromptRejected(t *testing.T) {
	task := Task{
		ID:       "task_run_llm_empty_prompt",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type:   ActionTypeRunLLM,
			Prompt: "",
		},
		Status: TaskStatusActive,
	}
	if err := ValidateTask(task); err == nil {
		t.Fatal("expected empty run_llm prompt error")
	}
}
