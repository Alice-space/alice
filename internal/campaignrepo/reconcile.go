package campaignrepo

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const defaultDispatchLease = 2 * time.Hour

type ReconcileResult struct {
	Repository       Repository         `json:"repository"`
	Summary          Summary            `json:"summary"`
	DispatchTasks    []DispatchTaskSpec `json:"dispatch_tasks,omitempty"`
	Changed          bool               `json:"changed"`
	AppliedReviews   int                `json:"applied_reviews"`
	ClaimedExecutors int                `json:"claimed_executors"`
	ClaimedReviewers int                `json:"claimed_reviewers"`
}

func ReconcileFromPath(root string, now time.Time, maxParallel int) (Repository, Summary, error) {
	result, err := ReconcileAndPrepare(root, now, maxParallel, defaultDispatchLease)
	if err != nil {
		return Repository{}, Summary{}, err
	}
	return result.Repository, result.Summary, nil
}

func ReconcileAndPrepare(root string, now time.Time, maxParallel int, leaseDuration time.Duration) (ReconcileResult, error) {
	if leaseDuration <= 0 {
		leaseDuration = defaultDispatchLease
	}
	repo, err := Load(root)
	if err != nil {
		return ReconcileResult{}, err
	}
	changed := false

	appliedReviews, err := applyReviewVerdicts(&repo)
	if err != nil {
		return ReconcileResult{}, err
	}
	if appliedReviews > 0 {
		changed = true
	}

	summary := Summarize(repo, now, maxParallel)
	claimedExecutors, err := claimSelectedExecutorTasks(&repo, summary, now, leaseDuration)
	if err != nil {
		return ReconcileResult{}, err
	}
	if claimedExecutors > 0 {
		changed = true
		summary = Summarize(repo, now, maxParallel)
	}

	claimedReviewers, err := claimSelectedReviewTasks(&repo, summary, now, leaseDuration)
	if err != nil {
		return ReconcileResult{}, err
	}
	if claimedReviewers > 0 {
		changed = true
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
	}, nil
}

func applyReviewVerdicts(repo *Repository) (int, error) {
	if repo == nil || len(repo.Reviews) == 0 || len(repo.Tasks) == 0 {
		return 0, nil
	}
	reviewIndex := reviewsByTask(repo.Reviews)
	applied := 0
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
		switch verdict {
		case "approve":
			task.Frontmatter.Status = TaskStatusAccepted
			task.Frontmatter.ReviewStatus = "approved"
		case "blocking":
			task.Frontmatter.Status = TaskStatusBlocked
			task.Frontmatter.ReviewStatus = "blocked"
		case "reject":
			task.Frontmatter.Status = TaskStatusRejected
			task.Frontmatter.ReviewStatus = "blocked"
		default:
			task.Frontmatter.Status = TaskStatusRework
			task.Frontmatter.ReviewStatus = "changes_requested"
		}
		task.Frontmatter.DispatchState = "judge_applied"
		task.Frontmatter.LastReviewPath = filepath.ToSlash(review.Path)
		task.Frontmatter.OwnerAgent = ""
		task.LeaseUntil = time.Time{}
		task.WakeAt = time.Time{}
		task.Frontmatter.WakePrompt = ""
		if commit := strings.TrimSpace(review.Frontmatter.TargetCommit); commit != "" {
			task.Frontmatter.HeadCommit = commit
		}
		if err := persistTaskDocument(repo, idx); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func claimSelectedExecutorTasks(repo *Repository, summary Summary, now time.Time, leaseDuration time.Duration) (int, error) {
	if repo == nil || len(summary.SelectedReady) == 0 {
		return 0, nil
	}
	index := taskIndexesByPath(repo.Tasks)
	claimed := 0
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
			return claimed, err
		}
		if filepath.ToSlash(repo.Tasks[taskIndex].Path) != filepath.ToSlash(selected.Path) {
			return claimed, fmt.Errorf("task index stale for %s", selected.Path)
		}
		claimed++
	}
	return claimed, nil
}

func claimSelectedReviewTasks(repo *Repository, summary Summary, now time.Time, leaseDuration time.Duration) (int, error) {
	if repo == nil || len(summary.SelectedReview) == 0 {
		return 0, nil
	}
	index := taskIndexesByPath(repo.Tasks)
	claimed := 0
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
			return claimed, err
		}
		if filepath.ToSlash(repo.Tasks[taskIndex].Path) != filepath.ToSlash(selected.Path) {
			return claimed, fmt.Errorf("task index stale for %s", selected.Path)
		}
		claimed++
	}
	return claimed, nil
}
