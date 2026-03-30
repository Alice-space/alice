package campaignrepo

import (
	"strings"

	"github.com/Alice-space/alice/internal/storeutil"
)

func normalizeTaskStatus(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "", "todo", "draft", "planned":
		return TaskStatusDraft
	case "ready", "queued":
		return TaskStatusReady
	case "inprogress", "in_progress", "running", "executing":
		return TaskStatusExecuting
	case "review_pending", "pending_review", "awaiting_review":
		return TaskStatusReviewPending
	case "review", "reviewing":
		return TaskStatusReviewing
	case "rework", "changes_requested", "needs_changes":
		return TaskStatusRework
	case "accepted", "approved":
		return TaskStatusAccepted
	case "blocked", "hold", "paused":
		return TaskStatusBlocked
	case "waiting", "waiting_external", "sleeping":
		return TaskStatusWaitingExternal
	case "done", "completed", "complete", "merged":
		return TaskStatusDone
	case "rejected", "canceled", "cancelled", "aborted":
		return TaskStatusRejected
	default:
		return value
	}
}

func normalizeRoleConfig(cfg RoleConfig) RoleConfig {
	cfg.Role = strings.TrimSpace(cfg.Role)
	cfg.Provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Profile = strings.TrimSpace(cfg.Profile)
	cfg.Workflow = strings.ToLower(strings.TrimSpace(cfg.Workflow))
	cfg.ReasoningEffort = strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	cfg.Personality = strings.ToLower(strings.TrimSpace(cfg.Personality))
	return cfg
}

func normalizeReviewStatus(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "", "pending", "review_pending", "pending_review", "awaiting_review":
		return "pending"
	case "queued":
		return "queued"
	case "reviewing", "in_review":
		return "reviewing"
	case "approved", "approve":
		return "approved"
	case "changes_requested", "rework", "concern":
		return "changes_requested"
	case "blocked", "reject", "rejected":
		return "blocked"
	default:
		return value
	}
}

func normalizeReviewVerdict(raw string, blocking bool) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	if blocking && value == "" {
		return "blocking"
	}
	switch value {
	case "":
		if blocking {
			return "blocking"
		}
		return ""
	case "concern":
		return "concern"
	case "approve", "approved", "accept", "accepted", "merge", "pass":
		return "approve"
	case "blocking", "block", "blocked":
		return "blocking"
	case "reject", "rejected", "abort", "aborted":
		return "reject"
	case "needs_more_evidence", "more_evidence":
		return "concern"
	default:
		if blocking {
			return "blocking"
		}
		return value
	}
}

func normalizeStringList(values []string) []string {
	return storeutil.UniqueNonEmptyStrings(values)
}
