package ops

import (
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
		out = appendUnique(out, normalized)
	}
	return out
}

func appendUnique(in []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return in
	}
	if containsValue(in, value) {
		return in
	}
	return append(in, value)
}

func containsValue(in []string, value string) bool {
	for _, item := range in {
		if item == value {
			return true
		}
	}
	return false
}

func removeValue(in []string, value string) []string {
	out := in[:0]
	for _, item := range in {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}
