package campaignrepo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (s Summary) LiveReportMarkdown() string {
	var lines []string
	lines = append(lines,
		"---",
		"scope: live",
		fmt.Sprintf("status: %s", liveReportStatus(s)),
		fmt.Sprintf("generated_at: %s", s.GeneratedAt.Format(time.RFC3339)),
		fmt.Sprintf("campaign_id: %q", s.CampaignID),
		fmt.Sprintf("current_phase: %q", s.CurrentPhase),
		"---",
		"",
		"# Live Report",
		"",
		"## Summary",
		fmt.Sprintf("- campaign: `%s`", blankForSummary(s.CampaignTitle)),
		fmt.Sprintf("- phase: `%s`", blankForSummary(s.CurrentPhase)),
		fmt.Sprintf("- tasks: `%d` total, `%d` active, `%d` ready, `%d` review-pending, `%d` accepted, `%d` blocked, `%d` waiting, `%d` done, `%d` rejected", s.TaskCount, s.ActiveCount, s.ReadyCount, s.ReviewPendingCount, s.AcceptedCount, s.BlockedCount, s.WaitingCount, s.DoneCount, s.RejectedCount),
		fmt.Sprintf("- selected ready: `%d`, selected review: `%d` (max parallel `%d`)", s.SelectedReadyCount, s.SelectedReviewCount, s.MaxParallel),
		fmt.Sprintf("- wake due: `%d`, wake pending: `%d`", len(s.WakeDue), len(s.WakePending)),
		"",
		"## Active Tasks",
	)
	lines = appendTaskList(lines, s.ActiveTasks, "")
	lines = append(lines, "", "## Ready Tasks")
	lines = appendTaskList(lines, s.ReadyTasks, "")
	lines = append(lines, "", "## Review Queue")
	lines = appendTaskList(lines, s.ReviewPendingTasks, "")
	lines = append(lines, "", "## Accepted")
	lines = appendTaskList(lines, s.AcceptedTasks, "")
	lines = append(lines, "", "## Blockers")
	lines = appendTaskList(lines, s.BlockedTasks, "blocked_reason")
	lines = append(lines, "", "## Next")
	if len(s.WakeDue) > 0 {
		for _, task := range s.WakeDue {
			lines = append(lines, fmt.Sprintf("- wake `%s` at `%s` from `%s`", task.TaskID, task.WakeAt.Format(time.RFC3339), task.Path))
		}
	}
	if len(s.SelectedReady) > 0 {
		for _, task := range s.SelectedReady {
			lines = append(lines, fmt.Sprintf("- dispatch executor for `%s` from `%s`", task.TaskID, task.Path))
		}
	}
	if len(s.SelectedReview) > 0 {
		for _, task := range s.SelectedReview {
			lines = append(lines, fmt.Sprintf("- dispatch reviewer for `%s` from `%s`", task.TaskID, task.Path))
		}
	}
	if len(s.WakeDue) == 0 && len(s.SelectedReady) == 0 && len(s.SelectedReview) == 0 {
		lines = append(lines, "- no immediate next action")
	}
	return strings.Join(lines, "\n") + "\n"
}

func WriteLiveReport(root string, summary Summary) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("campaign repo path is empty")
	}
	path := filepath.Join(root, "reports", "live-report.md")
	if err := writeFileIfChanged(path, []byte(summary.LiveReportMarkdown())); err != nil {
		return "", err
	}
	return path, nil
}

func buildWakePrompt(repo Repository, task TaskDocument) string {
	return strings.TrimSpace(fmt.Sprintf(
		"Continue the repo-first campaign.\nCampaign repo: %s\nCampaign file: %s\nTask file: %s\nTask id: %s\nTask title: %s\nScheduled wake_at: %s\nWake prompt: %s\nRead the task context from the campaign repo, continue from the recorded state, then update the task files and live report. If the task is still blocked, explain the blocker clearly and request human help if needed.",
		repo.Root,
		repo.Campaign.Path,
		filepath.ToSlash(task.Path),
		task.Frontmatter.TaskID,
		task.Frontmatter.Title,
		task.WakeAt.Format(time.RFC3339),
		task.Frontmatter.WakePrompt,
	))
}

func wakeTaskStateKey(campaignID string, task TaskDocument) string {
	campaignID = blankForKey(strings.TrimSpace(campaignID))
	taskID := blankForKey(strings.TrimSpace(task.Frontmatter.TaskID))
	wakeAt := blankForKey(task.WakeAt.Format(time.RFC3339))
	return fmt.Sprintf("campaign_wake:%s:%s:%s", campaignID, taskID, wakeAt)
}

func blankForKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.ReplaceAll(value, " ", "_")
}

func blankForSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func appendTaskList(lines []string, tasks []TaskSummary, extra string) []string {
	if len(tasks) == 0 {
		return append(lines, "- none")
	}
	for _, task := range tasks {
		item := fmt.Sprintf("- `%s` [%s] %s", blankForSummary(task.TaskID), blankForSummary(task.Phase), blankForSummary(task.Path))
		if title := strings.TrimSpace(task.Title); title != "" {
			item += " " + title
		}
		if extra == "blocked_reason" && strings.TrimSpace(task.BlockedReason) != "" {
			item += " | " + task.BlockedReason
		}
		lines = append(lines, item)
	}
	return lines
}

func liveReportStatus(summary Summary) string {
	switch {
	case summary.ActiveCount > 0 || summary.ReadyCount > 0 || summary.ReviewPendingCount > 0 || len(summary.WakeDue) > 0:
		return "active"
	case summary.BlockedCount > 0 || summary.WaitingCount > 0:
		return "blocked"
	default:
		return "idle"
	}
}
