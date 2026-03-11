package ops

import (
	"slices"
	"strings"

	"alice/internal/domain"
)

// Utility functions for the read model.

func allowedApprovalDecisions(gateType string) []string {
	switch strings.ToLower(strings.TrimSpace(gateType)) {
	case "approval":
		return []string{"approve", "reject"}
	case "confirmation", "evaluation":
		return []string{"confirm", "reject"}
	case "budget":
		return []string{"resume-budget", "reject"}
	default:
		return nil
	}
}

func normalizeResumeOptions(options []string) []string {
	out := make([]string, 0, len(options))
	for _, option := range options {
		normalized := strings.ReplaceAll(string(domain.NormalizeHumanActionKind(option)), "_", "-")
		if normalized == "" {
			continue
		}
		if !slices.Contains(out, normalized) {
			out = append(out, normalized)
		}
	}
	return out
}

func removeValue(in []string, value string) []string {
	return slices.DeleteFunc(in, func(s string) bool { return s == value })
}
