package bootstrap

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
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

func TestDispatchCompletionTargetFromStateKey(t *testing.T) {
	tests := []struct {
		name     string
		stateKey string
		want     dispatchCompletionTarget
		wantOK   bool
	}{
		{
			name:     "executor dispatch",
			stateKey: "campaign_dispatch:camp_demo:executor:T001:x1",
			want: dispatchCompletionTarget{
				Kind:   campaignrepo.DispatchKindExecutor,
				TaskID: "T001",
			},
			wantOK: true,
		},
		{
			name:     "reviewer dispatch",
			stateKey: "campaign_dispatch:camp_demo:reviewer:T001:r2",
			want: dispatchCompletionTarget{
				Kind:   campaignrepo.DispatchKindReviewer,
				TaskID: "T001",
			},
			wantOK: true,
		},
		{
			name:     "planner dispatch",
			stateKey: "campaign_dispatch:camp_demo:planner:r1",
			want: dispatchCompletionTarget{
				Kind:      campaignrepo.DispatchKindPlanner,
				PlanRound: 1,
			},
			wantOK: true,
		},
		{
			name:     "planner reviewer dispatch",
			stateKey: "campaign_dispatch:camp_demo:planner_reviewer:r2",
			want: dispatchCompletionTarget{
				Kind:      campaignrepo.DispatchKindPlannerReviewer,
				PlanRound: 2,
			},
			wantOK: true,
		},
		{
			name:     "wake task is ignored",
			stateKey: "campaign_wake:camp_demo:T001:2026-03-25T10:00:00Z",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := dispatchCompletionTargetFromStateKey(tt.stateKey)
			if gotOK != tt.wantOK {
				t.Fatalf("unexpected ok state: got=%v want=%v", gotOK, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("unexpected dispatch completion target: got=%+v want=%+v", got, tt.want)
			}
		})
	}
}

func TestNewCampaignAutomationFailureEvent(t *testing.T) {
	event, ok := newCampaignAutomationFailureEvent("camp_demo", automation.Task{
		ID:                  "task_dispatch",
		Title:               "Demo Campaign · T304 · 评审 · 第 8 轮",
		Status:              automation.TaskStatusPaused,
		MaxRuns:             1,
		ConsecutiveFailures: 3,
		LastResult:          "error: codex exec failed: exit status 1",
		Action: automation.Action{
			Type:     automation.ActionTypeRunWorkflow,
			Workflow: "code_army",
			StateKey: "campaign_dispatch:camp_demo:reviewer:T304:r8",
		},
	})
	if !ok {
		t.Fatal("expected third failed internal workflow task to emit automation failure event")
	}
	if event.Kind != campaignrepo.EventAutomationFailed {
		t.Fatalf("unexpected event kind: %+v", event)
	}
	if event.TaskID != "T304" {
		t.Fatalf("unexpected event task id: %q", event.TaskID)
	}
	if event.Title != "内部调度连续失败，已暂停" {
		t.Fatalf("unexpected event title: %q", event.Title)
	}
	if !strings.Contains(event.Detail, "连续失败 3 次") {
		t.Fatalf("expected failure count in detail, got %q", event.Detail)
	}
	if !strings.Contains(event.Detail, "campaign_dispatch:camp_demo:reviewer:T304:r8") {
		t.Fatalf("expected state key in detail, got %q", event.Detail)
	}
	if !strings.Contains(event.Detail, "codex exec failed: exit status 1") {
		t.Fatalf("expected last error in detail, got %q", event.Detail)
	}
	if !strings.Contains(event.Detail, "repo-reconcile") {
		t.Fatalf("expected suggested action in detail, got %q", event.Detail)
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

func TestShouldKeepExistingDispatchTask_ResetsInterruptedActiveTask(t *testing.T) {
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
	interrupted := automation.Task{
		Title: "Demo Campaign · 规划 · 第 1 轮",
		Action: automation.Action{
			Type:            automation.ActionTypeRunWorkflow,
			Prompt:          "plan this campaign",
			Provider:        "claude",
			Workflow:        "code_army",
			ReasoningEffort: "high",
			Personality:     "analytical",
			StateKey:        "campaign_dispatch:camp_demo:planner:r1",
		},
		Status:   automation.TaskStatusActive,
		MaxRuns:  1,
		RunCount: 1,
	}
	if shouldKeepExistingDispatchTask(interrupted, spec, now) {
		t.Fatal("expected interrupted active dispatch task to be reset")
	}

	healthy := interrupted
	healthy.RunCount = 0
	healthy.NextRunAt = now
	if !shouldKeepExistingDispatchTask(healthy, spec, now) {
		t.Fatal("expected healthy active dispatch task to be kept")
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

func TestDeleteStaleCampaignAutomationTasks_DeletesOrphanedActiveTask(t *testing.T) {
	store := automation.NewStore(t.TempDir() + "/automation.db")
	builder := &connectorRuntimeBuilder{automationStore: store}

	created, err := store.CreateTask(automation.Task{
		Title: "orphaned dispatch",
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
			StateKey: "campaign_dispatch:camp_demo:executor:T001:x3",
		},
		Status: automation.TaskStatusActive,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	err = builder.deleteStaleCampaignAutomationTasks(map[string]automation.Task{
		created.Action.StateKey: created,
	}, map[string]struct{}{})
	if err != nil {
		t.Fatalf("delete stale dispatch tasks failed: %v", err)
	}

	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get updated task failed: %v", err)
	}
	if updated.Status != automation.TaskStatusDeleted {
		t.Fatalf("expected deleted status, got %s", updated.Status)
	}
	if !updated.NextRunAt.IsZero() {
		t.Fatalf("expected next_run_at cleared, got %s", updated.NextRunAt.Format(time.RFC3339))
	}
}

func TestShouldMarkCampaignCompleted(t *testing.T) {
	item := campaign.Campaign{Status: campaign.StatusRunning}
	summary := campaignrepo.Summary{
		TaskCount:           3,
		AcceptedCount:       2,
		DoneCount:           1,
		RejectedCount:       0,
		ActiveCount:         0,
		ReadyCount:          0,
		ReworkCount:         0,
		ReviewPendingCount:  0,
		ReviewingCount:      0,
		SelectedReadyCount:  0,
		SelectedReviewCount: 0,
		BlockedCount:        0,
		WaitingCount:        0,
	}
	if !shouldMarkCampaignCompleted(item, summary) {
		t.Fatal("expected fully terminal campaign summary to mark campaign completed")
	}

	summary.ReviewPendingCount = 1
	if shouldMarkCampaignCompleted(item, summary) {
		t.Fatal("expected pending review to keep campaign running")
	}

	summary.ReviewPendingCount = 0
	item.Status = campaign.StatusHold
	if shouldMarkCampaignCompleted(item, summary) {
		t.Fatal("expected hold status to skip auto-completion")
	}
}

func TestValidateCampaignRepoTaskCompletion_ExecutorValidationFailureBecomesRetrying(t *testing.T) {
	root := t.TempDir()
	mustWriteBootstrapTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
---
`)
	mustWriteBootstrapTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteBootstrapTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "task.md"), `---
task_id: T001
title: "Replay on remote host"
phase: P01
status: executing
owner_agent: executor
lease_until: "2026-04-02T12:00:00+08:00"
dispatch_state: executor_dispatched
review_status: pending
execution_round: 1
---
`)

	event, ok, err := validateCampaignRepoTaskCompletion(campaign.Campaign{
		ID:               "camp_demo",
		CampaignRepoPath: root,
	}, automation.Task{
		Action: automation.Action{
			StateKey: "campaign_dispatch:camp_demo:executor:T001:x1",
		},
	})
	if err != nil {
		t.Fatalf("validate campaign repo task completion failed: %v", err)
	}
	if !ok {
		t.Fatal("expected validation failure event to be returned")
	}
	if event.Kind != campaignrepo.EventTaskRetrying {
		t.Fatalf("expected task_retrying event, got %+v", event)
	}

	repo, err := campaignrepo.Load(root)
	if err != nil {
		t.Fatalf("load repo failed: %v", err)
	}
	task := repo.Tasks[0]
	if got := task.Frontmatter.Status; got != campaignrepo.TaskStatusReviewPending {
		t.Fatalf("expected task to enter review_pending, got %q", got)
	}
	if got := task.Frontmatter.DispatchState; got != "blocked_guidance_requested" {
		t.Fatalf("expected blocked guidance dispatch state, got %q", got)
	}
}

func TestRecoverCampaignRepoAfterStartup_ReactivatesInterruptedDispatchTask(t *testing.T) {
	root := t.TempDir()
	mustWriteBootstrapTestFile(t, filepath.Join(root, "campaign.md"), `---
campaign_id: camp_demo
title: "Demo Campaign"
current_phase: P01
plan_status: human_approved
---
`)
	mustWriteBootstrapTestFile(t, filepath.Join(root, "phases", "P01", "phase.md"), `---
phase: P01
status: active
goal: "Ship the first phase"
---
`)
	mustWriteBootstrapTestFile(t, filepath.Join(root, "phases", "P01", "tasks", "T001", "task.md"), `---
task_id: T001
title: "Replay on remote host"
phase: P01
status: executing
owner_agent: executor
lease_until: "2026-04-02T12:00:00+08:00"
dispatch_state: executor_dispatched
review_status: pending
execution_round: 1
---
`)

	automationStore := automation.NewStore(filepath.Join(t.TempDir(), "automation.db"))
	campaignStore := campaign.NewStore(filepath.Join(t.TempDir(), "campaigns.db"))
	builder := &connectorRuntimeBuilder{
		automationStore: automationStore,
		campaignStore:   campaignStore,
	}
	now := time.Date(2026, 4, 2, 9, 0, 0, 0, time.Local)

	_, err := campaignStore.CreateCampaign(campaign.Campaign{
		ID:               "camp_demo",
		Title:            "Demo Campaign",
		Objective:        "Ship the first phase",
		CampaignRepoPath: root,
		Session: campaign.SessionRoute{
			ScopeKey:      "chat_id:oc_chat|scene:work|thread:omt_demo",
			ReceiveIDType: "chat_id",
			ReceiveID:     "oc_chat",
			ChatType:      "group",
		},
		Creator: campaign.Actor{
			OpenID: "ou_actor",
		},
		Status:            campaign.StatusRunning,
		MaxParallelTrials: 1,
	})
	if err != nil {
		t.Fatalf("create campaign failed: %v", err)
	}
	result, err := campaignrepo.ReconcileAndPrepare(root, now, 1, 2*time.Hour)
	if err != nil {
		t.Fatalf("reconcile and prepare failed: %v", err)
	}
	if len(result.DispatchTasks) != 1 || result.DispatchTasks[0].StateKey != "campaign_dispatch:camp_demo:executor:T001:x1" {
		t.Fatalf("expected startup fixture to produce one executor dispatch, got %+v summary=%+v", result.DispatchTasks, result.Summary)
	}

	created, err := automationStore.CreateTask(automation.Task{
		Title: "Demo Campaign · T001 · 执行 · 第 1 轮",
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
			Type:       automation.ActionTypeRunWorkflow,
			Prompt:     "stale prompt",
			Workflow:   "code_army",
			StateKey:   "campaign_dispatch:camp_demo:executor:T001:x1",
			SessionKey: "chat_id:oc_chat|scene:work|thread:omt_demo",
		},
		Status:    automation.TaskStatusActive,
		MaxRuns:   1,
		NextRunAt: now.Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("create interrupted dispatch task failed: %v", err)
	}
	claimed, err := automationStore.ClaimDueTasks(now, 10)
	if err != nil {
		t.Fatalf("claim due tasks failed: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != created.ID {
		t.Fatalf("expected to claim the interrupted dispatch task, got %+v", claimed)
	}
	if err := automationStore.ResetRunningTasks(); err != nil {
		t.Fatalf("reset running tasks failed: %v", err)
	}

	builder.recoverCampaignRepoAfterStartup()

	updated, err := automationStore.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get updated task failed: %v", err)
	}
	if updated.Status != automation.TaskStatusActive {
		t.Fatalf("expected recovered dispatch task to stay active, got %s", updated.Status)
	}
	if updated.RunCount != 0 {
		t.Fatalf("expected recovered dispatch task run_count reset, got %d", updated.RunCount)
	}
	if updated.NextRunAt.IsZero() {
		t.Fatal("expected recovered dispatch task to be runnable again")
	}
	if updated.LastResult != "" {
		t.Fatalf("expected recovered dispatch task to clear last result, got %q", updated.LastResult)
	}
	if strings.TrimSpace(updated.Action.Prompt) == "stale prompt" {
		t.Fatalf("expected startup recovery to refresh dispatch prompt, got %q", updated.Action.Prompt)
	}
}

func TestNewCampaignCompletedEvent(t *testing.T) {
	item := campaign.Campaign{
		ID:     "camp_demo",
		Title:  "Demo Campaign",
		Status: campaign.StatusRunning,
	}
	summary := campaignrepo.Summary{
		TaskCount:           3,
		AcceptedCount:       1,
		DoneCount:           2,
		RejectedCount:       0,
		ActiveCount:         0,
		ReadyCount:          0,
		ReworkCount:         0,
		ReviewPendingCount:  0,
		ReviewingCount:      0,
		SelectedReadyCount:  0,
		SelectedReviewCount: 0,
		BlockedCount:        0,
		WaitingCount:        0,
	}

	event, ok := newCampaignCompletedEvent(item, summary)
	if !ok {
		t.Fatal("expected terminal summary to emit completion event")
	}
	if event.Kind != campaignrepo.EventCampaignCompleted {
		t.Fatalf("unexpected event kind: %q", event.Kind)
	}
	if event.Title != "全部运行结束" {
		t.Fatalf("unexpected event title: %q", event.Title)
	}
	if !strings.Contains(event.Detail, "runtime 状态已更新为 `completed`") {
		t.Fatalf("unexpected event detail: %q", event.Detail)
	}

	item.Status = campaign.StatusHold
	if _, ok := newCampaignCompletedEvent(item, summary); ok {
		t.Fatal("expected hold campaign to skip completion event")
	}
}
