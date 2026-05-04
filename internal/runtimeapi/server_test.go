package runtimeapi

import (
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
			session: sessionctx.SessionContext{SessionKey: "chat_id:oc_chat|scene:work|seed:om_1"},
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
			session: sessionctx.SessionContext{SessionKey: "chat_id:oc_chat|scene:work|seed:om_1"},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if task.SessionKey != "chat_id:oc_chat|scene:work|seed:om_1" {
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
			session: sessionctx.SessionContext{SessionKey: "chat_id:oc_chat|scene:work|seed:om_1"},
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
				SessionKey:      "chat_id:oc_group|scene:chat",
				SourceMessageID: "om_thread_xyz",
			},
		},
	)
	if err != nil {
		t.Fatalf("build task failed: %v", err)
	}
	if want := "chat_id:oc_group|scene:chat|message:om_thread_xyz"; task.SessionKey != want {
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
		SessionKey: "chat_id:oc_chat|scene:chat",
		Status:     automation.TaskStatusActive,
	}

	next, err := applyTaskPatch(current, []byte(`{"prompt":"updated prompt"}`), "application/merge-patch+json", automationScopeContext{
		scope:   automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		route:   automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		creator: automation.Actor{OpenID: "ou_actor"},
		session: sessionctx.SessionContext{SessionKey: "chat_id:oc_chat|scene:work|seed:om_1|message:om_2"},
	})
	if err != nil {
		t.Fatalf("apply task patch failed: %v", err)
	}
	if next.SessionKey != "chat_id:oc_chat|scene:chat" {
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
		t.Fatalf("expected paused status, got %q", next.Status)
	}
}
