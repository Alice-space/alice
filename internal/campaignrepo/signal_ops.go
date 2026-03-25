package campaignrepo

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// MarkTaskBlocked loads the campaign repo, finds the task by ID, marks it as blocked,
// and persists the change.
func MarkTaskBlocked(root, taskID, reason string) error {
	root = strings.TrimSpace(root)
	taskID = strings.TrimSpace(taskID)
	if root == "" || taskID == "" {
		return nil
	}
	repo, err := Load(root)
	if err != nil {
		return fmt.Errorf("load repo failed: %w", err)
	}
	found := false
	for idx := range repo.Tasks {
		if strings.TrimSpace(repo.Tasks[idx].Frontmatter.TaskID) != taskID {
			continue
		}
		task := &repo.Tasks[idx]
		status := normalizeTaskStatus(task.Frontmatter.Status)
		// Only mark as blocked if task is currently executing or rework — don't override terminal states.
		switch status {
		case TaskStatusExecuting, TaskStatusReady, TaskStatusRework, TaskStatusReviewPending, TaskStatusReviewing:
		default:
			continue
		}
		task.Frontmatter.Status = TaskStatusBlocked
		task.Frontmatter.DispatchState = "signal_blocked"
		task.Frontmatter.OwnerAgent = ""
		task.LeaseUntil = time.Time{}
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		// Append blocked reason to body
		reason = strings.TrimSpace(reason)
		if reason != "" {
			if task.Body == "" {
				task.Body = "## Blocked\n\n" + reason
			} else {
				task.Body = strings.TrimRight(task.Body, "\n") + "\n\n## Blocked\n\n" + reason
			}
		}
		if err := writeTaskDocument(root, *task); err != nil {
			return fmt.Errorf("write task document failed: %w", err)
		}
		found = true
		break
	}
	if !found {
		_ = filepath.ToSlash(root) // keep import
	}
	return nil
}

// ResetPlanForReplan resets the campaign plan_status to "planning" and increments plan_round,
// so the next reconciliation will dispatch a planner.
func ResetPlanForReplan(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	repo, err := Load(root)
	if err != nil {
		return fmt.Errorf("load repo for replan failed: %w", err)
	}
	planStatus := normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)
	// Only reset if currently in execution phase (human_approved or beyond)
	switch planStatus {
	case PlanStatusHumanApproved, PlanStatusIdle:
		// Fine — reset
	case PlanStatusPlanning, PlanStatusPlanReviewPending, PlanStatusPlanReviewing, PlanStatusPlanApproved:
		// Already in planning phase, no need to reset
		return nil
	default:
		// For any other state (including ""), reset to planning
	}
	if err := markCurrentProposalSuperseded(&repo); err != nil {
		return fmt.Errorf("mark proposal superseded failed: %w", err)
	}
	repo.Campaign.Frontmatter.PlanRound++
	repo.Campaign.Frontmatter.PlanStatus = PlanStatusPlanning
	_, err = persistCampaignDocument(&repo)
	return err
}
