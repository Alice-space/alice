package campaignrepo

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type taskReservation struct {
	TaskID      string
	TargetRepos []string
	WriteScope  []string
}

func Summarize(repo Repository, now time.Time, maxParallel int) Summary {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.Local()
	maxParallel = effectiveMaxParallel(maxParallel)
	summary := Summary{
		Root:          repo.Root,
		CampaignID:    strings.TrimSpace(repo.Campaign.Frontmatter.CampaignID),
		CampaignTitle: strings.TrimSpace(repo.Campaign.Frontmatter.Title),
		CurrentPhase:  strings.TrimSpace(repo.Campaign.Frontmatter.CurrentPhase),
		PlanRound:     repo.Campaign.Frontmatter.PlanRound,
		PlanStatus:    strings.TrimSpace(repo.Campaign.Frontmatter.PlanStatus),
		MaxParallel:   maxParallel,
		TaskCount:     len(repo.Tasks),
		GeneratedAt:   now,
		PhaseCounts:   map[string]int{},
	}

	byID := make(map[string]TaskDocument, len(repo.Tasks))
	for _, task := range repo.Tasks {
		if id := strings.TrimSpace(task.Frontmatter.TaskID); id != "" {
			byID[id] = task
		}
	}

	taskOrder := append([]TaskDocument(nil), repo.Tasks...)
	sort.Slice(taskOrder, func(i, j int) bool {
		left := taskOrder[i]
		right := taskOrder[j]
		if left.Frontmatter.Phase != right.Frontmatter.Phase {
			return left.Frontmatter.Phase < right.Frontmatter.Phase
		}
		if left.Frontmatter.TaskID != right.Frontmatter.TaskID {
			return left.Frontmatter.TaskID < right.Frontmatter.TaskID
		}
		return left.Path < right.Path
	})

	activeReservations := make([]taskReservation, 0, len(taskOrder))
	for _, task := range taskOrder {
		status := normalizeTaskStatus(task.Frontmatter.Status)
		view := taskSummary(task)
		if phase := strings.TrimSpace(view.Phase); phase != "" {
			summary.PhaseCounts[phase]++
		}
		switch status {
		case TaskStatusDraft:
			summary.DraftCount++
		case TaskStatusExecuting:
			summary.ActiveCount++
			summary.ExecutingCount++
			summary.ActiveTasks = append(summary.ActiveTasks, view)
			activeReservations = append(activeReservations, buildReservation(task))
		case TaskStatusReviewPending:
			summary.ReviewCount++
			summary.ReviewPendingCount++
			summary.ReviewPendingTasks = append(summary.ReviewPendingTasks, view)
		case TaskStatusReviewing:
			summary.ActiveCount++
			summary.ReviewCount++
			summary.ReviewingCount++
			summary.ActiveTasks = append(summary.ActiveTasks, view)
		case TaskStatusAccepted:
			summary.AcceptedCount++
			summary.AcceptedTasks = append(summary.AcceptedTasks, view)
		case TaskStatusBlocked:
			summary.BlockedCount++
			summary.BlockedTasks = append(summary.BlockedTasks, withBlockedReason(view, "status is blocked"))
		case TaskStatusWaitingExternal:
			summary.WaitingCount++
			if !task.WakeAt.IsZero() && strings.TrimSpace(task.Frontmatter.WakePrompt) != "" {
				if !task.WakeAt.After(now) {
					summary.WakeDue = append(summary.WakeDue, view)
				} else {
					summary.WakePending = append(summary.WakePending, view)
				}
				summary.WakeTasks = append(summary.WakeTasks, WakeTaskSpec{
					StateKey: wakeTaskStateKey(summary.CampaignID, task),
					TaskID:   view.TaskID,
					Title:    fmt.Sprintf("campaign wake %s %s", blankForKey(summary.CampaignID), blankForKey(view.TaskID)),
					TaskPath: blankTaskLocation(view),
					RunAt:    task.WakeAt,
					Prompt:   buildWakePrompt(repo, task),
				})
			}
		case TaskStatusDone:
			summary.DoneCount++
		case TaskStatusRejected:
			summary.RejectedCount++
		}
	}

	availableSlots := maxParallel - len(summary.ActiveTasks)
	if availableSlots < 0 {
		availableSlots = 0
	}
	selectedReservations := append([]taskReservation(nil), activeReservations...)
	for _, task := range taskOrder {
		status := normalizeTaskStatus(task.Frontmatter.Status)
		if status != TaskStatusReady && status != TaskStatusRework {
			continue
		}
		view := taskSummary(task)
		if reason := dependencyBlockReason(task, byID); reason != "" {
			summary.BlockedCount++
			summary.BlockedTasks = append(summary.BlockedTasks, withBlockedReason(view, reason))
			continue
		}
		if reason := leaseBlockReason(task, now); reason != "" {
			summary.BlockedCount++
			summary.BlockedTasks = append(summary.BlockedTasks, withBlockedReason(view, reason))
			continue
		}
		if reason := conflictBlockReason(task, selectedReservations); reason != "" {
			summary.BlockedCount++
			summary.BlockedTasks = append(summary.BlockedTasks, withBlockedReason(view, reason))
			continue
		}
		summary.ReadyCount++
		if status == TaskStatusRework {
			summary.ReworkCount++
		}
		summary.ReadyTasks = append(summary.ReadyTasks, view)
		if len(summary.SelectedReady) < availableSlots {
			summary.SelectedReady = append(summary.SelectedReady, view)
			selectedReservations = append(selectedReservations, buildReservation(task))
		}
	}
	summary.SelectedReadyCount = len(summary.SelectedReady)

	reviewSlots := maxParallel - summary.ReviewingCount
	if reviewSlots < 0 {
		reviewSlots = 0
	}
	for _, task := range taskOrder {
		if normalizeTaskStatus(task.Frontmatter.Status) != TaskStatusReviewPending {
			continue
		}
		view := taskSummary(task)
		if reason := leaseBlockReason(task, now); reason != "" {
			summary.BlockedCount++
			summary.BlockedTasks = append(summary.BlockedTasks, withBlockedReason(view, reason))
			continue
		}
		if len(summary.SelectedReview) < reviewSlots {
			summary.SelectedReview = append(summary.SelectedReview, view)
		}
	}
	summary.SelectedReviewCount = len(summary.SelectedReview)
	return summary
}

func (s Summary) SummaryLine() string {
	parts := []string{
		fmt.Sprintf("plan=%s", blankForSummary(s.PlanStatus)),
		fmt.Sprintf("plan_round=%d", s.PlanRound),
		fmt.Sprintf("phase=%s", blankForSummary(s.CurrentPhase)),
		fmt.Sprintf("active=%d", s.ActiveCount),
		fmt.Sprintf("ready=%d", s.ReadyCount),
		fmt.Sprintf("review_pending=%d", s.ReviewPendingCount),
		fmt.Sprintf("selected=%d", s.SelectedReadyCount),
		fmt.Sprintf("selected_review=%d", s.SelectedReviewCount),
		fmt.Sprintf("blocked=%d", s.BlockedCount),
		fmt.Sprintf("waiting=%d", s.WaitingCount),
		fmt.Sprintf("accepted=%d", s.AcceptedCount),
		fmt.Sprintf("wake_due=%d", len(s.WakeDue)),
	}
	return strings.Join(parts, " ")
}

func taskSummary(task TaskDocument) TaskSummary {
	return TaskSummary{
		TaskID:         strings.TrimSpace(task.Frontmatter.TaskID),
		Title:          strings.TrimSpace(task.Frontmatter.Title),
		Phase:          strings.TrimSpace(task.Frontmatter.Phase),
		Status:         normalizeTaskStatus(task.Frontmatter.Status),
		Path:           filepath.ToSlash(task.Path),
		Dir:            filepath.ToSlash(task.Dir),
		OwnerAgent:     strings.TrimSpace(task.Frontmatter.OwnerAgent),
		LeaseUntil:     task.LeaseUntil,
		WakeAt:         task.WakeAt,
		WakePrompt:     strings.TrimSpace(task.Frontmatter.WakePrompt),
		DependsOn:      append([]string(nil), task.Frontmatter.DependsOn...),
		TargetRepos:    append([]string(nil), task.Frontmatter.TargetRepos...),
		WriteScope:     append([]string(nil), task.Frontmatter.WriteScope...),
		DispatchState:  strings.TrimSpace(task.Frontmatter.DispatchState),
		ReviewStatus:   normalizeReviewStatus(task.Frontmatter.ReviewStatus),
		ExecutionRound: task.Frontmatter.ExecutionRound,
		ReviewRound:    task.Frontmatter.ReviewRound,
		HeadCommit:     strings.TrimSpace(task.Frontmatter.HeadCommit),
		LastReviewPath: filepath.ToSlash(strings.TrimSpace(task.Frontmatter.LastReviewPath)),
	}
}

func withBlockedReason(task TaskSummary, reason string) TaskSummary {
	task.BlockedReason = strings.TrimSpace(reason)
	return task
}

func dependencyBlockReason(task TaskDocument, byID map[string]TaskDocument) string {
	for _, depID := range task.Frontmatter.DependsOn {
		dependency, ok := byID[depID]
		if !ok {
			return fmt.Sprintf("missing dependency `%s`", depID)
		}
		switch normalizeTaskStatus(dependency.Frontmatter.Status) {
		case TaskStatusAccepted, TaskStatusDone:
			continue
		case TaskStatusRejected:
			return fmt.Sprintf("dependency `%s` ended in `%s`", depID, normalizeTaskStatus(dependency.Frontmatter.Status))
		default:
			return fmt.Sprintf("dependency `%s` not done yet", depID)
		}
	}
	return ""
}

func leaseBlockReason(task TaskDocument, now time.Time) string {
	if strings.TrimSpace(task.Frontmatter.OwnerAgent) == "" || task.LeaseUntil.IsZero() {
		return ""
	}
	if task.LeaseUntil.After(now) {
		return fmt.Sprintf("leased to `%s` until `%s`", task.Frontmatter.OwnerAgent, task.LeaseUntil.Format(time.RFC3339))
	}
	return ""
}

func buildReservation(task TaskDocument) taskReservation {
	return taskReservation{
		TaskID:      strings.TrimSpace(task.Frontmatter.TaskID),
		TargetRepos: normalizeRepoKeys(task.Frontmatter.TargetRepos),
		WriteScope:  normalizeScopes(task.Frontmatter.WriteScope),
	}
}

func conflictBlockReason(task TaskDocument, reservations []taskReservation) string {
	current := buildReservation(task)
	for _, reservation := range reservations {
		if !reservationsConflict(current, reservation) {
			continue
		}
		return fmt.Sprintf("write scope overlaps with `%s`", reservation.TaskID)
	}
	return ""
}

func reservationsConflict(left, right taskReservation) bool {
	if len(left.TargetRepos) == 0 || len(right.TargetRepos) == 0 {
		return false
	}
	if !hasIntersection(left.TargetRepos, right.TargetRepos) {
		return false
	}
	if len(left.WriteScope) == 0 || len(right.WriteScope) == 0 {
		return true
	}
	for _, ls := range left.WriteScope {
		for _, rs := range right.WriteScope {
			if scopeOverlaps(ls, rs) {
				return true
			}
		}
	}
	return false
}

func normalizeRepoKeys(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		trimmed := strings.ToLower(strings.TrimSpace(raw))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizeScopes(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		trimmed := filepath.ToSlash(strings.TrimSpace(raw))
		trimmed = strings.TrimPrefix(trimmed, "./")
		trimmed = strings.TrimSuffix(trimmed, "/**")
		trimmed = strings.TrimSuffix(trimmed, "/*")
		trimmed = strings.TrimSuffix(trimmed, "/")
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func scopeOverlaps(left, right string) bool {
	left = filepath.ToSlash(strings.TrimSpace(left))
	right = filepath.ToSlash(strings.TrimSpace(right))
	if left == "" || right == "" {
		return true
	}
	if left == right {
		return true
	}
	return strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func hasIntersection(left, right []string) bool {
	index := make(map[string]struct{}, len(left))
	for _, item := range left {
		index[item] = struct{}{}
	}
	for _, item := range right {
		if _, ok := index[item]; ok {
			return true
		}
	}
	return false
}

func effectiveMaxParallel(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}
