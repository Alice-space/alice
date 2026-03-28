package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/campaignrepo"
	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/runtimeapi"
)

const (
	runtimeCampaignDispatchTaskLimit   = 200
	runtimeCampaignDispatchEverySecs   = 60
	runtimeDispatchFailureCooldown     = 1 * time.Minute
	runtimeCampaignDispatchStatePrefix = "campaign_dispatch:"
)

type runtimeAutomationClient interface {
	ListTasks(ctx context.Context, session mcpbridge.SessionContext, status string, limit int) (map[string]any, error)
	CreateTask(ctx context.Context, session mcpbridge.SessionContext, req runtimeapi.CreateTaskRequest) (map[string]any, error)
	PatchTask(ctx context.Context, session mcpbridge.SessionContext, taskID string, contentType string, patchBody []byte) (map[string]any, error)
	DeleteTask(ctx context.Context, session mcpbridge.SessionContext, taskID string) (map[string]any, error)
}

func syncRuntimeDispatchTasks(
	ctx context.Context,
	client runtimeAutomationClient,
	session mcpbridge.SessionContext,
	item campaign.Campaign,
	specs []campaignrepo.DispatchTaskSpec,
) (int, error) {
	if client == nil {
		return 0, nil
	}

	statePrefix := runtimeCampaignDispatchStatePrefix + strings.TrimSpace(item.ID) + ":"
	listPayload, err := client.ListTasks(ctx, session, "all", runtimeCampaignDispatchTaskLimit)
	if err != nil {
		return 0, err
	}
	tasks, err := decodeRuntimeAutomationTasks(listPayload)
	if err != nil {
		return 0, err
	}

	existingByState := make(map[string]automation.Task)
	for _, task := range tasks {
		task = automation.NormalizeTask(task)
		if task.Status == automation.TaskStatusDeleted {
			continue
		}
		stateKey := strings.TrimSpace(task.Action.StateKey)
		if !strings.HasPrefix(stateKey, statePrefix) {
			continue
		}
		existingByState[stateKey] = task
	}

	desired := make(map[string]struct{}, len(specs))
	synced := 0
	now := time.Now().Local()
	for _, spec := range specs {
		stateKey := strings.TrimSpace(spec.StateKey)
		if stateKey == "" {
			continue
		}
		desired[stateKey] = struct{}{}

		if existing, ok := existingByState[stateKey]; ok {
			if shouldKeepRuntimeDispatchTask(existing, spec, now) {
				synced++
				continue
			}
			if existing.Status != automation.TaskStatusActive {
				if _, err := client.DeleteTask(ctx, session, existing.ID); err != nil {
					return synced, err
				}
				if _, err := client.CreateTask(ctx, session, buildRuntimeDispatchCreateRequest(item, spec)); err != nil {
					return synced, err
				}
				synced++
				continue
			}
			patchBody, err := buildRuntimeDispatchPatch(item, spec)
			if err != nil {
				return synced, err
			}
			if _, err := client.PatchTask(ctx, session, existing.ID, "application/merge-patch+json", patchBody); err != nil {
				return synced, err
			}
			synced++
			continue
		}

		if _, err := client.CreateTask(ctx, session, buildRuntimeDispatchCreateRequest(item, spec)); err != nil {
			return synced, err
		}
		synced++
	}

	for stateKey, task := range existingByState {
		if _, ok := desired[stateKey]; ok {
			continue
		}
		if task.Status == automation.TaskStatusDeleted {
			continue
		}
		if _, err := client.DeleteTask(ctx, session, task.ID); err != nil {
			return synced, err
		}
	}

	return synced, nil
}

func decodeRuntimeAutomationTasks(payload map[string]any) ([]automation.Task, error) {
	raw, ok := payload["tasks"]
	if !ok || raw == nil {
		return nil, nil
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var tasks []automation.Task
	if err := json.Unmarshal(body, &tasks); err != nil {
		return nil, err
	}
	for i := range tasks {
		tasks[i] = automation.NormalizeTask(tasks[i])
	}
	return tasks, nil
}

func shouldKeepRuntimeDispatchTask(task automation.Task, spec campaignrepo.DispatchTaskSpec, now time.Time) bool {
	task = automation.NormalizeTask(task)
	if strings.TrimSpace(task.Action.Prompt) != strings.TrimSpace(spec.Prompt) {
		return false
	}
	if strings.TrimSpace(task.Title) != strings.TrimSpace(spec.Title) {
		return false
	}
	if strings.TrimSpace(task.Action.Provider) != strings.TrimSpace(spec.Role.Provider) {
		return false
	}
	if strings.TrimSpace(task.Action.Model) != strings.TrimSpace(spec.Role.Model) {
		return false
	}
	if strings.TrimSpace(task.Action.Profile) != strings.TrimSpace(spec.Role.Profile) {
		return false
	}
	if strings.TrimSpace(task.Action.Workflow) != strings.TrimSpace(spec.Role.Workflow) {
		return false
	}
	if strings.TrimSpace(task.Action.ReasoningEffort) != strings.TrimSpace(spec.Role.ReasoningEffort) {
		return false
	}
	if strings.TrimSpace(task.Action.Personality) != strings.TrimSpace(spec.Role.Personality) {
		return false
	}
	if task.Status == automation.TaskStatusActive {
		return true
	}
	if task.Status != automation.TaskStatusPaused {
		return false
	}
	if !strings.HasPrefix(strings.TrimSpace(task.LastResult), "error:") {
		return false
	}
	if task.UpdatedAt.IsZero() {
		return false
	}
	return task.UpdatedAt.Add(runtimeDispatchFailureCooldown).After(now.Local())
}

func buildRuntimeDispatchCreateRequest(item campaign.Campaign, spec campaignrepo.DispatchTaskSpec) runtimeapi.CreateTaskRequest {
	return runtimeapi.CreateTaskRequest{
		Title:      spec.Title,
		ManageMode: runtimeManageModeFromCampaign(item),
		Schedule: automation.Schedule{
			Type:         automation.ScheduleTypeInterval,
			EverySeconds: runtimeCampaignDispatchEverySecs,
		},
		Action: automation.Action{
			Type:            automation.ActionTypeRunWorkflow,
			Text:            fmt.Sprintf("Scheduled %s for `%s`", spec.Kind, spec.TaskID),
			Prompt:          spec.Prompt,
			Provider:        spec.Role.Provider,
			Model:           spec.Role.Model,
			Profile:         spec.Role.Profile,
			Workflow:        spec.Role.Workflow,
			StateKey:        spec.StateKey,
			ReasoningEffort: spec.Role.ReasoningEffort,
			Personality:     spec.Role.Personality,
		},
		MaxRuns:   1,
		NextRunAt: spec.RunAt,
	}
}

func buildRuntimeDispatchPatch(item campaign.Campaign, spec campaignrepo.DispatchTaskSpec) ([]byte, error) {
	patch := map[string]any{
		"title":       spec.Title,
		"manage_mode": runtimeManageModeFromCampaign(item),
		"schedule": automation.Schedule{
			Type:         automation.ScheduleTypeInterval,
			EverySeconds: runtimeCampaignDispatchEverySecs,
		},
		"action": automation.Action{
			Type:            automation.ActionTypeRunWorkflow,
			Text:            fmt.Sprintf("Scheduled %s for `%s`", spec.Kind, spec.TaskID),
			Prompt:          spec.Prompt,
			Provider:        spec.Role.Provider,
			Model:           spec.Role.Model,
			Profile:         spec.Role.Profile,
			Workflow:        spec.Role.Workflow,
			StateKey:        spec.StateKey,
			ReasoningEffort: spec.Role.ReasoningEffort,
			Personality:     spec.Role.Personality,
		},
		"max_runs":    1,
		"status":      automation.TaskStatusActive,
		"next_run_at": spec.RunAt,
	}
	return json.Marshal(patch)
}

func runtimeManageModeFromCampaign(item campaign.Campaign) automation.ManageMode {
	if item.ManageMode == campaign.ManageModeScopeAll {
		return automation.ManageModeScopeAll
	}
	return automation.ManageModeCreatorOnly
}
