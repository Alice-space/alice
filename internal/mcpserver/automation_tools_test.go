package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"gitee.com/alicespace/alice/internal/automation"
	"gitee.com/alicespace/alice/internal/mcpbridge"
)

func TestAutomationTaskCreate_PrivateScope(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_p2p"
			case mcpbridge.EnvActorUserID:
				return "ou_actor"
			case mcpbridge.EnvChatType:
				return "p2p"
			default:
				return ""
			}
		},
	}

	okResult, err := svc.handleAutomationTaskCreate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"every_seconds":    60,
			"text":             "hello",
			"mention_user_ids": []string{"ou_actor"},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected create handler error: %v", err)
	}
	if okResult == nil || okResult.IsError {
		t.Fatalf("expected create success result, got %#v", okResult)
	}

	failedResult, err := svc.handleAutomationTaskCreate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"every_seconds":    60,
			"text":             "hello",
			"mention_user_ids": []string{"ou_other"},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected create handler error: %v", err)
	}
	if failedResult == nil || !failedResult.IsError {
		t.Fatalf("expected private mention permission error, got %#v", failedResult)
	}
}

func TestAutomationTaskCreate_GroupScopeAllowsMentionOthers(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_actor"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskCreate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"every_seconds":    60,
			"text":             "ping",
			"mention_user_ids": []string{"ou_776ddbea0c07fd88caaf8fce1b413a41"},
		}},
	})
	if err != nil {
		t.Fatalf("unexpected create handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected group create success, got %#v", result)
	}

	list, err := store.ListTasks(automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"}, "", 10)
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 created task, got %d", len(list))
	}
	if list[0].Scope.Kind != automation.ScopeKindChat || list[0].Scope.ID != "oc_group" {
		t.Fatalf("unexpected task scope: %+v", list[0].Scope)
	}
}

func TestAutomationTaskCreate_RunLLMByPromptDefaultActionType(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_actor"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskCreate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"every_seconds": 60,
			"prompt":        "请输出当前时间 {{now}}",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected create handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected run_llm create success, got %#v", result)
	}

	list, err := store.ListTasks(automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"}, "", 10)
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 created task, got %d", len(list))
	}
	if list[0].Action.Type != automation.ActionTypeRunLLM {
		t.Fatalf("expected run_llm action type, got %s", list[0].Action.Type)
	}
	if list[0].Action.Prompt == "" {
		t.Fatalf("expected run_llm prompt to be stored, got %+v", list[0].Action)
	}
}

func TestAutomationTaskCreate_MaxRuns(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_actor"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskCreate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"every_seconds": 60,
			"text":          "run once",
			"max_runs":      1,
		}},
	})
	if err != nil {
		t.Fatalf("unexpected create handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected create success, got %#v", result)
	}

	list, err := store.ListTasks(automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"}, "", 10)
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 created task, got %d", len(list))
	}
	if list[0].MaxRuns != 1 {
		t.Fatalf("expected max_runs=1, got %d", list[0].MaxRuns)
	}
}

func TestAutomationTaskUpdate_PermissionDeniedForCreatorOnly(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	created, err := store.CreateTask(automation.Task{
		Title:      "group reminder",
		Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"},
		Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_group"},
		Creator:    automation.Actor{UserID: "ou_creator"},
		ManageMode: automation.ManageModeCreatorOnly,
		Schedule:   automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 60},
		Action:     automation.Action{Type: automation.ActionTypeSendText, Text: "hello"},
		Status:     automation.TaskStatusActive,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_other"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskUpdate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"task_id": created.ID,
			"text":    "changed",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected update handler error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected permission denied result, got %#v", result)
	}
}

func TestAutomationTaskDelete_ScopeAllAllowsOthers(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	created, err := store.CreateTask(automation.Task{
		Title:      "group reminder",
		Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"},
		Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_group"},
		Creator:    automation.Actor{UserID: "ou_creator"},
		ManageMode: automation.ManageModeScopeAll,
		Schedule:   automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 60},
		Action:     automation.Action{Type: automation.ActionTypeSendText, Text: "hello"},
		Status:     automation.TaskStatusActive,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_other"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskDelete(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{"task_id": created.ID}},
	})
	if err != nil {
		t.Fatalf("unexpected delete handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected delete success result, got %#v", result)
	}

	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if updated.Status != automation.TaskStatusDeleted {
		t.Fatalf("expected deleted status, got %s", updated.Status)
	}
}

func TestAutomationTaskUpdate_CanSwitchToRunLLM(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	created, err := store.CreateTask(automation.Task{
		Title:      "group reminder",
		Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"},
		Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_group"},
		Creator:    automation.Actor{UserID: "ou_creator"},
		ManageMode: automation.ManageModeCreatorOnly,
		Schedule:   automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 60},
		Action:     automation.Action{Type: automation.ActionTypeSendText, Text: "hello"},
		Status:     automation.TaskStatusActive,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_creator"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskUpdate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"task_id":     created.ID,
			"action_type": "run_llm",
			"prompt":      "请输出当前时间 {{now}}",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected update handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected update success result, got %#v", result)
	}

	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if updated.Action.Type != automation.ActionTypeRunLLM {
		t.Fatalf("expected run_llm action type, got %s", updated.Action.Type)
	}
	if updated.Action.Prompt == "" {
		t.Fatalf("expected updated run_llm prompt, got %+v", updated.Action)
	}
}

func TestAutomationTaskUpdate_MaxRunsReachedCannotEnable(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	created, err := store.CreateTask(automation.Task{
		Title:      "single run",
		Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"},
		Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_group"},
		Creator:    automation.Actor{UserID: "ou_creator"},
		ManageMode: automation.ManageModeCreatorOnly,
		Schedule:   automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 60},
		Action:     automation.Action{Type: automation.ActionTypeSendText, Text: "hello"},
		Status:     automation.TaskStatusPaused,
		MaxRuns:    1,
		RunCount:   1,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_creator"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskUpdate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"task_id": created.ID,
			"enabled": true,
		}},
	})
	if err != nil {
		t.Fatalf("unexpected update handler error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("expected max_runs reached update error, got %#v", result)
	}
}

func TestAutomationTaskCreate_CronSchedule(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_actor"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskCreate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"schedule_type": "cron",
			"cron_expr":     "0 9 * * *",
			"text":          "daily brief",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected create handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected cron create success, got %#v", result)
	}

	list, err := store.ListTasks(automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"}, "", 10)
	if err != nil {
		t.Fatalf("list tasks failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 created task, got %d", len(list))
	}
	if list[0].Schedule.Type != automation.ScheduleTypeCron {
		t.Fatalf("expected cron schedule type, got %s", list[0].Schedule.Type)
	}
	if list[0].Schedule.CronExpr != "0 9 * * *" {
		t.Fatalf("expected cron_expr to be stored, got %q", list[0].Schedule.CronExpr)
	}
}

func TestAutomationTaskUpdate_CanSwitchIntervalToCron(t *testing.T) {
	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	created, err := store.CreateTask(automation.Task{
		Title:      "group reminder",
		Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"},
		Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_group"},
		Creator:    automation.Actor{UserID: "ou_creator"},
		ManageMode: automation.ManageModeCreatorOnly,
		Schedule:   automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 60},
		Action:     automation.Action{Type: automation.ActionTypeSendText, Text: "hello"},
		Status:     automation.TaskStatusActive,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	svc := &service{
		sender:          &senderStub{},
		automationStore: store,
		getenv: func(key string) string {
			switch key {
			case mcpbridge.EnvReceiveIDType:
				return "chat_id"
			case mcpbridge.EnvReceiveID:
				return "oc_group"
			case mcpbridge.EnvActorUserID:
				return "ou_creator"
			case mcpbridge.EnvChatType:
				return "group"
			default:
				return ""
			}
		},
	}

	result, err := svc.handleAutomationTaskUpdate(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Arguments: map[string]any{
			"task_id":       created.ID,
			"schedule_type": "cron",
			"cron_expr":     "0 9 * * *",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected update handler error: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("expected update success result, got %#v", result)
	}

	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if updated.Schedule.Type != automation.ScheduleTypeCron {
		t.Fatalf("expected cron schedule type, got %s", updated.Schedule.Type)
	}
	if updated.Schedule.CronExpr != "0 9 * * *" {
		t.Fatalf("expected cron_expr to be updated, got %q", updated.Schedule.CronExpr)
	}
	if updated.NextRunAt.IsZero() || !updated.NextRunAt.After(time.Now().UTC().Add(-time.Minute)) {
		t.Fatalf("expected next_run_at to be recalculated, got %s", updated.NextRunAt.Format(time.RFC3339))
	}
}
