package campaignrepo

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type BlockedTaskOutcome struct {
	GuidanceRequested bool
	TerminalBlocked   bool
	GuidanceAttempt   int
	Reason            string
}

// MarkTaskBlocked loads the campaign repo, finds the task by ID, marks it as blocked,
// and persists the change as a terminal blocked state.
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
		task.Frontmatter.DispatchState = "signal_blocked_terminal"
		task.Frontmatter.ReviewStatus = "blocked"
		recordBlockedReason(task, reason)
		clearTaskAssignment(task)
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

// HandleTaskBlocked records an executor-side block. It routes the task through reviewer
// guidance for the first few blocked attempts, then falls back to a terminal blocked state.
func HandleTaskBlocked(root, taskID, reason string) (BlockedTaskOutcome, error) {
	root = strings.TrimSpace(root)
	taskID = strings.TrimSpace(taskID)
	if root == "" || taskID == "" {
		return BlockedTaskOutcome{}, nil
	}
	repo, err := Load(root)
	if err != nil {
		return BlockedTaskOutcome{}, fmt.Errorf("load repo failed: %w", err)
	}
	for idx := range repo.Tasks {
		if strings.TrimSpace(repo.Tasks[idx].Frontmatter.TaskID) != taskID {
			continue
		}
		task := &repo.Tasks[idx]
		status := normalizeTaskStatus(task.Frontmatter.Status)
		switch status {
		case TaskStatusExecuting, TaskStatusReady, TaskStatusRework, TaskStatusReviewPending, TaskStatusReviewing, TaskStatusBlocked:
		default:
			return BlockedTaskOutcome{}, nil
		}
		outcome := applyBlockedGuidanceTransition(task, reason)
		if err := writeTaskDocument(root, *task); err != nil {
			return BlockedTaskOutcome{}, fmt.Errorf("write task document failed: %w", err)
		}
		return outcome, nil
	}
	_ = filepath.ToSlash(root)
	return BlockedTaskOutcome{}, nil
}

func applyBlockedGuidanceTransition(task *TaskDocument, reason string) BlockedTaskOutcome {
	if task == nil {
		return BlockedTaskOutcome{}
	}
	recordBlockedReason(task, reason)
	clearTaskAssignment(task)
	if task.Frontmatter.BlockGuidanceCount < maxBlockedGuidanceRetries {
		task.Frontmatter.BlockGuidanceCount++
		task.Frontmatter.Status = TaskStatusReviewPending
		task.Frontmatter.DispatchState = "blocked_guidance_requested"
		task.Frontmatter.ReviewStatus = "pending"
		return BlockedTaskOutcome{
			GuidanceRequested: true,
			GuidanceAttempt:   task.Frontmatter.BlockGuidanceCount,
			Reason:            task.Frontmatter.LastBlockedReason,
		}
	}
	task.Frontmatter.Status = TaskStatusBlocked
	task.Frontmatter.DispatchState = "signal_blocked_terminal"
	task.Frontmatter.ReviewStatus = "blocked"
	return BlockedTaskOutcome{
		TerminalBlocked: true,
		GuidanceAttempt: task.Frontmatter.BlockGuidanceCount,
		Reason:          task.Frontmatter.LastBlockedReason,
	}
}

func clearTaskAssignment(task *TaskDocument) {
	if task == nil {
		return
	}
	task.Frontmatter.OwnerAgent = ""
	task.LeaseUntil = time.Time{}
	task.WakeAt = time.Time{}
	task.Frontmatter.WakePrompt = ""
}

func recordBlockedReason(task *TaskDocument, reason string) {
	if task == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = strings.TrimSpace(task.Frontmatter.LastBlockedReason)
	}
	task.Frontmatter.LastBlockedReason = reason
	if reason == "" {
		return
	}
	blockSection := "## Blocked\n\n" + reason
	if strings.Contains(task.Body, blockSection) {
		return
	}
	if task.Body == "" {
		task.Body = blockSection
		return
	}
	task.Body = strings.TrimRight(task.Body, "\n") + "\n\n" + blockSection
}

func shouldRequestBlockedGuidance(task TaskDocument) bool {
	if normalizeTaskStatus(task.Frontmatter.Status) != TaskStatusBlocked {
		return false
	}
	if task.Frontmatter.BlockGuidanceCount >= maxBlockedGuidanceRetries {
		return false
	}
	if normalizeReviewStatus(task.Frontmatter.ReviewStatus) == "blocked" {
		return false
	}
	if strings.TrimSpace(task.Frontmatter.LastBlockedReason) == "" {
		return false
	}
	if strings.TrimSpace(task.Frontmatter.OwnerAgent) != "" || !task.LeaseUntil.IsZero() {
		return false
	}
	if task.Frontmatter.ExecutionRound <= 0 {
		return false
	}
	switch strings.TrimSpace(task.Frontmatter.DispatchState) {
	case "", "executor_dispatched", "signal_blocked", "signal_blocked_terminal", "executor_completed", "wake_resumed":
		return true
	default:
		return false
	}
}

func taskInBlockedGuidanceLoop(task TaskDocument) bool {
	if task.Frontmatter.BlockGuidanceCount <= 0 || strings.TrimSpace(task.Frontmatter.LastBlockedReason) == "" {
		return false
	}
	switch strings.TrimSpace(task.Frontmatter.DispatchState) {
	case "blocked_guidance_requested", "blocked_guidance_applied":
		return true
	default:
		return false
	}
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
