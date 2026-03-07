package runtime

import (
	"context"
	"fmt"

	"github.com/Alice-space/alice/internal/domain"
)

type ModelTaskClassifier struct {
	model   StructuredModel
	profile string
}

func NewModelTaskClassifier(model StructuredModel, profile string) *ModelTaskClassifier {
	return &ModelTaskClassifier{model: model, profile: profile}
}

func (c *ModelTaskClassifier) Classify(ctx context.Context, task domain.TaskSpec, memory []domain.MemoryRecord) (domain.TaskClassifiedResult, error) {
	if c.model == nil {
		return domain.TaskClassifiedResult{}, fmt.Errorf("task classifier model is nil")
	}

	resp, err := c.model.CompleteJSON(ctx, c.profile, classifierInstruction, map[string]any{
		"task":   task,
		"memory": memory,
	})
	if err != nil {
		return domain.TaskClassifiedResult{}, err
	}

	result := domain.TaskClassifiedResult{
		RoutingDecision:      domain.RoutingPath(asString(resp, "routing_decision", string(domain.RoutingPathTask))),
		RequiredCapabilities: asStringSlice(resp, "required_capabilities"),
		NeedsNetwork:         asBool(resp, "needs_network", false),
		NeedsSimpleExecution: asBool(resp, "needs_simple_execution", false),
		NeedsRepoContext:     asBool(resp, "needs_repo_context", false),
		NeedsMemoryRecall:    asBool(resp, "needs_memory_recall", true),
		RiskLevel:            asString(resp, "risk_level", "medium"),
		SuggestedBudgetTier:  asString(resp, "suggested_budget_tier", "default"),
		SuggestedRuntime:     asString(resp, "suggested_runtime", string(RuntimeTypeAgentic)),
		SuggestedRoles:       asStringSlice(resp, "suggested_roles"),
		Reason:               asString(resp, "reason", ""),
	}
	if task.TaskType == domain.TaskTypeQuery && task.WriteScope == domain.WriteScopeNone {
		if result.RoutingDecision == "" || result.RoutingDecision == domain.RoutingPathReject {
			result.RoutingDecision = domain.RoutingPathFast
		}
	}
	return result, nil
}

const classifierInstruction = `
You are TaskClassifier for a research control-plane.
Return ONLY JSON with keys:
- routing_decision: fast path|task path|reject
- required_capabilities: string[]
- needs_network: bool
- needs_simple_execution: bool
- needs_repo_context: bool
- needs_memory_recall: bool
- risk_level: low|medium|high
- suggested_budget_tier: string
- suggested_runtime: FastSingleAgentRuntime|AgenticRuntime|LLMRuntime
- suggested_roles: string[]
- reason: string
Prefer conservative upgrade over risky underestimation.
`
