package policy

import (
	"context"
	"strings"
	"time"

	"alice/internal/domain"
)

type StaticReception struct {
	IDGen domain.IDGenerator
}

func NewStaticReception(idgen domain.IDGenerator) *StaticReception {
	return &StaticReception{IDGen: idgen}
}

func (r *StaticReception) Assess(_ context.Context, in domain.ReceptionInput) (*domain.PromotionDecision, error) {
	now := time.Now().UTC()
	decision := &domain.PromotionDecision{
		DecisionID:          r.IDGen.New(domain.IDPrefixDecision),
		RequestID:           in.RequestID,
		IntentKind:          "general_query",
		RiskLevel:           "low",
		Result:              domain.PromotionResultAskFollowup,
		Confidence:          0.8,
		ReasonCodes:         []string{"default_assessment"},
		ProducedBy:          "reception.static",
		ProducedAt:          now,
		ProposedWorkflowIDs: nil,
	}

	switch {
	case in.Event.ScheduledTaskID != "" || in.Event.ControlObjectRef != "":
		decision.IntentKind = "schedule_management"
		decision.RiskLevel = "high"
		decision.ExternalWrite = true
		decision.CreatePersistentObject = true
		decision.MultiStep = true
		decision.ProposedWorkflowIDs = []string{"schedule-management"}
		decision.ReasonCodes = append(decision.ReasonCodes, "control_plane_schedule")
	case in.Event.WorkflowObjectRef != "":
		decision.IntentKind = "workflow_management"
		decision.RiskLevel = "high"
		decision.ExternalWrite = true
		decision.CreatePersistentObject = true
		decision.MultiStep = true
		decision.ProposedWorkflowIDs = []string{"workflow-management"}
		decision.ReasonCodes = append(decision.ReasonCodes, "control_plane_workflow")
	case in.Event.RepoRef != "" && (in.Event.IssueRef != "" || in.Event.PRRef != ""):
		decision.IntentKind = "issue_delivery"
		decision.RiskLevel = "medium"
		decision.ExternalWrite = true
		decision.MultiStep = true
		decision.MultiAgent = true
		decision.RecoveryRequired = true
		decision.ProposedWorkflowIDs = []string{"issue-delivery"}
		decision.ReasonCodes = append(decision.ReasonCodes, "repo_delivery")
	default:
		decision.IntentKind = "direct_query"
		decision.ExternalWrite = false
		decision.CreatePersistentObject = false
		decision.Async = false
		decision.MultiStep = false
		decision.MultiAgent = false
		decision.ReasonCodes = append(decision.ReasonCodes, "direct_default")
		if strings.Contains(strings.ToLower(in.Event.SourceRef), "research") {
			decision.IntentKind = "research_exploration"
			decision.Async = true
			decision.MultiStep = true
			decision.BudgetRequired = true
			decision.RecoveryRequired = true
			decision.ProposedWorkflowIDs = []string{"research-exploration"}
		}
	}
	return decision, nil
}
