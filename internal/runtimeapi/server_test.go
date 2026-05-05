package runtimeapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/sessionctx"
)

func TestBuildTaskFromRequest_BasicFields(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Title:        "heartbeat",
			Prompt:       "总结当前状态",
			EverySeconds: 3600,
			MaxRuns:      5,
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
			route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{SessionKey: "chat_id:oc_chat|work:om_1"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.Prompt != "总结当前状态" {
		t.Fatalf("unexpected prompt: %q", task.Prompt)
	}
	if task.Title != "heartbeat" {
		t.Fatalf("unexpected title: %q", task.Title)
	}
	if task.MaxRuns != 5 {
		t.Fatalf("unexpected max_runs: %d", task.MaxRuns)
	}
	if task.Schedule.EverySeconds != 3600 {
		t.Fatalf("unexpected every_seconds: %d", task.Schedule.EverySeconds)
	}
	if task.Status != automation.TaskStatusActive {
		t.Fatalf("unexpected status: %q", task.Status)
	}
}

func TestBuildTaskFromRequest_SessionKeyIsSet(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Prompt:       "ping",
			EverySeconds: 60,
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
			route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{SessionKey: "chat_id:oc_chat|work:om_1"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.SessionKey != "chat_id:oc_chat|work:om_1" {
		t.Fatalf("unexpected session key: %q", task.SessionKey)
	}
}

func TestBuildTaskFromRequest_PreservesExplicitNextRunAt(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	nextRunAt := time.Date(2026, 3, 26, 15, 30, 0, 0, time.UTC)

	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Prompt:       "ping",
			EverySeconds: 900,
			NextRunAt:    nextRunAt,
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
			route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{SessionKey: "chat_id:oc_chat|work:om_1"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if !task.NextRunAt.Equal(nextRunAt) {
		t.Fatalf("unexpected next_run_at: got=%s want=%s", task.NextRunAt.Format(time.RFC3339), nextRunAt.Format(time.RFC3339))
	}
}

func TestBuildTaskFromRequest_SourceMessageIDThreadsSessionKeyAndRoute_P2P(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Prompt:       "summarize",
			EverySeconds: 600,
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
			route:   automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{
				SessionKey:      "open_id:ou_actor",
				SourceMessageID: "om_thread_abc",
			},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if want := "open_id:ou_actor|message:om_thread_abc"; task.SessionKey != want {
		t.Fatalf("unexpected session key: got=%q want=%q", task.SessionKey, want)
	}
	if task.Route.ReceiveIDType != "source_message_id" {
		t.Fatalf("unexpected route type: got=%q want=source_message_id", task.Route.ReceiveIDType)
	}
	if task.Route.ReceiveID != "om_thread_abc" {
		t.Fatalf("unexpected route id: got=%q want=om_thread_abc", task.Route.ReceiveID)
	}
}

func TestBuildTaskFromRequest_SourceMessageIDThreadsSessionKeyAndRoute_GroupChat(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Prompt:       "summarize",
			EverySeconds: 600,
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"},
			route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_group"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{
				SessionKey:      "chat_id:oc_group",
				SourceMessageID: "om_thread_xyz",
			},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if want := "chat_id:oc_group|message:om_thread_xyz"; task.SessionKey != want {
		t.Fatalf("unexpected session key: got=%q want=%q", task.SessionKey, want)
	}
	if task.Route.ReceiveIDType != "source_message_id" {
		t.Fatalf("unexpected route type: got=%q want=source_message_id", task.Route.ReceiveIDType)
	}
	if task.Route.ReceiveID != "om_thread_xyz" {
		t.Fatalf("unexpected route id: got=%q want=om_thread_xyz", task.Route.ReceiveID)
	}
}

func TestBuildTaskFromRequest_SourceMessageIDDoesNotAffectNoSource(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Prompt:       "summarize",
			EverySeconds: 600,
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
			route:   automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{
				SessionKey: "open_id:ou_actor",
			},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if want := "open_id:ou_actor"; task.SessionKey != want {
		t.Fatalf("unexpected session key: got=%q want=%q", task.SessionKey, want)
	}
	if task.Route.ReceiveIDType != "open_id" {
		t.Fatalf("unexpected route type: got=%q want=open_id", task.Route.ReceiveIDType)
	}
}

func TestBuildTaskFromRequest_CronSchedule(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Prompt:   "daily report",
			CronExpr: "0 9 * * *",
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
			route:   automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{SessionKey: "open_id:ou_actor"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.Schedule.CronExpr != "0 9 * * *" {
		t.Fatalf("unexpected cron_expr: %q", task.Schedule.CronExpr)
	}
	if task.Schedule.EverySeconds != 0 {
		t.Fatalf("expected every_seconds to be cleared when cron is set, got %d", task.Schedule.EverySeconds)
	}
}

func TestBuildTaskFromRequest_EnabledFalse(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	enabled := false
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Prompt:       "hello",
			EverySeconds: 60,
			Enabled:      &enabled,
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
			route:   automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{SessionKey: "open_id:ou_actor"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.Status != automation.TaskStatusPaused {
		t.Fatalf("expected paused status for disabled task, got %q", task.Status)
	}
}

func TestBuildTaskFromRequest_ResumeThreadID(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	task, err := srv.buildTaskFromRequest(
		CreateTaskRequest{
			Prompt:         "continue work",
			EverySeconds:   300,
			ResumeThreadID: "uuid-xxx",
			Fresh:          false,
		},
		automationScopeContext{
			scope:   automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
			route:   automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
			creator: automation.Actor{OpenID: "ou_actor"},
			session: sessionctx.SessionContext{SessionKey: "open_id:ou_actor"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.ResumeThreadID != "uuid-xxx" {
		t.Fatalf("unexpected resume_thread_id: %q", task.ResumeThreadID)
	}
	if task.Fresh != false {
		t.Fatalf("unexpected fresh: %v", task.Fresh)
	}
}

func TestApplyTaskPatch_PreservesSessionKey(t *testing.T) {
	current := automation.Task{
		ID:         "task_123",
		Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    automation.Actor{OpenID: "ou_actor"},
		Schedule:   automation.Schedule{EverySeconds: 3600},
		Prompt:     "总结当前状态",
		SessionKey: "chat_id:oc_chat",
		Status:     automation.TaskStatusActive,
	}

	next, err := applyTaskPatch(current, []byte(`{"prompt":"updated prompt"}`), "application/merge-patch+json", automationScopeContext{
		scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		creator: automation.Actor{OpenID: "ou_actor"},
		session: sessionctx.SessionContext{SessionKey: "chat_id:oc_chat|work:om_1"},
	})
	if err != nil {
		t.Fatalf("apply task patch failed: %v", err)
	}
	if next.SessionKey != "chat_id:oc_chat" {
		t.Fatalf("patch should preserve system session key, got %q", next.SessionKey)
	}
	if next.Prompt != "updated prompt" {
		t.Fatalf("patch should update prompt, got %q", next.Prompt)
	}
}

func TestApplyTaskPatch_PreservesResumeThreadID(t *testing.T) {
	current := automation.Task{
		ID:              "task_threaded",
		Scope:           automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
		Route:           automation.Route{ReceiveIDType: "source_message_id", ReceiveID: "om_thread_a"},
		Creator:         automation.Actor{OpenID: "ou_actor"},
		Schedule:        automation.Schedule{EverySeconds: 3600},
		Prompt:          "ping",
		SessionKey:      "open_id:ou_actor|message:om_thread_a",
		ResumeThreadID:  "uuid_sticky",
		SourceMessageID: "om_thread_a",
		Status:          automation.TaskStatusActive,
	}

	next, err := applyTaskPatch(current, []byte(`{"prompt":"updated"}`), "application/merge-patch+json", automationScopeContext{
		scope:   automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
		route:   automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
		creator: automation.Actor{OpenID: "ou_actor"},
		session: sessionctx.SessionContext{
			SessionKey:      "open_id:ou_actor",
			SourceMessageID: "om_thread_b",
		},
	})
	if err != nil {
		t.Fatalf("apply task patch failed: %v", err)
	}
	if next.SessionKey != "open_id:ou_actor|message:om_thread_a" {
		t.Fatalf("patch should preserve original session key, got %q", next.SessionKey)
	}
	if next.ResumeThreadID != "uuid_sticky" {
		t.Fatalf("patch should preserve resume_thread_id, got %q", next.ResumeThreadID)
	}
	if next.Route.ReceiveIDType != "source_message_id" {
		t.Fatalf("patch should preserve route type, got %q", next.Route.ReceiveIDType)
	}
}

func TestApplyTaskPatch_CanChangeStatus(t *testing.T) {
	current := automation.Task{
		ID:         "task_status",
		Scope:      automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
		Route:      automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
		Creator:    automation.Actor{OpenID: "ou_actor"},
		Schedule:   automation.Schedule{EverySeconds: 60},
		Prompt:     "hello",
		SessionKey: "open_id:ou_actor",
		Status:     automation.TaskStatusActive,
	}

	next, err := applyTaskPatch(current, []byte(`{"status":"paused"}`), "application/merge-patch+json", automationScopeContext{
		scope:   automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
		route:   automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
		creator: automation.Actor{OpenID: "ou_actor"},
		session: sessionctx.SessionContext{SessionKey: "open_id:ou_actor"},
	})
	if err != nil {
		t.Fatalf("apply task patch failed: %v", err)
	}
	if next.Status != automation.TaskStatusPaused {
		t.Fatalf("expected paused status for disabled task, got %q", next.Status)
	}
}

func TestAutomationTaskGet_EnforcesScopeIsolation(t *testing.T) {
	store := automation.NewStore(t.TempDir() + "/automation.db")
	server := NewServer("", "test-token", nil, store, config.Config{})
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()
	client := NewClient(httpServer.URL, "test-token")

	session1 := sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ActorOpenID:   "ou_actor",
		ChatType:      "group",
		SessionKey:    "chat_id:oc_chat|work:om_seed_1",
	}
	session2 := sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ActorOpenID:   "ou_actor",
		ChatType:      "group",
		SessionKey:    "chat_id:oc_chat|work:om_seed_2",
	}

	result1, err := client.CreateTask(t.Context(), session1, CreateTaskRequest{
		Prompt:       "task one",
		EverySeconds: 60,
	})
	if err != nil {
		t.Fatalf("create task1 failed: %v", err)
	}
	task1, ok := result1["task"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected task1 response: %#v", result1)
	}
	task1ID, _ := task1["id"].(string)
	if task1ID == "" {
		t.Fatalf("task1 missing id: %#v", task1)
	}

	result2, err := client.CreateTask(t.Context(), session2, CreateTaskRequest{
		Prompt:       "task two",
		EverySeconds: 60,
	})
	if err != nil {
		t.Fatalf("create task2 failed: %v", err)
	}
	task2, ok := result2["task"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected task2 response: %#v", result2)
	}
	task2ID, _ := task2["id"].(string)
	if task2ID == "" {
		t.Fatalf("task2 missing id: %#v", task2)
	}

	gotResult1, err := client.GetTask(t.Context(), session1, task1ID)
	if err != nil {
		t.Fatalf("get task1 in own scope failed: %v", err)
	}
	gotTask1, _ := gotResult1["task"].(map[string]any)
	gotID1, _ := gotTask1["id"].(string)
	if gotID1 != task1ID {
		t.Fatalf("expected task1 id %q, got %q", task1ID, gotID1)
	}

	_, err = client.GetTask(t.Context(), session1, task2ID)
	if err == nil {
		t.Fatalf("expected error when getting task2 from session1 scope, got nil")
	}
	if !strings.Contains(err.Error(), "task not found in current scope") {
		t.Fatalf("expected scope isolation error, got: %v", err)
	}
}

func TestGoalCreate_RejectsNonWorkSession(t *testing.T) {
	store := automation.NewStore(t.TempDir() + "/automation.db")
	server := NewServer("", "test-token", nil, store, config.Config{})
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()
	client := NewClient(httpServer.URL, "test-token")

	_, err := client.CreateGoal(t.Context(), sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ActorOpenID:   "ou_actor",
		ChatType:      "group",
		SessionKey:    "chat_id:oc_chat",
	}, CreateGoalRequest{
		Objective:  "test goal without work session",
		DeadlineIn: "24h",
	})
	if err == nil {
		t.Fatalf("expected error for non-work session goal creation, got nil")
	}
	if !strings.Contains(err.Error(), "work sessions") {
		t.Fatalf("expected 'work sessions' in error message, got: %v", err)
	}
}

func TestResolveAutomationScope_StripsMessageSuffixFromScopeID(t *testing.T) {
	session := sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ActorUserID:   "ou_user",
		ChatType:      "group",
		SessionKey:    "chat_id:oc_chat|work:om_seed|message:om_reply",
	}
	scopeCtx, err := resolveAutomationScope(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scopeCtx.scope.ID != "chat_id:oc_chat|work:om_seed" {
		t.Fatalf("expected scope ID without message suffix, got %q", scopeCtx.scope.ID)
	}
}
