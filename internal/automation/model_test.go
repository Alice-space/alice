package automation

import (
	"testing"
	"time"
)

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
			Type:            ActionTypeRunLLM,
			Prompt:          "请输出当前时间 {{now}}",
			Model:           "gpt-4.1-mini",
			Profile:         "worker-cheap",
			ReasoningEffort: "high",
			Personality:     "pragmatic",
			MentionUserIDs:  []string{"ou_actor"},
		},
		Status: TaskStatusActive,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected run_llm task to be valid, got err=%v", err)
	}
}

func TestNormalizeTask_TrimRunLLMSelectors(t *testing.T) {
	task := NormalizeTask(Task{
		Action: Action{
			Type:            ActionTypeRunLLM,
			Prompt:          "hi",
			Model:           "  gpt-4.1-mini  ",
			Profile:         "  worker-cheap  ",
			ReasoningEffort: "  XHIGH  ",
			Personality:     "  Pragmatic  ",
		},
	})
	if task.Action.Model != "gpt-4.1-mini" {
		t.Fatalf("unexpected normalized model: %q", task.Action.Model)
	}
	if task.Action.Profile != "worker-cheap" {
		t.Fatalf("unexpected normalized profile: %q", task.Action.Profile)
	}
	if task.Action.ReasoningEffort != "xhigh" {
		t.Fatalf("unexpected normalized reasoning effort: %q", task.Action.ReasoningEffort)
	}
	if task.Action.Personality != "pragmatic" {
		t.Fatalf("unexpected normalized personality: %q", task.Action.Personality)
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

func TestValidateTask_RunWorkflow(t *testing.T) {
	task := Task{
		ID:       "task_run_workflow",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type:            ActionTypeRunWorkflow,
			Prompt:          "请推进当前 campaign",
			Workflow:        " code_army ",
			StateKey:        " fm16 ",
			SessionKey:      " chat_id:oc_chat|thread:omt_1 ",
			Model:           "gpt-5.4",
			Profile:         "worker-cheap",
			ReasoningEffort: "high",
			Personality:     "pragmatic",
			MentionUserIDs:  []string{"ou_actor"},
		},
		Status: TaskStatusActive,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected run_workflow task to be valid, got err=%v", err)
	}
	normalized := NormalizeTask(task)
	if normalized.Action.Workflow != "code_army" {
		t.Fatalf("unexpected normalized workflow: %q", normalized.Action.Workflow)
	}
	if normalized.Action.StateKey != "fm16" {
		t.Fatalf("unexpected normalized state key: %q", normalized.Action.StateKey)
	}
	if normalized.Action.SessionKey != "chat_id:oc_chat|thread:omt_1" {
		t.Fatalf("unexpected normalized session key: %q", normalized.Action.SessionKey)
	}
}

func TestValidateTask_RunWorkflowEmptyWorkflowRejected(t *testing.T) {
	task := Task{
		ID:       "task_run_workflow_empty_workflow",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type:   ActionTypeRunWorkflow,
			Prompt: "请推进当前 campaign",
		},
		Status: TaskStatusActive,
	}
	if err := ValidateTask(task); err == nil {
		t.Fatal("expected empty run_workflow workflow error")
	}
}

func TestValidateTask_Cron(t *testing.T) {
	task := Task{
		ID:       "task_cron",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeCron, CronExpr: "0 9 * * *"},
		Action: Action{
			Type: ActionTypeSendText,
			Text: "daily brief",
		},
		Status: TaskStatusActive,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected cron task to be valid, got err=%v", err)
	}
}

func TestValidateTask_CronInvalidExprRejected(t *testing.T) {
	task := Task{
		ID:       "task_cron_invalid",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeCron, CronExpr: "bad expr"},
		Action: Action{
			Type: ActionTypeSendText,
			Text: "daily brief",
		},
		Status: TaskStatusActive,
	}
	if err := ValidateTask(task); err == nil {
		t.Fatal("expected invalid cron_expr error")
	}
}

func TestValidateTask_MaxRunsReachedActiveRejected(t *testing.T) {
	task := Task{
		ID:       "task_max_runs_reached",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type: ActionTypeSendText,
			Text: "hello",
		},
		Status:   TaskStatusActive,
		MaxRuns:  1,
		RunCount: 1,
	}
	if err := ValidateTask(task); err == nil {
		t.Fatal("expected active reached max_runs error")
	}
}

func TestValidateTask_MaxRunsReachedPausedAllowed(t *testing.T) {
	task := Task{
		ID:       "task_max_runs_paused",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type: ActionTypeSendText,
			Text: "hello",
		},
		Status:   TaskStatusPaused,
		MaxRuns:  1,
		RunCount: 1,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected paused reached max_runs task to be valid, got err=%v", err)
	}
}

func TestNextRunAt_Cron(t *testing.T) {
	from := time.Date(2026, 2, 23, 8, 30, 0, 0, time.Local)
	next := NextRunAt(from, Schedule{Type: ScheduleTypeCron, CronExpr: "0 9 * * *"})
	want := time.Date(2026, 2, 23, 9, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("unexpected cron next run at: got=%s want=%s", next.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}
