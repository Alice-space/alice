package runtimeapi

import (
	"path/filepath"
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

func TestBuildTaskFromRequest_RouteIsChatScopeNotThread(t *testing.T) {
	srv := NewServer("", "", nil, nil, config.Config{})
	tests := []struct {
		name     string
		scopeCtx automationScopeContext
		wantType string
		wantID   string
	}{
		{
			name: "P2P",
			scopeCtx: automationScopeContext{
				scope:   automation.Scope{Kind: automation.ScopeKindUser, ID: "ou_actor"},
				route:   automation.Route{ReceiveIDType: "open_id", ReceiveID: "ou_actor"},
				creator: automation.Actor{OpenID: "ou_actor"},
				session: sessionctx.SessionContext{
					SessionKey:      "open_id:ou_actor",
					SourceMessageID: "om_thread_abc",
				},
			},
			wantType: "open_id",
			wantID:   "ou_actor",
		},
		{
			name: "GroupChat",
			scopeCtx: automationScopeContext{
				scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_group"},
				route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_group"},
				creator: automation.Actor{OpenID: "ou_actor"},
				session: sessionctx.SessionContext{
					SessionKey:      "chat_id:oc_group",
					SourceMessageID: "om_thread_xyz",
				},
			},
			wantType: "chat_id",
			wantID:   "oc_group",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := srv.buildTaskFromRequest(
				CreateTaskRequest{Prompt: "task", EverySeconds: 600},
				tt.scopeCtx,
			)
			if err != nil {
				t.Fatalf("build task failed: %v", err)
			}
			if task.Route.ReceiveIDType != tt.wantType {
				t.Fatalf("expected route type %q (chat scope output), got %q", tt.wantType, task.Route.ReceiveIDType)
			}
			if task.Route.ReceiveID != tt.wantID {
				t.Fatalf("expected route id %q, got %q", tt.wantID, task.Route.ReceiveID)
			}
		})
	}
}

func TestResolveAutomationTaskScope_GroupChatScopeIsPerChat(t *testing.T) {
	session := sessionctx.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ActorUserID:   "ou_user",
		ChatType:      "group",
		SessionKey:    "chat_id:oc_chat|work:om_seed|message:om_reply",
	}
	scopeCtx, err := resolveAutomationTaskScope(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scopeCtx.scope.ID != "chat_id:oc_chat" {
		t.Fatalf("expected task scope per-chat 'chat_id:oc_chat', got %q", scopeCtx.scope.ID)
	}
	if scopeCtx.route.ReceiveIDType != "chat_id" {
		t.Fatalf("expected chat_id route, got %q", scopeCtx.route.ReceiveIDType)
	}
}

func TestResolveAutomationTaskScope_P2PScopeIsPerUser(t *testing.T) {
	session := sessionctx.SessionContext{
		ReceiveIDType: "open_id",
		ReceiveID:     "ou_actor",
		ActorOpenID:   "ou_actor",
		ChatType:      "p2p",
		SessionKey:    "open_id:ou_actor|message:om_thread_abc",
	}
	scopeCtx, err := resolveAutomationTaskScope(session)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scopeCtx.scope.ID != "ou_actor" {
		t.Fatalf("expected task scope per-user 'ou_actor', got %q", scopeCtx.scope.ID)
	}
	if scopeCtx.route.ReceiveIDType != "open_id" {
		t.Fatalf("expected open_id route, got %q", scopeCtx.route.ReceiveIDType)
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
	socketDir := shortSocketDir(t)
	socketPath := filepath.Join(socketDir, "s")
	store := automation.NewStore(filepath.Join(socketDir, "automation.db"))
	server := NewServer(socketPath, "test-token", nil, store, config.Config{})
	cancel := startServer(t, server, socketPath)
	defer cancel()
	client := newTestClient(t, socketPath, "test-token")

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
	if err != nil {
		t.Fatalf("expected task2 to be accessible from session1 (shared per-chat scope), got: %v", err)
	}

	_, err = client.GetTask(t.Context(), session2, task1ID)
	if err != nil {
		t.Fatalf("expected task1 to be accessible from session2 (shared per-chat scope), got: %v", err)
	}
}

func TestGoalCreate_RejectsNonWorkSession(t *testing.T) {
	socketDir := shortSocketDir(t)
	socketPath := filepath.Join(socketDir, "s")
	store := automation.NewStore(filepath.Join(socketDir, "automation.db"))
	server := NewServer(socketPath, "test-token", nil, store, config.Config{})
	cancel := startServer(t, server, socketPath)
	defer cancel()
	client := newTestClient(t, socketPath, "test-token")

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

func TestValidateDelayGoalRequest_RejectsEmptyDuration(t *testing.T) {
	_, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "",
		Reason:   "test reason",
	})
	if err == nil {
		t.Fatal("expected error for empty duration")
	}
	if !strings.Contains(err.Error(), "duration is required") {
		t.Fatalf("expected 'duration is required', got: %v", err)
	}
}

func TestValidateDelayGoalRequest_RejectsInvalidDuration(t *testing.T) {
	_, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "abc",
		Reason:   "test reason",
	})
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
	if !strings.Contains(err.Error(), "invalid duration") {
		t.Fatalf("expected 'invalid duration', got: %v", err)
	}
}

func TestValidateDelayGoalRequest_RejectsNegativeDuration(t *testing.T) {
	_, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "-5m",
		Reason:   "test reason",
	})
	if err == nil {
		t.Fatal("expected error for negative duration")
	}
	if !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("expected 'must not be negative', got: %v", err)
	}
}

func TestValidateDelayGoalRequest_RejectsDurationTooLarge(t *testing.T) {
	_, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "13h",
		Reason:   "test reason",
	})
	if err == nil {
		t.Fatal("expected error for duration > 12h")
	}
	if !strings.Contains(err.Error(), "must not exceed 12h") {
		t.Fatalf("expected 'must not exceed 12h', got: %v", err)
	}
}

func TestValidateDelayGoalRequest_RejectsDurationUnderOneMinute(t *testing.T) {
	_, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "30s",
		Reason:   "test reason",
	})
	if err == nil {
		t.Fatal("expected error for duration < 1m (positive but under 1m)")
	}
	if !strings.Contains(err.Error(), "at least 1m") {
		t.Fatalf("expected 'at least 1m', got: %v", err)
	}
}

func TestValidateDelayGoalRequest_AcceptsZeroSeconds(t *testing.T) {
	duration, reason, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "0s",
		Reason:   "继续推进下一项任务",
	})
	if err != nil {
		t.Fatalf("expected success for 0s, got: %v", err)
	}
	if duration != 0 {
		t.Fatalf("expected zero duration, got %s", duration)
	}
	if reason != "继续推进下一项任务" {
		t.Fatalf("expected reason preserved, got %q", reason)
	}
}

func TestValidateDelayGoalRequest_AcceptsValidDuration(t *testing.T) {
	duration, reason, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "30m",
		Reason:   "等待 CI 构建完成",
	})
	if err != nil {
		t.Fatalf("expected success for valid duration, got: %v", err)
	}
	if duration != 30*time.Minute {
		t.Fatalf("expected 30m duration, got %s", duration)
	}
	if reason != "等待 CI 构建完成" {
		t.Fatalf("expected reason preserved, got %q", reason)
	}
}

func TestValidateDelayGoalRequest_AcceptsCompoundDuration(t *testing.T) {
	duration, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "2h30m",
		Reason:   "waiting for review, then re-check",
	})
	if err != nil {
		t.Fatalf("expected success for compound duration, got: %v", err)
	}
	if duration != 2*time.Hour+30*time.Minute {
		t.Fatalf("expected 2h30m, got %s", duration)
	}
}

func TestValidateDelayGoalRequest_RejectsEmptyReason(t *testing.T) {
	_, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "5m",
		Reason:   "",
	})
	if err == nil {
		t.Fatal("expected error for empty reason")
	}
	if !strings.Contains(err.Error(), "reason is required") {
		t.Fatalf("expected 'reason is required', got: %v", err)
	}
}

func TestValidateDelayGoalRequest_AcceptsMaxDelay(t *testing.T) {
	duration, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "12h",
		Reason:   "long wait for external dependency",
	})
	if err != nil {
		t.Fatalf("expected success for 12h, got: %v", err)
	}
	if duration != 12*time.Hour {
		t.Fatalf("expected 12h, got %s", duration)
	}
}

func TestValidateDelayGoalRequest_AcceptsOneMinute(t *testing.T) {
	duration, _, err := validateDelayGoalRequest(DelayGoalRequest{
		Duration: "1m",
		Reason:   "minimum valid delay",
	})
	if err != nil {
		t.Fatalf("expected success for 1m, got: %v", err)
	}
	if duration != time.Minute {
		t.Fatalf("expected 1m, got %s", duration)
	}
}
