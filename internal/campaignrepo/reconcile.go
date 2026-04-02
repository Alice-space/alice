package campaignrepo

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const defaultDispatchLease = 2 * time.Hour
const maxSummaryBlockAutoRetries = 3
const maxBlockedGuidanceRetries = 3
const maxExecutionRoundsBeforeNeedsHuman = 6
const maxReviewRoundsBeforeNeedsHuman = 6

const (
	dispatchStateArtifactRepairRequested  = "artifact_repair_requested"
	dispatchStateArtifactRepairDispatched = "artifact_repair_dispatched"
	dispatchStateJudgeWaitingReviewer     = "judge_waiting_reviewer_self_check"
	dispatchStateHumanGuidanceRequested   = "human_guidance_requested"
	dispatchStateHumanGuidanceApplied     = "human_guidance_applied"
	dispatchStateNeedsHuman               = "needs_human"
)

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
	if len(repo.LoadIssues) > 0 {
		summary := Summarize(repo, now, maxParallel)
		return ReconcileResult{
			Repository: repo,
			Summary:    summary,
		}, nil
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

	planningPhase := isPlanningPhase(repo.Campaign.Frontmatter.PlanStatus)

	repairedPostRunBlocks, err := repairTerminalPostRunValidationBlocks(&repo)
	if err != nil {
		return ReconcileResult{}, err
	}
	if repairedPostRunBlocks > 0 {
		changed = true
	}

	appliedReviews, verdictEvents, err := applyReviewVerdicts(&repo, campaignID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if appliedReviews > 0 {
		changed = true
		events = append(events, verdictEvents...)
	}

	retriedIntegrationBlocks, err := retryResolvedIntegrationBlocks(&repo)
	if err != nil {
		return ReconcileResult{}, err
	}
	if retriedIntegrationBlocks > 0 {
		changed = true
	}

	integratedTasks, integrationEvents, err := integrateAcceptedTasks(&repo, campaignID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if integratedTasks > 0 {
		changed = true
		events = append(events, integrationEvents...)
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

	repairedWorkspaces, err := repairTaskExecutionWorkspaces(&repo)
	if err != nil {
		return ReconcileResult{}, err
	}
	if repairedWorkspaces > 0 {
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

	needsHumanTasks, needsHumanEvents, err := escalateLoopingTasksToHuman(&repo, campaignID)
	if err != nil {
		return ReconcileResult{}, err
	}
	if needsHumanTasks > 0 {
		changed = true
		events = append(events, needsHumanEvents...)
	}

	summary := Summarize(repo, now, maxParallel)
	if planningPhase {
		dispatchTasks, err := buildDispatchSpecs(repo, now)
		if err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{
			Repository:     repo,
			Summary:        summary,
			DispatchTasks:  dispatchTasks,
			Changed:        changed,
			AppliedReviews: appliedReviews,
			Events:         events,
		}, nil
	}
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

func retryResolvedIntegrationBlocks(repo *Repository) (int, error) {
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
		if normalizeTaskStatus(task.Frontmatter.Status) != TaskStatusBlocked {
			continue
		}
		if strings.TrimSpace(task.Frontmatter.DispatchState) != "integration_blocked" {
			continue
		}
		if normalizeReviewStatus(task.Frontmatter.ReviewStatus) != "approved" {
			continue
		}
		targetRepos := integrationTargetRepos(*task, resolveTaskSourceRepos(*task, sourceRepoByID))
		reason := strings.TrimSpace(task.Frontmatter.LastBlockedReason)
		if integrationFailureLooksLikeMergeConflict(reason) {
			if !queueIntegrationConflictRecovery(task) {
				continue
			}
			if err := persistTaskDocument(repo, idx); err != nil {
				return retried, err
			}
			retried++
			continue
		}
		if !integrationBlockerIsRetryable(reason) {
			continue
		}
		if !integrationTargetsReadyForRetry(*task, targetRepos) {
			continue
		}

		task.Frontmatter.Status = TaskStatusAccepted
		task.Frontmatter.DispatchState = "integration_retry_pending"
		task.Frontmatter.IntegrationRetryCount = 0
		task.Frontmatter.LastBlockedReason = ""
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

func integrationBlockerIsRetryable(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	retryableFragments := []string{
		"integration missing repo_id/local_path",
		"is missing default_branch for integration",
		"local_path is not a git worktree",
		"local_path must stay on default branch",
		"local_path has uncommitted changes",
		"is missing a working_branch for integration",
		"still points at default branch",
		"does not exist locally for integration",
		"does not contain reviewed head_commit",
	}
	for _, fragment := range retryableFragments {
		if strings.Contains(reason, fragment) {
			return true
		}
	}
	return false
}

func integrationTargetsReadyForRetry(task TaskDocument, repos []SourceRepoDocument) bool {
	if len(repos) == 0 {
		return false
	}
	if issues := taskExecutionWorkspaceIssues(task, repos); len(issues) > 0 {
		return false
	}
	for _, repoDoc := range repos {
		if _, err := validateTaskIntegrationTarget(task, repoDoc); err != nil {
			return false
		}
	}
	return true
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
		task.Frontmatter.DispatchState = dispatchStateArtifactRepairRequested
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

func taskDispatchesArtifactRepair(task TaskDocument) bool {
	switch strings.TrimSpace(task.Frontmatter.DispatchState) {
	case dispatchStateArtifactRepairRequested, dispatchStateArtifactRepairDispatched:
		return task.Frontmatter.AutoRetryCount > 0
	default:
		return false
	}
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

func escalateLoopingTasksToHuman(repo *Repository, campaignID string) (int, []ReconcileEvent, error) {
	if repo == nil || len(repo.Tasks) == 0 {
		return 0, nil, nil
	}
	reviewIndex := reviewsByTask(repo.Reviews)
	escalated := 0
	var events []ReconcileEvent
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		reason := taskNeedsHumanLoopReason(*task, reviewIndex[strings.TrimSpace(task.Frontmatter.TaskID)])
		if reason == "" {
			continue
		}
		if normalizeTaskStatus(task.Frontmatter.Status) == TaskStatusBlocked &&
			strings.TrimSpace(task.Frontmatter.DispatchState) == dispatchStateNeedsHuman &&
			strings.TrimSpace(task.Frontmatter.LastBlockedReason) == reason {
			continue
		}
		task.Frontmatter.Status = TaskStatusBlocked
		task.Frontmatter.DispatchState = dispatchStateNeedsHuman
		task.Frontmatter.ReviewStatus = "blocked"
		task.Frontmatter.LastBlockedReason = reason
		clearTaskAssignment(task)
		if err := persistTaskDocument(repo, idx); err != nil {
			return escalated, events, err
		}
		taskID := strings.TrimSpace(task.Frontmatter.TaskID)
		taskTitle := strings.TrimSpace(task.Frontmatter.Title)
		events = append(events, ReconcileEvent{
			Kind:       EventTaskNeedsHuman,
			CampaignID: campaignID,
			TaskID:     taskID,
			Title:      "任务需要人工脱困",
			Detail:     fmt.Sprintf("任务 **%s** %s 已停止自动返工并升级为人工处理：%s\n建议命令：`/alice guide-task %s accept <接受说明>` 或 `/alice guide-task %s resume <恢复说明>`", taskID, taskTitle, reason, taskID, taskID),
			Severity:   "error",
		})
		escalated++
	}
	return escalated, events, nil
}

func taskNeedsHumanLoopReason(task TaskDocument, reviews []ReviewDocument) string {
	status := normalizeTaskStatus(task.Frontmatter.Status)
	switch status {
	case TaskStatusReady, TaskStatusRework, TaskStatusReviewPending, TaskStatusBlocked:
	default:
		return ""
	}
	if strings.TrimSpace(task.Frontmatter.DispatchState) == dispatchStateNeedsHuman {
		return ""
	}
	if task.Frontmatter.AutoRetryCount >= maxSummaryBlockAutoRetries && strings.TrimSpace(task.Frontmatter.LastBlockedReason) != "" {
		return fmt.Sprintf("artifact-only repair has already retried %d times but the blocker remains: %s", task.Frontmatter.AutoRetryCount, strings.TrimSpace(task.Frontmatter.LastBlockedReason))
	}
	if reason := repeatedConcernOnSameTargetReason(task, reviews); reason != "" {
		return reason
	}
	if strings.TrimSpace(task.Frontmatter.LastBlockedReason) != "" &&
		(task.Frontmatter.ExecutionRound >= maxExecutionRoundsBeforeNeedsHuman ||
			task.Frontmatter.ReviewRound >= maxReviewRoundsBeforeNeedsHuman) {
		return fmt.Sprintf("task has looped to execution_round=%d review_round=%d while the blocker is still %q", task.Frontmatter.ExecutionRound, task.Frontmatter.ReviewRound, strings.TrimSpace(task.Frontmatter.LastBlockedReason))
	}
	return ""
}

func repeatedConcernOnSameTargetReason(task TaskDocument, reviews []ReviewDocument) string {
	if len(reviews) < 2 || task.Frontmatter.ReviewRound < 2 {
		return ""
	}
	relevant := make([]ReviewDocument, 0, len(reviews))
	for _, review := range reviews {
		if !reviewMatchesTaskReviewer(task, review) {
			continue
		}
		verdict := normalizeReviewVerdict(review.Frontmatter.Verdict, review.Frontmatter.Blocking)
		if verdict == "" || verdict == "approve" {
			continue
		}
		relevant = append(relevant, review)
	}
	if len(relevant) < 2 {
		return ""
	}
	sort.Slice(relevant, func(i, j int) bool {
		return compareReviewDocs(relevant[i], relevant[j]) > 0
	})
	latest := relevant[0]
	previous := relevant[1]
	if latest.Frontmatter.ReviewRound == previous.Frontmatter.ReviewRound {
		return ""
	}
	latestTarget := reviewTargetFingerprint(task, latest)
	previousTarget := reviewTargetFingerprint(task, previous)
	if latestTarget == "" || latestTarget != previousTarget {
		return ""
	}
	return fmt.Sprintf("review rounds %d and %d both returned non-approve verdicts on the same target %s", latest.Frontmatter.ReviewRound, previous.Frontmatter.ReviewRound, latestTarget)
}

func reviewTargetFingerprint(task TaskDocument, review ReviewDocument) string {
	target := strings.TrimSpace(review.Frontmatter.TargetCommit)
	if target != "" {
		return target
	}
	if !taskRequiresSourceRepoEvidence(task) {
		return "campaign-artifacts"
	}
	return strings.TrimSpace(task.Frontmatter.HeadCommit)
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

func repairTerminalPostRunValidationBlocks(repo *Repository) (int, error) {
	if repo == nil || len(repo.Tasks) == 0 {
		return 0, nil
	}
	reviewIndex := reviewsByTask(repo.Reviews)
	repaired := 0
	for idx := range repo.Tasks {
		task := &repo.Tasks[idx]
		if !taskEligibleForPostRunValidationRepair(*task) {
			continue
		}
		if !recoverTerminalPostRunValidationBlock(task, reviewIndex[strings.TrimSpace(task.Frontmatter.TaskID)]) {
			continue
		}
		if err := persistTaskDocument(repo, idx); err != nil {
			return repaired, err
		}
		repaired++
	}
	return repaired, nil
}

func taskEligibleForPostRunValidationRepair(task TaskDocument) bool {
	if normalizeTaskStatus(task.Frontmatter.Status) != TaskStatusBlocked {
		return false
	}
	if strings.TrimSpace(task.Frontmatter.DispatchState) != "signal_blocked_terminal" {
		return false
	}
	reason := strings.ToLower(strings.TrimSpace(task.Frontmatter.LastBlockedReason))
	return strings.Contains(reason, "post-run validation failed")
}

func recoverTerminalPostRunValidationBlock(task *TaskDocument, reviews []ReviewDocument) bool {
	if task == nil {
		return false
	}

	switch DispatchKind(strings.ToLower(strings.TrimSpace(task.Frontmatter.SelfCheckKind))) {
	case DispatchKindExecutor:
		if !taskHasPassedPostRunSelfCheck(*task, DispatchKindExecutor) {
			return false
		}
		if strings.TrimSpace(task.Frontmatter.LastRunPath) == "" {
			return false
		}
		if review, ok := latestTaskReview(reviews); ok {
			if round := review.Frontmatter.ReviewRound; round > 0 && task.Frontmatter.ReviewRound > round {
				task.Frontmatter.ReviewRound = round
			}
		}
		task.Frontmatter.Status = TaskStatusReviewPending
		task.Frontmatter.DispatchState = "executor_completed"
		task.Frontmatter.ReviewStatus = "pending"
	case DispatchKindReviewer:
		if !taskHasPassedPostRunSelfCheck(*task, DispatchKindReviewer) {
			return false
		}
		review, ok := latestRelevantReview(*task, reviews)
		if !ok {
			return false
		}
		if normalizeReviewVerdict(review.Frontmatter.Verdict, review.Frontmatter.Blocking) == "" {
			return false
		}
		task.Frontmatter.Status = TaskStatusReviewPending
		task.Frontmatter.DispatchState = "reviewer_completed_recovered"
		task.Frontmatter.ReviewStatus = "pending"
	default:
		return false
	}

	task.Frontmatter.LastBlockedReason = ""
	clearTaskAssignment(task)
	return true
}

func taskHasPassedPostRunSelfCheck(task TaskDocument, kind DispatchKind) bool {
	if DispatchKind(strings.ToLower(strings.TrimSpace(task.Frontmatter.SelfCheckKind))) != kind {
		return false
	}
	if normalizeTaskSelfCheckStatus(task.Frontmatter.SelfCheckStatus) != taskSelfCheckStatusPassed {
		return false
	}
	if task.Frontmatter.SelfCheckRound <= 0 || task.Frontmatter.SelfCheckRound != taskPostRunRound(task, kind) {
		return false
	}
	if strings.TrimSpace(task.Frontmatter.SelfCheckAtRaw) == "" {
		return false
	}
	return taskSelfCheckProofMatchesCurrentState(task, kind)
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
		if reason := reviewVerdictGateReason(*task, review); reason != "" {
			if strings.TrimSpace(task.Frontmatter.LastBlockedReason) == reason &&
				strings.TrimSpace(task.Frontmatter.DispatchState) == dispatchStateJudgeWaitingReviewer {
				continue
			}
			task.Frontmatter.LastBlockedReason = reason
			task.Frontmatter.DispatchState = dispatchStateJudgeWaitingReviewer
			if err := persistTaskDocument(repo, idx); err != nil {
				return applied, events, err
			}
			taskID := strings.TrimSpace(task.Frontmatter.TaskID)
			taskTitle := strings.TrimSpace(task.Frontmatter.Title)
			events = append(events, ReconcileEvent{
				Kind:       EventJudgeDeferred,
				CampaignID: campaignID,
				TaskID:     taskID,
				Title:      "Judge 等待 reviewer 自检",
				Detail:     fmt.Sprintf("任务 **%s** %s 的评审结论暂不应用：%s", taskID, taskTitle, reason),
				Severity:   "warning",
			})
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
		task.Frontmatter.LastBlockedReason = ""
		task.Frontmatter.LastReviewPath = filepath.ToSlash(review.Path)
		task.Frontmatter.OwnerAgent = ""
		task.LeaseUntil = time.Time{}
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if taskRequiresSourceRepoEvidence(*task) {
			if commit := strings.TrimSpace(review.Frontmatter.TargetCommit); commit != "" {
				task.Frontmatter.HeadCommit = commit
			}
		} else {
			task.Frontmatter.HeadCommit = ""
		}
		if err := persistTaskDocument(repo, idx); err != nil {
			return applied, events, err
		}
		applied++
	}
	return applied, events, nil
}

func reviewVerdictReadyForJudge(task TaskDocument, review ReviewDocument) bool {
	return reviewVerdictGateReason(task, review) == ""
}

func reviewVerdictGateReason(task TaskDocument, review ReviewDocument) string {
	targetRound := task.Frontmatter.ReviewRound
	if review.Frontmatter.ReviewRound > 0 {
		targetRound = review.Frontmatter.ReviewRound
	}
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	recordedKind := DispatchKind(strings.ToLower(strings.TrimSpace(task.Frontmatter.SelfCheckKind)))
	switch {
	case recordedKind == "":
		return fmt.Sprintf("judge is waiting for reviewer self-check proof for task %s round %d: no self_check_kind is recorded", taskID, targetRound)
	case recordedKind != DispatchKindReviewer:
		return fmt.Sprintf("judge is waiting for reviewer self-check proof for task %s round %d: latest self_check_kind is %s", taskID, targetRound, recordedKind)
	case normalizeTaskSelfCheckStatus(task.Frontmatter.SelfCheckStatus) != taskSelfCheckStatusPassed:
		return fmt.Sprintf("judge is waiting for reviewer self-check proof for task %s round %d: self_check_status is %q instead of %q", taskID, targetRound, blankForSummary(task.Frontmatter.SelfCheckStatus), taskSelfCheckStatusPassed)
	case targetRound <= 0:
		return fmt.Sprintf("judge cannot apply reviewer verdict for task %s because review_round is empty", taskID)
	case task.Frontmatter.SelfCheckRound != targetRound:
		return fmt.Sprintf("judge is waiting for reviewer self-check proof for task %s round %d: latest proof is for round %d", taskID, targetRound, task.Frontmatter.SelfCheckRound)
	case strings.TrimSpace(task.Frontmatter.SelfCheckAtRaw) == "":
		return fmt.Sprintf("judge is waiting for reviewer self-check proof for task %s round %d: self_check_at is empty", taskID, targetRound)
	case !taskSelfCheckProofMatchesCurrentState(task, DispatchKindReviewer):
		return fmt.Sprintf("judge is waiting for reviewer self-check proof for task %s round %d: task state no longer matches the recorded proof digest", taskID, targetRound)
	default:
		return ""
	}
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
			Kind:       EventTaskRetrying,
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
	switch normalizeReviewStatus(task.Frontmatter.ReviewStatus) {
	case "pending", "queued":
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
		if err := ensureTaskExecutionWorkspaces(repo, task); err != nil {
			return claimed, events, err
		}
		artifactRepairOnly := taskDispatchesArtifactRepair(*task)
		if !artifactRepairOnly {
			task.Frontmatter.ExecutionRound++
		}
		task.Frontmatter.Status = TaskStatusExecuting
		task.Frontmatter.DispatchState = "executor_dispatched"
		if artifactRepairOnly {
			task.Frontmatter.DispatchState = dispatchStateArtifactRepairDispatched
		}
		clearTaskSelfCheck(task)
		task.Frontmatter.OwnerAgent = roleLabel(role)
		task.LeaseUntil = now.Add(leaseDuration)
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if !artifactRepairOnly && !taskNeedsIntegrationConflictRecovery(*task) {
			task.Frontmatter.LastBlockedReason = ""
		}
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
		detail := fmt.Sprintf("任务 **%s** %s 已派发给 %s（第 %d 轮）", taskID, taskTitle, roleLabel(role), task.Frontmatter.ExecutionRound)
		if artifactRepairOnly {
			detail = fmt.Sprintf("任务 **%s** %s 已派发给 %s 进行 task-local artifact repair（执行轮次仍为第 %d 轮，补件第 %d 次）", taskID, taskTitle, roleLabel(role), task.Frontmatter.ExecutionRound, task.Frontmatter.AutoRetryCount)
		}
		events = append(events, ReconcileEvent{
			Kind:       EventTaskDispatched,
			CampaignID: campaignID,
			TaskID:     taskID,
			Title:      "任务已派发执行",
			Detail:     detail,
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
		clearTaskSelfCheck(task)
		task.Frontmatter.OwnerAgent = roleLabel(role)
		task.LeaseUntil = now.Add(leaseDuration)
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		task.Frontmatter.LastBlockedReason = ""
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
