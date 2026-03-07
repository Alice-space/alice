package governance

import (
	"fmt"

	"github.com/Alice-space/alice/internal/domain"
)

type BudgetDecision struct {
	Allowed bool
	Reason  string
}

type BudgetGovernor struct{}

func NewBudgetGovernor() *BudgetGovernor {
	return &BudgetGovernor{}
}

func (g *BudgetGovernor) CanStartTask(project domain.ProjectSpec, task domain.TaskSpec, consumed domain.CostSummary, manualBypass bool) BudgetDecision {
	if manualBypass && project.BudgetPolicy.AllowManualBypass {
		return BudgetDecision{Allowed: true, Reason: "manual bypass"}
	}
	hard := project.BudgetPolicy.TokenHardLimit
	used := consumed.InputTokens + consumed.OutputTokens
	if hard > 0 && used >= hard {
		return BudgetDecision{Allowed: false, Reason: fmt.Sprintf("token hard limit reached (%d/%d)", used, hard)}
	}
	if project.BudgetPolicy.GPUMinuteLimit > 0 && consumed.GPUMinutes >= project.BudgetPolicy.GPUMinuteLimit {
		return BudgetDecision{Allowed: false, Reason: "gpu budget exhausted"}
	}
	if project.BudgetPolicy.MaxRetries >= 0 && task.RetryPolicy.MaxRetries > project.BudgetPolicy.MaxRetries {
		return BudgetDecision{Allowed: false, Reason: "task retry exceeds project policy"}
	}
	return BudgetDecision{Allowed: true, Reason: "within budget"}
}

func (g *BudgetGovernor) ShouldEscalateFastRun(result domain.RuntimeResult, limits domain.ResourceLimits, usedToolCalls int) bool {
	if result.RequiresEscalation {
		return true
	}
	if limits.MaxToolCalls > 0 && usedToolCalls > limits.MaxToolCalls {
		return true
	}
	return false
}
