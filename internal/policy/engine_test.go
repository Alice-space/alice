package policy

import (
	"testing"
	"time"

	"alice/internal/domain"
)

func TestPromotionHardRulePromote(t *testing.T) {
	engine := NewEngine(Config{MinConfidence: 0.7, DirectAllowlist: []string{"direct_query"}})
	in := &domain.PromotionDecision{
		DecisionID:    "dec_1",
		RequestID:     "req_1",
		IntentKind:    "direct_query",
		ExternalWrite: true,
		Confidence:    0.99,
		ProducedAt:    time.Now().UTC(),
	}
	got, err := engine.DecidePromotion(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Result != domain.PromotionResultPromote {
		t.Fatalf("expected promote, got %s", got.Result)
	}
}

func TestPromotionDirectAllowlist(t *testing.T) {
	engine := NewEngine(Config{MinConfidence: 0.7, DirectAllowlist: []string{"direct_query"}})
	in := &domain.PromotionDecision{
		DecisionID: "dec_1",
		RequestID:  "req_1",
		IntentKind: "direct_query",
		Confidence: 0.8,
		ProducedAt: time.Now().UTC(),
	}
	got, err := engine.DecidePromotion(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Result != domain.PromotionResultDirectAnswer {
		t.Fatalf("expected direct answer, got %s", got.Result)
	}
}
