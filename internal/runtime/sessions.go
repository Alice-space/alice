package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/domain"
)

type DefaultSessionFactory struct {
	model StructuredModel
}

func NewDefaultSessionFactory(model StructuredModel) *DefaultSessionFactory {
	return &DefaultSessionFactory{model: model}
}

func (f *DefaultSessionFactory) NewSession(rt RuntimeType, modelProfile string) (RuntimeSession, error) {
	switch rt {
	case RuntimeTypeFastSingle:
		return &FastSingleAgentRuntime{model: f.model, profile: modelProfile}, nil
	case RuntimeTypeAgentic:
		return &AgenticRuntime{model: f.model, profile: modelProfile}, nil
	case RuntimeTypeLLM:
		return &LLMRuntime{model: f.model, profile: modelProfile}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime: %s", rt)
	}
}

type FastSingleAgentRuntime struct {
	model   StructuredModel
	profile string
}

func (r *FastSingleAgentRuntime) Type() RuntimeType { return RuntimeTypeFastSingle }

func (r *FastSingleAgentRuntime) Plan(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "FastSingleAgent planner", pkt, "Provide a minimal read-only plan")
}

func (r *FastSingleAgentRuntime) Act(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "FastSingleAgent actor", pkt, "Solve with read-only steps; request escalation if writing is needed")
}

func (r *FastSingleAgentRuntime) Review(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "FastSingleAgent reviewer", pkt, "Review result quality and missing pieces")
}

func (r *FastSingleAgentRuntime) Summarize(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "FastSingleAgent summarizer", pkt, "Summarize succinctly")
}

func (r *FastSingleAgentRuntime) ClassifyCapability(ctx context.Context, pkt domain.ContextPacket) ([]string, error) {
	return []string{"read_only", "light_tool"}, nil
}

type AgenticRuntime struct {
	model   StructuredModel
	profile string
}

func (r *AgenticRuntime) Type() RuntimeType { return RuntimeTypeAgentic }

func (r *AgenticRuntime) Plan(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "Agentic planner", pkt, "Generate execution plan for coding and experiments")
}

func (r *AgenticRuntime) Act(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "Agentic actor", pkt, "Return concrete actions including command and code-change intents")
}

func (r *AgenticRuntime) Review(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "Agentic reviewer", pkt, "Review execution result and identify risks")
}

func (r *AgenticRuntime) Summarize(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "Agentic summarizer", pkt, "Summarize technical outcome")
}

func (r *AgenticRuntime) ClassifyCapability(ctx context.Context, pkt domain.ContextPacket) ([]string, error) {
	return []string{"code_write", "command_exec", "artifact_collect"}, nil
}

type LLMRuntime struct {
	model   StructuredModel
	profile string
}

func (r *LLMRuntime) Type() RuntimeType { return RuntimeTypeLLM }

func (r *LLMRuntime) Plan(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "LLM planner", pkt, "Produce strategy and constraints")
}

func (r *LLMRuntime) Act(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "LLM actor", pkt, "Provide analysis actions; avoid direct write operations")
}

func (r *LLMRuntime) Review(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "LLM reviewer", pkt, "Provide neutral review and risk analysis")
}

func (r *LLMRuntime) Summarize(ctx context.Context, pkt domain.ContextPacket) (domain.RuntimeResult, error) {
	return simpleModelResult(ctx, r.model, r.profile, "LLM summarizer", pkt, "Summarize with clear recommendation")
}

func (r *LLMRuntime) ClassifyCapability(ctx context.Context, pkt domain.ContextPacket) ([]string, error) {
	return []string{"language_only", "review"}, nil
}

func simpleModelResult(ctx context.Context, model StructuredModel, profile string, role string, pkt domain.ContextPacket, instruction string) (domain.RuntimeResult, error) {
	if model == nil {
		return domain.RuntimeResult{
			Role:               pkt.Role,
			Summary:            fmt.Sprintf("%s unavailable: model not configured", role),
			ProposedActions:    []string{"request_model_configuration"},
			Artifacts:          nil,
			MemoryCandidates:   nil,
			FollowupHint:       "configure model provider",
			RequiresEscalation: true,
		}, nil
	}

	resp, err := model.CompleteJSON(ctx, profile, instruction, map[string]any{"context_packet": pkt, "role": role})
	if err != nil {
		return domain.RuntimeResult{}, err
	}
	return domain.RuntimeResult{
		Role:               fallback(pkt.Role, asString(resp, "role", role)),
		Summary:            asString(resp, "summary", ""),
		ProposedActions:    asStringSlice(resp, "proposed_actions"),
		Artifacts:          asStringSlice(resp, "artifacts"),
		MemoryCandidates:   nil,
		FollowupHint:       asString(resp, "followup_hint", ""),
		RequiresEscalation: asBool(resp, "requires_escalation", false),
	}, nil
}

func fallback(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
