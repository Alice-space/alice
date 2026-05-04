package automation

import (
	"testing"
	"time"
)

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

func TestValidateTask_ValidInterval(t *testing.T) {
	task := Task{
		ID:       "task_interval",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "hello {{now}}",
		Status:   TaskStatusActive,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected task to be valid, got err=%v", err)
	}
}

func TestValidateTask_ValidCron(t *testing.T) {
	task := Task{
		ID:       "task_cron",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{CronExpr: "0 9 * * *"},
		Prompt:   "daily brief",
		Status:   TaskStatusActive,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected cron task to be valid, got err=%v", err)
	}
}

func TestValidateTask_EmptyPromptRejected(t *testing.T) {
	task := Task{
		ID:       "task_empty_prompt",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "",
		Status:   TaskStatusActive,
	}
	if err := ValidateTask(task); err == nil {
		t.Fatal("expected empty prompt error")
	}
}

func TestValidateTask_NoScheduleRejected(t *testing.T) {
	task := Task{
		ID:       "task_no_schedule",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{},
		Prompt:   "hello",
		Status:   TaskStatusActive,
	}
	if err := ValidateTask(task); err == nil {
		t.Fatal("expected no schedule error")
	}
}

func TestValidateTask_EverySecondsUnder60Rejected(t *testing.T) {
	task := Task{
		ID:       "task_short_interval",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{EverySeconds: 30},
		Prompt:   "hello",
		Status:   TaskStatusActive,
	}
	if err := ValidateTask(task); err == nil {
		t.Fatal("expected every_seconds >= 60 error")
	}
}

func TestValidateTask_CronInvalidExprRejected(t *testing.T) {
	task := Task{
		ID:       "task_cron_invalid",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{CronExpr: "bad expr"},
		Prompt:   "daily brief",
		Status:   TaskStatusActive,
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
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "hello",
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
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "hello",
		Status:   TaskStatusPaused,
		MaxRuns:  1,
		RunCount: 1,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected paused reached max_runs task to be valid, got err=%v", err)
	}
}

func TestValidateTask_MaxRunsReachedActiveRunningAllowed(t *testing.T) {
	task := Task{
		ID:       "task_max_runs_running",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "hello",
		Status:   TaskStatusActive,
		MaxRuns:  1,
		RunCount: 1,
		Running:  true,
	}
	if err := ValidateTask(task); err != nil {
		t.Fatalf("expected active running max_runs task to be valid, got err=%v", err)
	}
}

func TestNormalizeTask_TrimFields(t *testing.T) {
	task := NormalizeTask(Task{
		Prompt:   "  hello  ",
		Schedule: Schedule{EverySeconds: 120},
	})
	if task.Prompt != "hello" {
		t.Fatalf("unexpected normalized prompt: %q", task.Prompt)
	}
	if task.ManageMode != ManageModeCreatorOnly {
		t.Fatalf("unexpected default manage mode: %q", task.ManageMode)
	}
	if task.Status != TaskStatusActive {
		t.Fatalf("unexpected default status: %q", task.Status)
	}
}

func TestNextRunAt_Cron(t *testing.T) {
	from := time.Date(2026, 2, 23, 8, 30, 0, 0, time.Local)
	next := NextRunAt(from, Schedule{CronExpr: "0 9 * * *"})
	want := time.Date(2026, 2, 23, 9, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("unexpected cron next run at: got=%s want=%s", next.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestNextRunAt_Interval(t *testing.T) {
	from := time.Date(2026, 2, 23, 8, 30, 0, 0, time.Local)
	next := NextRunAt(from, Schedule{EverySeconds: 300})
	want := time.Date(2026, 2, 23, 8, 35, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("unexpected interval next run at: got=%s want=%s", next.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}
