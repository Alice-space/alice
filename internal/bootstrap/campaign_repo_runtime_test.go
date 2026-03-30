package bootstrap

import (
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaignrepo"
)

func TestCampaignIDFromAutomationStateKey(t *testing.T) {
	tests := []struct {
		name     string
		stateKey string
		wantID   string
		wantOK   bool
	}{
		{
			name:     "dispatch task",
			stateKey: "campaign_dispatch:camp_demo:executor:T001:x1",
			wantID:   "camp_demo",
			wantOK:   true,
		},
		{
			name:     "wake task",
			stateKey: "campaign_wake:camp_demo:T001:2026-03-25T10:00:00Z",
			wantID:   "camp_demo",
			wantOK:   true,
		},
		{
			name:     "unknown campaign",
			stateKey: "campaign_dispatch:unknown:executor:T001:x1",
			wantOK:   false,
		},
		{
			name:     "non campaign task",
			stateKey: "automation:other",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := campaignIDFromAutomationStateKey(tt.stateKey)
			if gotOK != tt.wantOK {
				t.Fatalf("unexpected ok state: got=%v want=%v", gotOK, tt.wantOK)
			}
			if gotID != tt.wantID {
				t.Fatalf("unexpected campaign id: got=%q want=%q", gotID, tt.wantID)
			}
		})
	}
}

func TestDispatchKindAndTaskIDFromStateKey(t *testing.T) {
	tests := []struct {
		name     string
		stateKey string
		wantKind campaignrepo.DispatchKind
		wantTask string
		wantOK   bool
	}{
		{
			name:     "executor dispatch",
			stateKey: "campaign_dispatch:camp_demo:executor:T001:x1",
			wantKind: campaignrepo.DispatchKindExecutor,
			wantTask: "T001",
			wantOK:   true,
		},
		{
			name:     "reviewer dispatch",
			stateKey: "campaign_dispatch:camp_demo:reviewer:T001:r2",
			wantKind: campaignrepo.DispatchKindReviewer,
			wantTask: "T001",
			wantOK:   true,
		},
		{
			name:     "planner dispatch is ignored",
			stateKey: "campaign_dispatch:camp_demo:planner:plan-r1:r1",
			wantOK:   false,
		},
		{
			name:     "wake task is ignored",
			stateKey: "campaign_wake:camp_demo:T001:2026-03-25T10:00:00Z",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKind, gotTask, gotOK := dispatchKindAndTaskIDFromStateKey(tt.stateKey)
			if gotOK != tt.wantOK {
				t.Fatalf("unexpected ok state: got=%v want=%v", gotOK, tt.wantOK)
			}
			if gotKind != tt.wantKind {
				t.Fatalf("unexpected dispatch kind: got=%q want=%q", gotKind, tt.wantKind)
			}
			if gotTask != tt.wantTask {
				t.Fatalf("unexpected task id: got=%q want=%q", gotTask, tt.wantTask)
			}
		})
	}
}

func TestShouldKeepExistingDispatchTask_RespectsFailureCooldown(t *testing.T) {
	now := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	spec := campaignrepo.DispatchTaskSpec{
		StateKey: "campaign_dispatch:camp_demo:planner:r1",
		TaskID:   "plan-r1",
		Title:    "Demo Campaign · 规划 · 第 1 轮",
		Prompt:   "plan this campaign",
		Role: campaignrepo.RoleConfig{
			Provider:        "claude",
			Workflow:        "code_army",
			ReasoningEffort: "high",
			Personality:     "analytical",
		},
	}
	active := automation.Task{
		Title: "Demo Campaign · 规划 · 第 1 轮",
		Action: automation.Action{
			Prompt:          "plan this campaign",
			Provider:        "claude",
			Workflow:        "code_army",
			ReasoningEffort: "high",
			Personality:     "analytical",
		},
		Status: automation.TaskStatusActive,
	}
	if !shouldKeepExistingDispatchTask(active, spec, now) {
		t.Fatal("expected active matching task to be kept")
	}

	paused := active
	paused.Status = automation.TaskStatusPaused
	paused.RunCount = 1
	if shouldKeepExistingDispatchTask(paused, spec, now) {
		t.Fatal("expected completed paused task without error to be recreated")
	}

	cooling := paused
	cooling.LastResult = "error: planner failed"
	cooling.UpdatedAt = now.Add(-30 * time.Second)
	if !shouldKeepExistingDispatchTask(cooling, spec, now) {
		t.Fatal("expected recently failed paused task to stay in cooldown")
	}

	expired := cooling
	expired.UpdatedAt = now.Add(-campaignDispatchFailureCooldown - time.Second)
	if shouldKeepExistingDispatchTask(expired, spec, now) {
		t.Fatal("expected expired failed paused task to be recreated")
	}
}

func TestBuildDispatchAutomationTask_PreservesRunAt(t *testing.T) {
	runAt := time.Date(2026, 3, 25, 10, 30, 0, 0, time.UTC)
	task := buildDispatchAutomationTask(
		automationTaskTarget{
			Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
			Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
			Creator:    automation.Actor{OpenID: "ou_actor"},
			ManageMode: automation.ManageModeCreatorOnly,
			SessionKey: "chat_id:oc_chat|scene:work|thread:omt_1",
		},
		campaignrepo.DispatchTaskSpec{
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Kind:     campaignrepo.DispatchKindPlanner,
			TaskID:   "plan-r1",
			Title:    "Demo Campaign · 规划 · 第 1 轮",
			RunAt:    runAt,
			Prompt:   "plan this campaign",
			Role: campaignrepo.RoleConfig{
				Provider: "claude",
				Workflow: "code_army",
			},
		},
	)
	if !task.NextRunAt.Equal(runAt) {
		t.Fatalf("unexpected next_run_at: got=%s want=%s", task.NextRunAt.Format(time.RFC3339), runAt.Format(time.RFC3339))
	}
}

func TestUpsertDispatchTask_ReactivatesCompletedTask(t *testing.T) {
	store := automation.NewStore(t.TempDir() + "/automation.db")
	builder := &connectorRuntimeBuilder{automationStore: store}
	runAt := time.Date(2026, 3, 25, 10, 30, 0, 0, time.UTC)

	created, err := store.CreateTask(automation.Task{
		Title: "old dispatch",
		Scope: automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		Route: automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator: automation.Actor{
			OpenID: "ou_actor",
		},
		ManageMode: automation.ManageModeCreatorOnly,
		Schedule: automation.Schedule{
			Type:         automation.ScheduleTypeInterval,
			EverySeconds: 60,
		},
		Action: automation.Action{
			Type:     automation.ActionTypeRunWorkflow,
			Prompt:   "old prompt",
			Workflow: "code_army",
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
		},
		Status:              automation.TaskStatusPaused,
		MaxRuns:             1,
		RunCount:            1,
		LastResult:          "error: old failure",
		LastSignalKind:      "blocked",
		ConsecutiveFailures: 1,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	err = builder.upsertDispatchTask(
		created,
		automationTaskTarget{
			Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
			Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
			Creator:    automation.Actor{OpenID: "ou_actor"},
			ManageMode: automation.ManageModeCreatorOnly,
			SessionKey: "chat_id:oc_chat|scene:work|thread:omt_1",
		},
		campaignrepo.DispatchTaskSpec{
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Kind:     campaignrepo.DispatchKindPlanner,
			TaskID:   "plan-r1",
			Title:    "Demo Campaign · 规划 · 第 1 轮",
			RunAt:    runAt,
			Prompt:   "plan this campaign",
			Role: campaignrepo.RoleConfig{
				Provider:        "claude",
				Workflow:        "code_army",
				ReasoningEffort: "high",
				Personality:     "pragmatic",
			},
		},
	)
	if err != nil {
		t.Fatalf("upsert dispatch task failed: %v", err)
	}

	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get updated task failed: %v", err)
	}
	if updated.Status != automation.TaskStatusActive {
		t.Fatalf("expected active task after reset, got %s", updated.Status)
	}
	if updated.RunCount != 0 {
		t.Fatalf("expected run_count reset, got %d", updated.RunCount)
	}
	if updated.LastResult != "" {
		t.Fatalf("expected last_result reset, got %q", updated.LastResult)
	}
	if updated.ConsecutiveFailures != 0 {
		t.Fatalf("expected failures reset, got %d", updated.ConsecutiveFailures)
	}
	if !updated.NextRunAt.Equal(runAt) {
		t.Fatalf("unexpected next_run_at: got=%s want=%s", updated.NextRunAt.Format(time.RFC3339), runAt.Format(time.RFC3339))
	}
}
