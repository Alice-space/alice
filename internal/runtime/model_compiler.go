package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/Alice-space/alice/internal/domain"
	"github.com/Alice-space/alice/internal/util"
)

type ModelIntentCompiler struct {
	model   StructuredModel
	profile string
}

func NewModelIntentCompiler(model StructuredModel, profile string) *ModelIntentCompiler {
	return &ModelIntentCompiler{model: model, profile: profile}
}

func (c *ModelIntentCompiler) Compile(ctx context.Context, actorID, channel, text string) (domain.IntentSpec, error) {
	if strings.TrimSpace(text) == "" {
		return domain.IntentSpec{}, fmt.Errorf("empty natural language input")
	}
	if c.model == nil {
		return domain.IntentSpec{}, fmt.Errorf("intent compiler model is nil")
	}

	resp, err := c.model.CompleteJSON(ctx, c.profile, intentCompilerInstruction, map[string]any{
		"text":    text,
		"actor_id": actorID,
		"channel": channel,
	})
	if err != nil {
		return domain.IntentSpec{}, err
	}

	intent := domain.IntentSpec{
		IntentID:             util.NewID("intent"),
		RawText:              text,
		ActorID:              actorID,
		Channel:              channel,
		IntentKind:           domain.IntentKind(asString(resp, "intent_kind", string(domain.IntentKindTaskRequest))),
		Scope:                domain.IntentScope(asString(resp, "scope", string(domain.IntentScopeProject))),
		ParsedPayload:        asMap(resp, "parsed_payload"),
		CompilerProfile:      c.profile,
		Confidence:           asFloat(resp, "confidence", 0.7),
		RiskLevel:            asString(resp, "risk_level", "medium"),
		RequiresConfirmation: asBool(resp, "requires_confirmation", false),
	}
	if intent.Confidence < 0.5 || intent.RiskLevel == "high" {
		intent.RequiresConfirmation = true
	}
	if err := domain.ValidateIntentSpec(intent); err != nil {
		return domain.IntentSpec{}, err
	}
	return intent, nil
}

const intentCompilerInstruction = `
You are an IntentCompiler for a research control-plane.
Return ONLY JSON keys:
- intent_kind: one of task_request, setting_change, state_query, approval_response, manual_signal
- scope: one of global, project, run
- parsed_payload: object
- confidence: number [0,1]
- risk_level: low|medium|high
- requires_confirmation: bool
Rules:
- classify by semantics, not keyword table
- if ambiguous, lower confidence and set requires_confirmation=true
`
