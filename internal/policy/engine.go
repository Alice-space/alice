package policy

import (
	"fmt"

	"alice/internal/domain"
)

type Config struct {
	MinConfidence   float64
	DirectAllowlist []string
}

type Engine struct {
	minConfidence float64
	allowlist     map[string]struct{}
}

type Decision struct {
	Result      domain.PromotionResult
	ReasonCodes []string
}

func NewEngine(cfg Config) *Engine {
	if cfg.MinConfidence <= 0 {
		cfg.MinConfidence = 0.6
	}
	allowlist := make(map[string]struct{}, len(cfg.DirectAllowlist))
	for _, v := range cfg.DirectAllowlist {
		allowlist[v] = struct{}{}
	}
	return &Engine{minConfidence: cfg.MinConfidence, allowlist: allowlist}
}

func (e *Engine) DecidePromotion(in *domain.PromotionDecision) (Decision, error) {
	if in == nil {
		return Decision{}, fmt.Errorf("promotion decision is nil")
	}
	if err := domain.ValidatePromotionDecision(*in); err != nil {
		return Decision{}, err
	}

	if requiresPromote(in) {
		return Decision{Result: domain.PromotionResultPromote, ReasonCodes: append(in.ReasonCodes, "hard_rule_promote")}, nil
	}
	if in.Confidence < e.minConfidence {
		return Decision{Result: domain.PromotionResultAskFollowup, ReasonCodes: append(in.ReasonCodes, "low_confidence")}, nil
	}
	if _, ok := e.allowlist[in.IntentKind]; ok {
		return Decision{Result: domain.PromotionResultDirectAnswer, ReasonCodes: append(in.ReasonCodes, "direct_allowlist")}, nil
	}
	return Decision{Result: domain.PromotionResultAskFollowup, ReasonCodes: append(in.ReasonCodes, "intent_not_allowlisted")}, nil
}

func requiresPromote(in *domain.PromotionDecision) bool {
	return in.ExternalWrite ||
		in.CreatePersistentObject ||
		in.Async ||
		in.MultiStep ||
		in.MultiAgent ||
		in.ApprovalRequired ||
		in.BudgetRequired ||
		in.RecoveryRequired
}
