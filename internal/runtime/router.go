package runtime

import (
	"fmt"

	"github.com/Alice-space/alice/internal/domain"
)

type DefaultRouter struct{}

func NewDefaultRouter() *DefaultRouter {
	return &DefaultRouter{}
}

func (r *DefaultRouter) Route(task domain.TaskSpec, classified domain.TaskClassifiedResult) (RuntimeType, string, error) {
	if classified.RoutingDecision == domain.RoutingPathReject {
		return "", "", fmt.Errorf("task rejected by classifier")
	}
	if classified.SuggestedRuntime != "" {
		s := RuntimeType(classified.SuggestedRuntime)
		if s == RuntimeTypeFastSingle || s == RuntimeTypeAgentic || s == RuntimeTypeLLM {
			return s, classified.SuggestedBudgetTier, nil
		}
	}

	switch {
	case task.TaskType == domain.TaskTypeQuery && task.WriteScope == domain.WriteScopeNone && !classified.NeedsRepoContext:
		return RuntimeTypeFastSingle, classified.SuggestedBudgetTier, nil
	case task.TaskType == domain.TaskTypeReview || task.TaskType == domain.TaskTypeReport:
		return RuntimeTypeLLM, classified.SuggestedBudgetTier, nil
	default:
		return RuntimeTypeAgentic, classified.SuggestedBudgetTier, nil
	}
}
