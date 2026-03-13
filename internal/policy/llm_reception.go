package policy

import (
	"context"
	"fmt"
	"time"

	"alice/internal/agent"
	"alice/internal/domain"
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

	// Execute with local agent
	result, err := r.agent.Execute(ctx, agent.ExecuteRequest{
		RequestID: in.RequestID,
		EventID:   in.Event.EventID,
		Stage:     "reception",
		Operation: "reception_assessment",
		Skills:    []string{"mcp-tool-output", "reception-assessment"},
		Input:     r.buildAssessmentInput(in),
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

func (r *LLMReception) buildAssessmentInput(in domain.ReceptionInput) map[string]any {
	return map[string]any{
		"request_id":     in.RequestID,
		"required_refs":  in.RequiredRefs,
		"route_target":   map[string]any{"kind": in.RouteTarget.Kind, "id": in.RouteTarget.ID},
		"route_snapshot": map[string]any{"matched_by": in.RouteSnapshot.MatchedBy, "route_keys": in.RouteSnapshot.RouteKeys},
		"event": map[string]any{
			"event_id":            in.Event.EventID,
			"event_type":          in.Event.EventType,
			"source_kind":         in.Event.SourceKind,
			"transport_kind":      in.Event.TransportKind,
			"source_ref":          in.Event.SourceRef,
			"actor_ref":           in.Event.ActorRef,
			"action_kind":         in.Event.ActionKind,
			"reply_to_event_id":   in.Event.ReplyToEventID,
			"conversation_id":     in.Event.ConversationID,
			"thread_id":           in.Event.ThreadID,
			"repo_ref":            in.Event.RepoRef,
			"issue_ref":           in.Event.IssueRef,
			"pr_ref":              in.Event.PRRef,
			"comment_ref":         in.Event.CommentRef,
			"scheduled_task_id":   in.Event.ScheduledTaskID,
			"control_object_ref":  in.Event.ControlObjectRef,
			"workflow_object_ref": in.Event.WorkflowObjectRef,
			"trace_id":            in.Event.TraceID,
		},
	}
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

	// Use structured output from MCP tool calls
	if result.StructuredOutput == nil {
		r.logger.Warn("no_structured_output_from_mcp", "request_id", in.RequestID, "output", result.Output)
		return r.simpleDecision(in, decision)
	}

	// Extract fields from MCP tool output
	// The agent extracts these from the MCP submit_promotion_decision tool call
	decision.IntentKind = getString(result.StructuredOutput, "intent_kind")
	decision.RiskLevel = getString(result.StructuredOutput, "risk_level")
	decision.ExternalWrite = getBool(result.StructuredOutput, "external_write")
	decision.CreatePersistentObject = getBool(result.StructuredOutput, "create_persistent_object")
	decision.Async = getBool(result.StructuredOutput, "async")
	decision.MultiStep = getBool(result.StructuredOutput, "multi_step")
	decision.MultiAgent = getBool(result.StructuredOutput, "multi_agent")
	decision.ApprovalRequired = getBool(result.StructuredOutput, "approval_required")
	decision.BudgetRequired = getBool(result.StructuredOutput, "budget_required")
	decision.RecoveryRequired = getBool(result.StructuredOutput, "recovery_required")
	decision.ProposedWorkflowIDs = getStringSlice(result.StructuredOutput, "proposed_workflow_ids")
	decision.ReasonCodes = getStringSlice(result.StructuredOutput, "reason_codes")
	decision.Confidence = getFloat64(result.StructuredOutput, "confidence")

	// Validate required fields
	if decision.IntentKind == "" {
		r.logger.Warn("missing_intent_kind_in_mcp_output", "request_id", in.RequestID, "output", result.StructuredOutput)
		return r.simpleDecision(in, decision)
	}

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

// Helper functions for extracting values from MCP structured output

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	case float64:
		return v != 0
	}
	return false
}

func getFloat64(m map[string]interface{}, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case string:
		var f float64
		fmt.Sscanf(v, "%f", &f)
		return f
	}
	return 0
}

func getStringSlice(m map[string]interface{}, key string) []string {
	if arr, ok := m[key].([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
