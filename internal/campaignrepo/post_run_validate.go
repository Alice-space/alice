package campaignrepo

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	taskSelfCheckStatusPassed = "passed"
	taskSelfCheckStatusFailed = "failed"
)

func ValidateTaskPostRun(root, taskID string, kind DispatchKind) (ValidationResult, error) {
	repo, task, _, err := loadTaskForPostRun(root, taskID)
	if err != nil {
		return ValidationResult{}, err
	}
	return validateTaskPostRun(repo, task, kind, true), nil
}

func RunTaskSelfCheck(root, taskID string, kind DispatchKind, checkedAt time.Time) (ValidationResult, error) {
	repo, task, index, err := loadTaskForPostRun(root, taskID)
	if err != nil {
		return ValidationResult{}, err
	}
	validation := validateTaskPostRun(repo, task, kind, false)
	if checkedAt.IsZero() {
		checkedAt = time.Now().Local()
	}
	task.Frontmatter.SelfCheckKind = string(kind)
	task.Frontmatter.SelfCheckRound = taskPostRunRound(task, kind)
	task.Frontmatter.SelfCheckStatus = taskSelfCheckStatusFailed
	if validation.Valid {
		task.Frontmatter.SelfCheckStatus = taskSelfCheckStatusPassed
	}
	task.Frontmatter.SelfCheckAtRaw = checkedAt.Format(time.RFC3339)
	repo.Tasks[index] = task
	if err := persistTaskDocument(&repo, index); err != nil {
		return ValidationResult{}, err
	}
	return validation, nil
}

func loadTaskForPostRun(root, taskID string) (Repository, TaskDocument, int, error) {
	repo, err := Load(root)
	if err != nil {
		return Repository{}, TaskDocument{}, -1, err
	}
	taskID = strings.TrimSpace(taskID)
	for idx, task := range repo.Tasks {
		if strings.TrimSpace(task.Frontmatter.TaskID) != taskID {
			continue
		}
		return repo, task, idx, nil
	}
	return Repository{}, TaskDocument{}, -1, fmt.Errorf("task %s not found", taskID)
}

func validateTaskPostRun(repo Repository, task TaskDocument, kind DispatchKind, requireSelfCheckProof bool) ValidationResult {
	var issues []ValidationIssue
	switch kind {
	case DispatchKindExecutor:
		validateExecutorPostRunTask(repo, task, &issues)
	case DispatchKindReviewer:
		validateReviewerPostRunTask(repo, task, &issues)
	default:
		return ValidationResult{Valid: true}
	}
	if requireSelfCheckProof {
		validateTaskSelfCheckProof(task, kind, &issues)
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		return issues[i].Message < issues[j].Message
	})
	return ValidationResult{
		Valid:  len(issues) == 0,
		Issues: issues,
	}
}

func validateExecutorPostRunTask(repo Repository, task TaskDocument, issues *[]ValidationIssue) {
	sourceRepoByID := make(map[string]SourceRepoDocument, len(repo.SourceRepos))
	for _, repoDoc := range repo.SourceRepos {
		repoID := strings.TrimSpace(repoDoc.Frontmatter.RepoID)
		if repoID == "" {
			continue
		}
		sourceRepoByID[repoID] = repoDoc
	}

	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	status := normalizeTaskStatus(task.Frontmatter.Status)
	switch status {
	case TaskStatusReviewPending, TaskStatusWaitingExternal:
		validateTaskStateContract(repo.Root, task, sourceRepoByID, issues)
	case TaskStatusBlocked:
		if strings.TrimSpace(task.Frontmatter.LastBlockedReason) == "" {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_blocked_reason_missing",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is blocked after executor round but last_blocked_reason is empty", taskID),
			})
		}
		if strings.TrimSpace(task.Frontmatter.OwnerAgent) != "" || !task.LeaseUntil.IsZero() {
			*issues = append(*issues, ValidationIssue{
				Code:    "task_blocked_lease_not_cleared",
				Path:    task.Path,
				Message: fmt.Sprintf("task %s is blocked after executor round but owner_agent/lease_until are still set", taskID),
			})
		}
	case TaskStatusExecuting, TaskStatusReviewing:
		*issues = append(*issues, ValidationIssue{
			Code:    "task_executor_post_run_active_state",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s executor round finished but task still claims active status %s; executor must hand off as review_pending, waiting_external, or blocked", taskID, status),
		})
		validateTaskStateContract(repo.Root, task, sourceRepoByID, issues)
	default:
		*issues = append(*issues, ValidationIssue{
			Code:    "task_executor_post_run_status_invalid",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s executor round ended in unsupported status %s; executor must hand off as review_pending, waiting_external, or blocked", taskID, blankForSummary(status)),
		})
	}
}

func validateReviewerPostRunTask(repo Repository, task TaskDocument, issues *[]ValidationIssue) {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	status := normalizeTaskStatus(task.Frontmatter.Status)
	switch status {
	case TaskStatusReviewing, TaskStatusReviewPending, TaskStatusRework, TaskStatusBlocked:
	default:
		*issues = append(*issues, ValidationIssue{
			Code:    "task_reviewer_post_run_status_invalid",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s reviewer round ended in unsupported status %s; reviewer should leave task in reviewing/review_pending, or allow judge-applied rework/blocked", taskID, blankForSummary(status)),
		})
	}

	review, ok := latestTaskReview(reviewsByTask(repo.Reviews)[taskID])
	if !ok {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_reviewer_post_run_review_missing",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s reviewer round completed without writing a review file", taskID),
		})
		return
	}
	validateReviewDocument(review, map[string]TaskDocument{taskID: task}, issues)
	if !reviewMatchesTaskReviewer(task, review) {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_role_mismatch",
			Path:    review.Path,
			Message: fmt.Sprintf("review for task %s uses reviewer.role %q, which does not match the task reviewer contract %q", taskID, review.Frontmatter.Reviewer.Role, task.Frontmatter.Reviewer.Role),
		})
	}
	if normalizeReviewVerdict(review.Frontmatter.Verdict, review.Frontmatter.Blocking) == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_verdict_missing",
			Path:    review.Path,
			Message: fmt.Sprintf("review for task %s must set a non-empty verdict", taskID),
		})
	}
	if task.Frontmatter.ReviewRound > 0 && review.Frontmatter.ReviewRound > 0 && review.Frontmatter.ReviewRound != task.Frontmatter.ReviewRound {
		*issues = append(*issues, ValidationIssue{
			Code:    "review_round_mismatch",
			Path:    review.Path,
			Message: fmt.Sprintf("review for task %s used round %d, but task is currently on review round %d", taskID, review.Frontmatter.ReviewRound, task.Frontmatter.ReviewRound),
		})
	}
}

func validateTaskSelfCheckProof(task TaskDocument, kind DispatchKind, issues *[]ValidationIssue) {
	taskID := strings.TrimSpace(task.Frontmatter.TaskID)
	recordedKind := DispatchKind(strings.ToLower(strings.TrimSpace(task.Frontmatter.SelfCheckKind)))
	expectedRound := taskPostRunRound(task, kind)
	if recordedKind == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_post_run_self_check_missing",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s finished a %s round but has no recorded self-check proof", taskID, kind),
		})
		return
	}
	if recordedKind != kind {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_post_run_self_check_kind_mismatch",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s finished a %s round but latest self-check proof is tagged as %s", taskID, kind, recordedKind),
		})
	}
	if task.Frontmatter.SelfCheckRound != expectedRound {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_post_run_self_check_round_mismatch",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s finished %s round %d but latest self-check proof is for round %d", taskID, kind, expectedRound, task.Frontmatter.SelfCheckRound),
		})
	}
	if normalizeTaskSelfCheckStatus(task.Frontmatter.SelfCheckStatus) != taskSelfCheckStatusPassed {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_post_run_self_check_not_passed",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s finished a %s round but latest self-check status is %q instead of %q", taskID, kind, blankForSummary(task.Frontmatter.SelfCheckStatus), taskSelfCheckStatusPassed),
		})
	}
	if strings.TrimSpace(task.Frontmatter.SelfCheckAtRaw) == "" {
		*issues = append(*issues, ValidationIssue{
			Code:    "task_post_run_self_check_timestamp_missing",
			Path:    task.Path,
			Message: fmt.Sprintf("task %s finished a %s round but self_check_at is empty", taskID, kind),
		})
	}
}

func taskPostRunRound(task TaskDocument, kind DispatchKind) int {
	switch kind {
	case DispatchKindExecutor:
		return task.Frontmatter.ExecutionRound
	case DispatchKindReviewer:
		return task.Frontmatter.ReviewRound
	default:
		return 0
	}
}

func normalizeTaskSelfCheckStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case taskSelfCheckStatusPassed:
		return taskSelfCheckStatusPassed
	case taskSelfCheckStatusFailed:
		return taskSelfCheckStatusFailed
	default:
		return ""
	}
}

func clearTaskSelfCheck(task *TaskDocument) {
	if task == nil {
		return
	}
	task.Frontmatter.SelfCheckKind = ""
	task.Frontmatter.SelfCheckRound = 0
	task.Frontmatter.SelfCheckStatus = ""
	task.Frontmatter.SelfCheckAtRaw = ""
}
