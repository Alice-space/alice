package campaignrepo

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const defaultDispatchLease = 2 * time.Hour
const maxSummaryBlockAutoRetries = 3
const maxBlockedGuidanceRetries = 3

type ReconcileResult struct {
	Repository       Repository         `json:"repository"`
	Summary          Summary            `json:"summary"`
	DispatchTasks    []DispatchTaskSpec `json:"dispatch_tasks,omitempty"`
	Changed          bool               `json:"changed"`
	AppliedReviews   int                `json:"applied_reviews"`
	ClaimedExecutors int                `json:"claimed_executors"`
	ClaimedReviewers int                `json:"claimed_reviewers"`
	Events           []ReconcileEvent   `json:"events,omitempty"`
}

func ReconcileFromPath(root string, now time.Time, maxParallel int) (Repository, Summary, error) {
	result, err := ReconcileAndPrepare(root, now, maxParallel, defaultDispatchLease)
	if err != nil {
		return Repository{}, Summary{}, err
	}
	return result.Repository, result.Summary, nil
}

func ReconcileAndPrepare(root string, now time.Time, maxParallel int, leaseDuration time.Duration, roleDefaults ...CampaignRoleDefaults) (ReconcileResult, error) {
	if leaseDuration <= 0 {
		leaseDuration = defaultDispatchLease
	}
	repo, err := Load(root)
	if err != nil {
		return ReconcileResult{}, err
	}
	if len(roleDefaults) > 0 {
		repo.ConfigRoleDefaults = roleDefaults[0]
	}
	changed := false
	campaignID := strings.TrimSpace(repo.Campaign.Frontmatter.CampaignID)
	campaignTitle := strings.TrimSpace(repo.Campaign.Frontmatter.Title)
	var events []ReconcileEvent

	prevPlanStatus := normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)
	planChanged, err := reconcilePlanPhase(&repo, now, leaseDuration)
	if err != nil {
		return ReconcileResult{}, err
	}
	if planChanged {
		changed = true
		newPlanStatus := normalizePlanStatus(repo.Campaign.Frontmatter.PlanStatus)
		if e, ok := planTransitionEvent(campaignID, campaignTitle, prevPlanStatus, newPlanStatus, repo.Campaign.Frontmatter.PlanRound); ok {
			events = append(events, e)
		}
	}

	if isPlanningPhase(repo.Campaign.Frontmatter.PlanStatus) {
		summary := Summarize(repo, now, maxParallel)
		dispatchTasks, err := buildDispatchSpecs(repo, now)
		if err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{
			Repository:    repo,
			Summary:       summary,
			DispatchTasks: dispatchTasks,
			Changed:       changed,
			Events:        events,
		}, nil
	}

	appliedReviews, verdictEvents, err := applyReviewVerdicts(&repo, campaignID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if appliedReviews > 0 {
		changed = true
		events = append(events, verdictEvents...)
	}

	repairedReviewHandOffs, err := repairDanglingExecutorReviewHandOff(&repo)
	if err != nil {
		return ReconcileResult{}, err
	}
	if repairedReviewHandOffs > 0 {
		changed = true
	}

	repairedTasks, err := repairInactiveTaskState(&repo)
	if err != nil {
		return ReconcileResult{}, err
	}
	if repairedTasks > 0 {
		changed = true
	}

	blockGuidanceTasks, blockedEvents, err := requeueBlockedTasksForReviewerGuidance(&repo, campaignID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if blockGuidanceTasks > 0 {
		changed = true
		events = append(events, blockedEvents...)
	}

	autoRetriedTasks, err := retryReviewPendingArtifactBlockers(&repo)
	if err != nil {
		return ReconcileResult{}, err
	}
	if autoRetriedTasks > 0 {
		changed = true
	}

	summary := Summarize(repo, now, maxParallel)
	claimedExecutors, executorEvents, err := claimSelectedExecutorTasks(&repo, summary, now, leaseDuration, campaignID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if claimedExecutors > 0 {
		changed = true
		events = append(events, executorEvents...)
		summary = Summarize(repo, now, maxParallel)
	}

	claimedReviewers, reviewerEvents, err := claimSelectedReviewTasks(&repo, summary, now, leaseDuration, campaignID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if claimedReviewers > 0 {
		changed = true
		events = append(events, reviewerEvents...)
		summary = Summarize(repo, now, maxParallel)
	}

	dispatchTasks, err := buildDispatchSpecs(repo, now)
	if err != nil {
		return ReconcileResult{}, err
	}

	return ReconcileResult{
		Repository:       repo,
		Summary:          summary,
		DispatchTasks:    dispatchTasks,
		Changed:          changed,
		AppliedReviews:   appliedReviews,
		ClaimedExecutors: claimedExecutors,
		ClaimedReviewers: claimedReviewers,
		Events:           events,
	}, nil
}

func retryReviewPendingArtifactBlockers(repo *Repository) (int, error) {
	if repo == nil || len(repo.Tasks) == 0 {
		return 0, nil
	}
	sourceRepoByID := make(map[string]SourceRepoDocument, len(repo.SourceRepos))
	for _, repoDoc := range repo.SourceRepos {
		if repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID); repoID != "" {
			sourceRepoByID[repoID] = repoDoc
		}
	}
	retried := 0
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		if normalizeTaskStatus(task.Frontmatter.Status) != TaskStatusReviewPending {
			continue
		}
		if taskInBlockedGuidanceLoop(*task) {
			continue
		}
		reason := strings.TrimSpace(taskExecutionArtifactBlockReason(repo.Root, *task, sourceRepoByID))
		if reason == "" {
			if strings.TrimSpace(task.Frontmatter.LastBlockedReason) == "" && task.Frontmatter.AutoRetryCount == 0 {
				continue
			}
			task.Frontmatter.LastBlockedReason = ""
			task.Frontmatter.AutoRetryCount = 0
			if err := persistTaskDocument(repo, idx); err != nil {
				return retried, err
			}
			continue
		}
		task.Frontmatter.LastBlockedReason = reason
		if task.Frontmatter.AutoRetryCount >= maxSummaryBlockAutoRetries {
			if err := persistTaskDocument(repo, idx); err != nil {
				return retried, err
			}
			continue
		}
		task.Frontmatter.AutoRetryCount++
		task.Frontmatter.Status = TaskStatusRework
		task.Frontmatter.DispatchState = "auto_retry_requested"
		task.Frontmatter.ReviewStatus = "changes_requested"
		task.Frontmatter.OwnerAgent = ""
		task.LeaseUntil = time.Time{}
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if err := persistTaskDocument(repo, idx); err != nil {
			return retried, err
		}
		retried++
	}
	return retried, nil
}

func repairInactiveTaskState(repo *Repository) (int, error) {
	if repo == nil || len(repo.Tasks) == 0 {
		return 0, nil
	}
	repaired := 0
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		status := normalizeTaskStatus(task.Frontmatter.Status)
		changed := false

		if status != TaskStatusExecuting && status != TaskStatusReviewing {
			if strings.TrimSpace(task.Frontmatter.OwnerAgent) != "" {
				task.Frontmatter.OwnerAgent = ""
				changed = true
			}
			if !task.LeaseUntil.IsZero() {
				task.LeaseUntil = time.Time{}
				changed = true
			}
		}

		if status != TaskStatusWaitingExternal {
			if !task.WakeAt.IsZero() {
				task.WakeAt = time.Time{}
				changed = true
			}
			if strings.TrimSpace(task.Frontmatter.WakePrompt) != "" {
				task.Frontmatter.WakePrompt = ""
				changed = true
			}
		}

		if !changed {
			continue
		}
		if err := persistTaskDocument(repo, idx); err != nil {
			return repaired, err
		}
		repaired++
	}
	return repaired, nil
}

func planTransitionEvent(campaignID, campaignTitle, prevStatus, newStatus string, planRound int) (ReconcileEvent, bool) {
	roundStr := fmt.Sprintf("第 %d 轮", planRound)
	switch {
	case prevStatus == PlanStatusIdle && newStatus == PlanStatusPlanning:
		return ReconcileEvent{
			Kind:       EventPlanningStarted,
			CampaignID: campaignID,
			PlanRound:  planRound,
			Title:      "规划启动",
			Detail:     fmt.Sprintf("Campaign **%s** 开始规划（%s）", campaignTitle, roundStr),
			Severity:   "info",
		}, true
	case prevStatus == PlanStatusPlanning && newStatus == PlanStatusPlanReviewPending:
		return ReconcileEvent{
			Kind:       EventProposalSubmitted,
			CampaignID: campaignID,
			PlanRound:  planRound,
			Title:      "规划方案已提交",
			Detail:     fmt.Sprintf("Campaign **%s** 规划方案已提交，等待评审（%s）", campaignTitle, roundStr),
			Severity:   "info",
		}, true
	case (prevStatus == PlanStatusPlanReviewPending || prevStatus == PlanStatusPlanReviewing) && newStatus == PlanStatusPlanApproved:
		return ReconcileEvent{
			Kind:       EventHumanApprovalNeeded,
			CampaignID: campaignID,
			PlanRound:  planRound,
			Title:      "方案评审通过",
			Detail:     fmt.Sprintf("Campaign **%s** 规划方案评审通过，等待人工批准（%s）", campaignTitle, roundStr),
			Severity:   "success",
		}, true
	case (prevStatus == PlanStatusPlanReviewPending || prevStatus == PlanStatusPlanReviewing) && newStatus == PlanStatusPlanning:
		return ReconcileEvent{
			Kind:       EventPlanReviewVerdict,
			CampaignID: campaignID,
			PlanRound:  planRound,
			Title:      "方案评审未通过，重新规划",
			Detail:     fmt.Sprintf("Campaign **%s** 规划方案评审未通过，进入第 %d 轮规划", campaignTitle, planRound),
			Severity:   "warning",
		}, true
	case prevStatus == PlanStatusPlanApproved && newStatus == PlanStatusHumanApproved:
		return ReconcileEvent{
			Kind:       EventPlanApproved,
			CampaignID: campaignID,
			PlanRound:  planRound,
			Title:      "方案已批准，开始执行",
			Detail:     fmt.Sprintf("Campaign **%s** 规划方案已批准，任务即将分配执行", campaignTitle),
			Severity:   "success",
		}, true
	}
	return ReconcileEvent{}, false
}

func applyReviewVerdicts(repo *Repository, campaignID string) (int, []ReconcileEvent, error) {
	if repo == nil || len(repo.Reviews) == 0 || len(repo.Tasks) == 0 {
		return 0, nil, nil
	}
	reviewIndex := reviewsByTask(repo.Reviews)
	applied := 0
	var events []ReconcileEvent
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		status := normalizeTaskStatus(task.Frontmatter.Status)
		if status != TaskStatusReviewPending && status != TaskStatusReviewing {
			continue
		}
		review, ok := latestRelevantReview(*task, reviewIndex[strings.TrimSpace(task.Frontmatter.TaskID)])
		if !ok {
			continue
		}
		if filepath.ToSlash(strings.TrimSpace(task.Frontmatter.LastReviewPath)) == filepath.ToSlash(review.Path) {
			continue
		}
		verdict := normalizeReviewVerdict(review.Frontmatter.Verdict, review.Frontmatter.Blocking)
		if verdict == "" {
			continue
		}
		taskID := strings.TrimSpace(task.Frontmatter.TaskID)
		taskTitle := strings.TrimSpace(task.Frontmatter.Title)
		blockedGuidanceLoop := taskInBlockedGuidanceLoop(*task)
		switch verdict {
		case "approve":
			task.Frontmatter.Status = TaskStatusAccepted
			task.Frontmatter.ReviewStatus = "approved"
			events = append(events, ReconcileEvent{
				Kind:       EventReviewVerdictApplied,
				CampaignID: campaignID,
				TaskID:     taskID,
				Title:      "评审通过",
				Detail:     fmt.Sprintf("任务 **%s** %s 评审通过，已接受", taskID, taskTitle),
				Severity:   "success",
			})
		case "blocking":
			if blockedGuidanceLoop {
				task.Frontmatter.Status = TaskStatusRework
				task.Frontmatter.ReviewStatus = "changes_requested"
				events = append(events, ReconcileEvent{
					Kind:       EventReviewVerdictApplied,
					CampaignID: campaignID,
					TaskID:     taskID,
					Title:      "阻塞指导返回执行",
					Detail:     fmt.Sprintf("任务 **%s** %s 的 reviewer 已给出阻塞恢复指导，返回 executor 继续尝试（第 %d/%d 次）", taskID, taskTitle, task.Frontmatter.BlockGuidanceCount, maxBlockedGuidanceRetries),
					Severity:   "warning",
				})
				break
			}
			task.Frontmatter.Status = TaskStatusBlocked
			task.Frontmatter.ReviewStatus = "blocked"
			events = append(events, ReconcileEvent{
				Kind:       EventReviewVerdictApplied,
				CampaignID: campaignID,
				TaskID:     taskID,
				Title:      "评审阻塞",
				Detail:     fmt.Sprintf("任务 **%s** %s 评审结果：阻塞", taskID, taskTitle),
				Severity:   "error",
			})
		case "reject":
			task.Frontmatter.Status = TaskStatusRejected
			task.Frontmatter.ReviewStatus = "blocked"
			events = append(events, ReconcileEvent{
				Kind:       EventReviewVerdictApplied,
				CampaignID: campaignID,
				TaskID:     taskID,
				Title:      "评审拒绝",
				Detail:     fmt.Sprintf("任务 **%s** %s 评审结果：拒绝", taskID, taskTitle),
				Severity:   "error",
			})
		default:
			task.Frontmatter.Status = TaskStatusRework
			task.Frontmatter.ReviewStatus = "changes_requested"
			events = append(events, ReconcileEvent{
				Kind:       EventReviewVerdictApplied,
				CampaignID: campaignID,
				TaskID:     taskID,
				Title:      "需要返工",
				Detail:     fmt.Sprintf("任务 **%s** %s 评审结果：需要修改", taskID, taskTitle),
				Severity:   "warning",
			})
		}
		task.Frontmatter.DispatchState = "judge_applied"
		if verdict == "blocking" && blockedGuidanceLoop {
			task.Frontmatter.DispatchState = "blocked_guidance_applied"
		}
		task.Frontmatter.LastReviewPath = filepath.ToSlash(review.Path)
		task.Frontmatter.OwnerAgent = ""
		task.LeaseUntil = time.Time{}
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if commit := strings.TrimSpace(review.Frontmatter.TargetCommit); commit != "" {
			task.Frontmatter.HeadCommit = commit
		}
		if err := persistTaskDocument(repo, idx); err != nil {
			return applied, events, err
		}
		applied++
	}
	return applied, events, nil
}

func requeueBlockedTasksForReviewerGuidance(repo *Repository, campaignID string) (int, []ReconcileEvent, error) {
	if repo == nil || len(repo.Tasks) == 0 {
		return 0, nil, nil
	}
	requeued := 0
	var events []ReconcileEvent
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		if !shouldRequestBlockedGuidance(*task) {
			continue
		}
		outcome := applyBlockedGuidanceTransition(task, task.Frontmatter.LastBlockedReason)
		if !outcome.GuidanceRequested {
			continue
		}
		if err := persistTaskDocument(repo, idx); err != nil {
			return requeued, events, err
		}
		taskID := strings.TrimSpace(task.Frontmatter.TaskID)
		taskTitle := strings.TrimSpace(task.Frontmatter.Title)
		events = append(events, ReconcileEvent{
			Kind:       EventTaskBlocked,
			CampaignID: campaignID,
			TaskID:     taskID,
			Title:      "任务遇到阻塞，转评审指导",
			Detail:     fmt.Sprintf("任务 **%s** %s 本轮遇到阻塞，已转 reviewer 指导（第 %d/%d 次）。\n\n**原因**: %s", taskID, taskTitle, outcome.GuidanceAttempt, maxBlockedGuidanceRetries, blankForSummary(outcome.Reason)),
			Severity:   "warning",
		})
		requeued++
	}
	return requeued, events, nil
}

func repairDanglingExecutorReviewHandOff(repo *Repository) (int, error) {
	if repo == nil || len(repo.Tasks) == 0 {
		return 0, nil
	}
	reviewIndex := reviewsByTask(repo.Reviews)
	repaired := 0
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		if normalizeTaskStatus(task.Frontmatter.Status) != TaskStatusExecuting {
			continue
		}
		if strings.TrimSpace(task.Frontmatter.OwnerAgent) != "" || !task.LeaseUntil.IsZero() {
			continue
		}
		if !taskLooksReadyForReviewHandOff(*task) {
			continue
		}
		if review, ok := latestTaskReview(reviewIndex[strings.TrimSpace(task.Frontmatter.TaskID)]); ok {
			if round := review.Frontmatter.ReviewRound; round > 0 && task.Frontmatter.ReviewRound > round {
				task.Frontmatter.ReviewRound = round
			}
		}
		task.Frontmatter.Status = TaskStatusReviewPending
		task.Frontmatter.DispatchState = "executor_completed"
		task.Frontmatter.ReviewStatus = "pending"
		task.Frontmatter.OwnerAgent = ""
		task.LeaseUntil = time.Time{}
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if err := persistTaskDocument(repo, idx); err != nil {
			return repaired, err
		}
		repaired++
	}
	return repaired, nil
}

func taskLooksReadyForReviewHandOff(task TaskDocument) bool {
	if strings.TrimSpace(task.Frontmatter.LastRunPath) == "" {
		return false
	}
	raw := strings.ToLower(strings.TrimSpace(task.Frontmatter.ReviewStatus))
	raw = strings.ReplaceAll(raw, "-", "_")
	raw = strings.ReplaceAll(raw, " ", "_")
	switch raw {
	case "review_pending", "pending_review", "awaiting_review":
		return true
	default:
		return false
	}
}

func claimSelectedExecutorTasks(repo *Repository, summary Summary, now time.Time, leaseDuration time.Duration, campaignID string) (int, []ReconcileEvent, error) {
	if repo == nil || len(summary.SelectedReady) == 0 {
		return 0, nil, nil
	}
	index := taskIndexesByPath(repo.Tasks)
	claimed := 0
	var events []ReconcileEvent
	for _, selected := range summary.SelectedReady {
		taskIndex, ok := index[filepath.ToSlash(selected.Path)]
		if !ok {
			continue
		}
		task := &repo.Tasks[taskIndex]
		role := resolveExecutorRole(*repo, *task)
		task.Frontmatter.ExecutionRound++
		task.Frontmatter.Status = TaskStatusExecuting
		task.Frontmatter.DispatchState = "executor_dispatched"
		task.Frontmatter.OwnerAgent = roleLabel(role)
		task.LeaseUntil = now.Add(leaseDuration)
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if task.Frontmatter.ReviewStatus == "" {
			task.Frontmatter.ReviewStatus = "pending"
		}
		if err := persistTaskDocument(repo, taskIndex); err != nil {
			return claimed, events, err
		}
		if filepath.ToSlash(repo.Tasks[taskIndex].Path) != filepath.ToSlash(selected.Path) {
			return claimed, events, fmt.Errorf("task index stale for %s", selected.Path)
		}
		taskID := strings.TrimSpace(task.Frontmatter.TaskID)
		taskTitle := strings.TrimSpace(task.Frontmatter.Title)
		events = append(events, ReconcileEvent{
			Kind:       EventTaskDispatched,
			CampaignID: campaignID,
			TaskID:     taskID,
			Title:      "任务已派发执行",
			Detail:     fmt.Sprintf("任务 **%s** %s 已派发给 %s（第 %d 轮）", taskID, taskTitle, roleLabel(role), task.Frontmatter.ExecutionRound),
			Severity:   "info",
		})
		claimed++
	}
	return claimed, events, nil
}

func claimSelectedReviewTasks(repo *Repository, summary Summary, now time.Time, leaseDuration time.Duration, campaignID string) (int, []ReconcileEvent, error) {
	if repo == nil || len(summary.SelectedReview) == 0 {
		return 0, nil, nil
	}
	index := taskIndexesByPath(repo.Tasks)
	claimed := 0
	var events []ReconcileEvent
	for _, selected := range summary.SelectedReview {
		taskIndex, ok := index[filepath.ToSlash(selected.Path)]
		if !ok {
			continue
		}
		task := &repo.Tasks[taskIndex]
		role := resolveReviewerRole(*repo, *task)
		task.Frontmatter.ReviewRound++
		task.Frontmatter.Status = TaskStatusReviewing
		task.Frontmatter.DispatchState = "reviewer_dispatched"
		task.Frontmatter.ReviewStatus = "reviewing"
		task.Frontmatter.OwnerAgent = roleLabel(role)
		task.LeaseUntil = now.Add(leaseDuration)
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if err := persistTaskDocument(repo, taskIndex); err != nil {
			return claimed, events, err
		}
		if filepath.ToSlash(repo.Tasks[taskIndex].Path) != filepath.ToSlash(selected.Path) {
			return claimed, events, fmt.Errorf("task index stale for %s", selected.Path)
		}
		taskID := strings.TrimSpace(task.Frontmatter.TaskID)
		taskTitle := strings.TrimSpace(task.Frontmatter.Title)
		events = append(events, ReconcileEvent{
			Kind:       EventTaskDispatched,
			CampaignID: campaignID,
			TaskID:     taskID,
			Title:      "任务已派发评审",
			Detail:     fmt.Sprintf("任务 **%s** %s 已派发给 %s 评审（第 %d 轮）", taskID, taskTitle, roleLabel(role), task.Frontmatter.ReviewRound),
			Severity:   "info",
		})
		claimed++
	}
	return claimed, events, nil
}
