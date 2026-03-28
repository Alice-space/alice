package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

type runtimeAutomationClientStub struct {
	listPayload map[string]any
	created     []runtimeapi.CreateTaskRequest
	patched     []patchedTaskCall
	deleted     []string
}

type patchedTaskCall struct {
	taskID      string
	contentType string
	body        []byte
}

func (s *runtimeAutomationClientStub) ListTasks(context.Context, mcpbridge.SessionContext, string, int) (map[string]any, error) {
	if s.listPayload == nil {
		return map[string]any{"status": "ok", "tasks": []automation.Task{}}, nil
	}
	return s.listPayload, nil
}

func (s *runtimeAutomationClientStub) CreateTask(_ context.Context, _ mcpbridge.SessionContext, req runtimeapi.CreateTaskRequest) (map[string]any, error) {
	s.created = append(s.created, req)
	return map[string]any{"status": "ok"}, nil
}

func (s *runtimeAutomationClientStub) PatchTask(_ context.Context, _ mcpbridge.SessionContext, taskID string, contentType string, patchBody []byte) (map[string]any, error) {
	s.patched = append(s.patched, patchedTaskCall{taskID: taskID, contentType: contentType, body: patchBody})
	return map[string]any{"status": "ok"}, nil
}

func (s *runtimeAutomationClientStub) DeleteTask(_ context.Context, _ mcpbridge.SessionContext, taskID string) (map[string]any, error) {
	s.deleted = append(s.deleted, taskID)
	return map[string]any{"status": "ok"}, nil
}

func TestSyncRuntimeDispatchTasksCreatesPlannerTask(t *testing.T) {
	now := time.Date(2026, 3, 25, 9, 0, 0, 0, time.UTC)
	client := &runtimeAutomationClientStub{}
	item := campaign.Campaign{ID: "camp_demo", ManageMode: campaign.ManageModeCreatorOnly}
	specs := []campaignrepo.DispatchTaskSpec{
		{
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Kind:     campaignrepo.DispatchKindPlanner,
			TaskID:   "plan-r1",
			Title:    "campaign planner camp_demo r1",
			RunAt:    now,
			Prompt:   "plan this campaign",
			Role: campaignrepo.RoleConfig{
				Provider:        "claude",
				Workflow:        "code_army",
				ReasoningEffort: "high",
				Personality:     "analytical",
			},
		},
	}

	synced, err := syncRuntimeDispatchTasks(context.Background(), client, mcpbridge.SessionContext{}, item, specs)
	if err != nil {
		t.Fatalf("syncRuntimeDispatchTasks returned err=%v", err)
	}
	if synced != 1 {
		t.Fatalf("expected synced=1, got %d", synced)
	}
	if len(client.created) != 1 {
		t.Fatalf("expected one created task, got %d", len(client.created))
	}
	req := client.created[0]
	if req.Title != "campaign planner camp_demo r1" {
		t.Fatalf("unexpected title: %q", req.Title)
	}
	if req.Action.Provider != "claude" {
		t.Fatalf("unexpected provider: %q", req.Action.Provider)
	}
	if req.Action.Workflow != "code_army" {
		t.Fatalf("unexpected workflow: %q", req.Action.Workflow)
	}
	if req.Action.StateKey != "campaign_dispatch:camp_demo:planner:r1" {
		t.Fatalf("unexpected state key: %q", req.Action.StateKey)
	}
	if req.MaxRuns != 1 {
		t.Fatalf("unexpected max runs: %d", req.MaxRuns)
	}
	if !req.NextRunAt.Equal(now) {
		t.Fatalf("unexpected next_run_at: got=%s want=%s", req.NextRunAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}
}

func TestSyncRuntimeDispatchTasksReusesMatchingActiveTask(t *testing.T) {
	existing := automation.Task{
		ID:    "task_existing",
		Title: "campaign planner camp_demo r1",
		Action: automation.Action{
			Prompt:          "plan this campaign",
			Provider:        "claude",
			Workflow:        "code_army",
			StateKey:        "campaign_dispatch:camp_demo:planner:r1",
			ReasoningEffort: "high",
			Personality:     "analytical",
		},
		Status: automation.TaskStatusActive,
	}
	client := &runtimeAutomationClientStub{
		listPayload: map[string]any{"status": "ok", "tasks": []automation.Task{existing}},
	}
	item := campaign.Campaign{ID: "camp_demo", ManageMode: campaign.ManageModeCreatorOnly}
	specs := []campaignrepo.DispatchTaskSpec{
		{
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Kind:     campaignrepo.DispatchKindPlanner,
			TaskID:   "plan-r1",
			Title:    "campaign planner camp_demo r1",
			Prompt:   "plan this campaign",
			Role: campaignrepo.RoleConfig{
				Provider:        "claude",
				Workflow:        "code_army",
				ReasoningEffort: "high",
				Personality:     "analytical",
			},
		},
	}

	synced, err := syncRuntimeDispatchTasks(context.Background(), client, mcpbridge.SessionContext{}, item, specs)
	if err != nil {
		t.Fatalf("syncRuntimeDispatchTasks returned err=%v", err)
	}
	if synced != 1 {
		t.Fatalf("expected synced=1, got %d", synced)
	}
	if len(client.created) != 0 {
		t.Fatalf("expected no created tasks, got %d", len(client.created))
	}
	if len(client.patched) != 0 {
		t.Fatalf("expected no patched tasks, got %d", len(client.patched))
	}
	if len(client.deleted) != 0 {
		t.Fatalf("expected no deleted tasks, got %d", len(client.deleted))
	}
}

func TestSyncRuntimeDispatchTasksKeepsRecentlyFailedDispatchTask(t *testing.T) {
	existing := automation.Task{
		ID:         "task_failed_recently",
		Title:      "campaign planner camp_demo r1",
		Status:     automation.TaskStatusPaused,
		RunCount:   1,
		UpdatedAt:  time.Now().Add(-30 * time.Second),
		LastResult: "error: planner failed",
		Action: automation.Action{
			Prompt:          "plan this campaign",
			Provider:        "claude",
			Workflow:        "code_army",
			StateKey:        "campaign_dispatch:camp_demo:planner:r1",
			ReasoningEffort: "high",
			Personality:     "analytical",
		},
	}
	client := &runtimeAutomationClientStub{
		listPayload: map[string]any{"status": "ok", "tasks": []automation.Task{existing}},
	}
	item := campaign.Campaign{ID: "camp_demo", ManageMode: campaign.ManageModeCreatorOnly}
	specs := []campaignrepo.DispatchTaskSpec{
		{
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Kind:     campaignrepo.DispatchKindPlanner,
			TaskID:   "plan-r1",
			Title:    "campaign planner camp_demo r1",
			Prompt:   "plan this campaign",
			Role: campaignrepo.RoleConfig{
				Provider:        "claude",
				Workflow:        "code_army",
				ReasoningEffort: "high",
				Personality:     "analytical",
			},
		},
	}

	synced, err := syncRuntimeDispatchTasks(context.Background(), client, mcpbridge.SessionContext{}, item, specs)
	if err != nil {
		t.Fatalf("syncRuntimeDispatchTasks returned err=%v", err)
	}
	if synced != 1 {
		t.Fatalf("expected synced=1, got %d", synced)
	}
	if len(client.created) != 0 {
		t.Fatalf("expected no created tasks during failure cooldown, got %d", len(client.created))
	}
	if len(client.patched) != 0 {
		t.Fatalf("expected no patched tasks during failure cooldown, got %d", len(client.patched))
	}
	if len(client.deleted) != 0 {
		t.Fatalf("expected no deleted tasks during failure cooldown, got %#v", client.deleted)
	}
}

func TestSyncRuntimeDispatchTasksDeletesStaleDispatchTask(t *testing.T) {
	existing := automation.Task{
		ID:    "task_stale",
		Title: "old dispatch",
		Action: automation.Action{
			StateKey: "campaign_dispatch:camp_demo:planner:r0",
		},
		Status: automation.TaskStatusActive,
	}
	client := &runtimeAutomationClientStub{
		listPayload: map[string]any{"status": "ok", "tasks": []automation.Task{existing}},
	}
	item := campaign.Campaign{ID: "camp_demo", ManageMode: campaign.ManageModeCreatorOnly}

	synced, err := syncRuntimeDispatchTasks(context.Background(), client, mcpbridge.SessionContext{}, item, nil)
	if err != nil {
		t.Fatalf("syncRuntimeDispatchTasks returned err=%v", err)
	}
	if synced != 0 {
		t.Fatalf("expected synced=0, got %d", synced)
	}
	if len(client.deleted) != 1 || client.deleted[0] != "task_stale" {
		t.Fatalf("expected stale task to be deleted, got %#v", client.deleted)
	}
}

func TestSyncRuntimeDispatchTasksIgnoresDeletedDispatchTask(t *testing.T) {
	existing := automation.Task{
		ID:    "task_deleted",
		Title: "campaign planner camp_demo r1",
		Action: automation.Action{
			Prompt:          "old failed plan",
			Provider:        "claude",
			Workflow:        "code_army",
			StateKey:        "campaign_dispatch:camp_demo:planner:r1",
			ReasoningEffort: "high",
			Personality:     "analytical",
		},
		Status: automation.TaskStatusDeleted,
	}
	client := &runtimeAutomationClientStub{
		listPayload: map[string]any{"status": "ok", "tasks": []automation.Task{existing}},
	}
	item := campaign.Campaign{ID: "camp_demo", ManageMode: campaign.ManageModeCreatorOnly}
	specs := []campaignrepo.DispatchTaskSpec{
		{
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Kind:     campaignrepo.DispatchKindPlanner,
			TaskID:   "plan-r1",
			Title:    "campaign planner camp_demo r1",
			Prompt:   "plan this campaign",
			Role: campaignrepo.RoleConfig{
				Provider:        "claude",
				Workflow:        "code_army",
				ReasoningEffort: "high",
				Personality:     "pragmatic",
			},
		},
	}

	synced, err := syncRuntimeDispatchTasks(context.Background(), client, mcpbridge.SessionContext{}, item, specs)
	if err != nil {
		t.Fatalf("syncRuntimeDispatchTasks returned err=%v", err)
	}
	if synced != 1 {
		t.Fatalf("expected synced=1, got %d", synced)
	}
	if len(client.created) != 1 {
		t.Fatalf("expected one created task, got %d", len(client.created))
	}
	if len(client.patched) != 0 {
		t.Fatalf("expected no patched tasks, got %d", len(client.patched))
	}
}

func TestBuildRuntimeDispatchPatchIncludesRunAt(t *testing.T) {
	runAt := time.Date(2026, 3, 25, 10, 30, 0, 0, time.UTC)
	body, err := buildRuntimeDispatchPatch(
		campaign.Campaign{ManageMode: campaign.ManageModeCreatorOnly},
		campaignrepo.DispatchTaskSpec{
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Kind:     campaignrepo.DispatchKindPlanner,
			TaskID:   "plan-r1",
			Title:    "campaign planner camp_demo r1",
			RunAt:    runAt,
			Prompt:   "plan this campaign",
			Role: campaignrepo.RoleConfig{
				Provider:        "claude",
				Workflow:        "code_army",
				ReasoningEffort: "high",
				Personality:     "analytical",
			},
		},
	)
	if err != nil {
		t.Fatalf("buildRuntimeDispatchPatch returned err=%v", err)
	}
	var patch map[string]any
	if err := json.Unmarshal(body, &patch); err != nil {
		t.Fatalf("unmarshal patch failed: %v", err)
	}
	if patch["status"] != string(automation.TaskStatusActive) {
		t.Fatalf("unexpected status: %#v", patch["status"])
	}
	if patch["next_run_at"] == "" {
		t.Fatal("expected next_run_at to be present")
	}
}

func TestSyncRuntimeDispatchTasksRecreatesCompletedDispatchTask(t *testing.T) {
	existing := automation.Task{
		ID:    "task_done_once",
		Title: "campaign planner camp_demo r1",
		Action: automation.Action{
			Prompt:          "plan this campaign",
			Provider:        "claude",
			Workflow:        "code_army",
			StateKey:        "campaign_dispatch:camp_demo:planner:r1",
			ReasoningEffort: "high",
			Personality:     "analytical",
		},
		Status:   automation.TaskStatusPaused,
		RunCount: 1,
	}
	client := &runtimeAutomationClientStub{
		listPayload: map[string]any{"status": "ok", "tasks": []automation.Task{existing}},
	}
	item := campaign.Campaign{ID: "camp_demo", ManageMode: campaign.ManageModeCreatorOnly}
	specs := []campaignrepo.DispatchTaskSpec{
		{
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Kind:     campaignrepo.DispatchKindPlanner,
			TaskID:   "plan-r1",
			Title:    "campaign planner camp_demo r1",
			RunAt:    time.Date(2026, 3, 25, 9, 0, 0, 0, time.UTC),
			Prompt:   "plan this campaign",
			Role: campaignrepo.RoleConfig{
				Provider:        "claude",
				Workflow:        "code_army",
				ReasoningEffort: "high",
				Personality:     "analytical",
			},
		},
	}

	synced, err := syncRuntimeDispatchTasks(context.Background(), client, mcpbridge.SessionContext{}, item, specs)
	if err != nil {
		t.Fatalf("syncRuntimeDispatchTasks returned err=%v", err)
	}
	if synced != 1 {
		t.Fatalf("expected synced=1, got %d", synced)
	}
	if len(client.deleted) != 1 || client.deleted[0] != "task_done_once" {
		t.Fatalf("expected completed task to be deleted first, got %#v", client.deleted)
	}
	if len(client.created) != 1 {
		t.Fatalf("expected one recreated task, got %d", len(client.created))
	}
	if len(client.patched) != 0 {
		t.Fatalf("expected no patch calls for completed task recreation, got %d", len(client.patched))
	}
}
