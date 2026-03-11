package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alice/internal/agent"
	"alice/internal/domain"
	"alice/internal/prompts"
)

// LLMReception uses local LLM agent to assess promotion decisions.
type LLMReception struct {
	agent  *agent.LocalAgent
	idgen  domain.IDGenerator
	logger Logger
}

// Logger interface for reception.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// NewLLMReception creates a new LLM-based reception.
func NewLLMReception(agent *agent.LocalAgent, idgen domain.IDGenerator, logger Logger) *LLMReception {
	return &LLMReception{
		agent:  agent,
		idgen:  idgen,
		logger: logger,
	}
}

// Assess uses LLM to analyze the input and make a promotion decision.
func (r *LLMReception) Assess(ctx context.Context, in domain.ReceptionInput) (*domain.PromotionDecision, error) {
	start := time.Now().UTC()
	r.logger.Info("reception_started", "request_id", in.RequestID, "event_id", in.Event.EventID, "source_ref", in.Event.SourceRef)

	// Build the assessment prompt
	prompt := r.buildAssessmentPrompt(in)

	// Execute with local agent
	result, err := r.agent.Execute(ctx, agent.ExecuteRequest{
		Task:         prompt,
		Skill:        "reception-assessment",
		SystemPrompt: r.assessmentSystemPrompt(),
		Constraints: agent.ExecuteConstraints{
			ReadOnly: true,
		},
	})

	if err != nil {
		r.logger.Error("reception_agent_error", "request_id", in.RequestID, "error", err.Error(), "duration_ms", time.Since(start).Milliseconds())
		// Fall back to static decision on error
		return r.fallbackDecision(in), nil
	}

	// Parse the structured output
	decision := r.parseDecision(result, in)

	r.logger.Info("reception_completed", "request_id", in.RequestID, "decision_id", decision.DecisionID, "intent_kind", decision.IntentKind, "risk_level", decision.RiskLevel, "result", decision.Result, "confidence", decision.Confidence, "duration_ms", time.Since(start).Milliseconds())

	return decision, nil
}

func (r *LLMReception) buildAssessmentPrompt(in domain.ReceptionInput) string {
	data := struct {
		UserInput       string
		RepoRef         string
		IssueRef        string
		PRRef           string
		ScheduledTaskID string
		MatchedBy       string
		RouteKeys       []string
	}{
		UserInput:       in.Event.SourceRef,
		RepoRef:         in.Event.RepoRef,
		IssueRef:        in.Event.IssueRef,
		PRRef:           in.Event.PRRef,
		ScheduledTaskID: in.Event.ScheduledTaskID,
		MatchedBy:       in.RouteSnapshot.MatchedBy,
		RouteKeys:       in.RouteSnapshot.RouteKeys,
	}

	result, err := prompts.Render(prompts.ReceptionAssessmentTask, data)
	if err != nil {
		// Fallback to simple prompt on error
		return fmt.Sprintf("Analyze the user request: %s", in.Event.SourceRef)
	}
	return result
}

func (r *LLMReception) assessmentSystemPrompt() string {
	return prompts.Get(prompts.ReceptionAssessmentSystem)
}

func (r *LLMReception) parseDecision(result *agent.ExecuteResult, in domain.ReceptionInput) *domain.PromotionDecision {
	now := time.Now().UTC()
	decision := &domain.PromotionDecision{
		DecisionID: r.idgen.New(domain.IDPrefixDecision),
		RequestID:  in.RequestID,
		Result:     domain.PromotionResultDirectAnswer, // default
		ProducedBy: "reception.llm",
		ProducedAt: now,
	}

	// Try to extract JSON from output
	output := result.Output
	if result.StructuredOutput != nil {
		// Agent already parsed JSON for us
		if data, err := json.Marshal(result.StructuredOutput); err == nil {
			output = string(data)
		}
	}

	var parsed struct {
		IntentKind          string   `json:"intent_kind"`
		RiskLevel           string   `json:"risk_level"`
		ExternalWrite       bool     `json:"external_write"`
		CreatePersistentObj bool     `json:"create_persistent_object"`
		Async               bool     `json:"async"`
		MultiStep           bool     `json:"multi_step"`
		MultiAgent          bool     `json:"multi_agent"`
		ApprovalRequired    bool     `json:"approval_required"`
		BudgetRequired      bool     `json:"budget_required"`
		RecoveryRequired    bool     `json:"recovery_required"`
		ProposedWorkflowIDs []string `json:"proposed_workflow_ids"`
		ReasonCodes         []string `json:"reason_codes"`
		Confidence          float64  `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(extractJSON(output)), &parsed); err != nil {
		r.logger.Warn("failed_to_parse_llm_decision", "request_id", in.RequestID, "output", output, "error", err.Error())
		// Fall back to simple decision
		return r.simpleDecision(in, decision)
	}

	decision.IntentKind = parsed.IntentKind
	decision.RiskLevel = parsed.RiskLevel
	decision.ExternalWrite = parsed.ExternalWrite
	decision.CreatePersistentObject = parsed.CreatePersistentObj
	decision.Async = parsed.Async
	decision.MultiStep = parsed.MultiStep
	decision.MultiAgent = parsed.MultiAgent
	decision.ApprovalRequired = parsed.ApprovalRequired
	decision.BudgetRequired = parsed.BudgetRequired
	decision.RecoveryRequired = parsed.RecoveryRequired
	decision.ProposedWorkflowIDs = parsed.ProposedWorkflowIDs
	decision.ReasonCodes = parsed.ReasonCodes
	decision.Confidence = parsed.Confidence

	// Determine result based on parsed fields
	if decision.ExternalWrite || decision.MultiStep || decision.ApprovalRequired ||
		decision.BudgetRequired || decision.RecoveryRequired || len(decision.ProposedWorkflowIDs) > 0 {
		decision.Result = domain.PromotionResultPromote
	} else {
		decision.Result = domain.PromotionResultDirectAnswer
	}

	return decision
}

func (r *LLMReception) simpleDecision(in domain.ReceptionInput, base *domain.PromotionDecision) *domain.PromotionDecision {
	switch {
	case in.Event.RepoRef != "" && (in.Event.IssueRef != "" || in.Event.PRRef != ""):
		base.IntentKind = "issue_delivery"
		base.RiskLevel = "medium"
		base.ExternalWrite = true
		base.MultiStep = true
		base.Result = domain.PromotionResultPromote
		base.ProposedWorkflowIDs = []string{"issue-delivery"}
		base.ReasonCodes = []string{"repo_delivery"}
		base.Confidence = 0.85

	default:
		base.IntentKind = "direct_query"
		base.RiskLevel = "low"
		base.Result = domain.PromotionResultDirectAnswer
		base.ReasonCodes = []string{"general_query"}
		base.Confidence = 0.8
	}

	return base
}

func (r *LLMReception) fallbackDecision(in domain.ReceptionInput) *domain.PromotionDecision {
	return r.simpleDecision(in, &domain.PromotionDecision{
		DecisionID: r.idgen.New(domain.IDPrefixDecision),
		RequestID:  in.RequestID,
		Result:     domain.PromotionResultDirectAnswer,
		ProducedBy: "reception.fallback",
		ProducedAt: time.Now().UTC(),
	})
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return "{}"
	}
	end := strings.LastIndex(s, "}")
	if end == -1 || end <= start {
		return "{}"
	}
	return s[start : end+1]
}
