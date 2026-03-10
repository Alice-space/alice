package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"alice/internal/domain"
)

func TestRegistryDigestStable(t *testing.T) {
	root := filepath.Join("..", "..", "configs", "workflows")

	r1 := NewRegistry(nil)
	if err := r1.LoadRoots(context.Background(), []string{root}); err != nil {
		t.Fatal(err)
	}
	ref1, ok := r1.Reference("issue-delivery", "v1")
	if !ok {
		t.Fatalf("issue-delivery@v1 not loaded")
	}

	r2 := NewRegistry(nil)
	if err := r2.LoadRoots(context.Background(), []string{root}); err != nil {
		t.Fatal(err)
	}
	ref2, ok := r2.Reference("issue-delivery", "v1")
	if !ok {
		t.Fatalf("issue-delivery@v1 not loaded")
	}
	if ref1.ManifestDigest != ref2.ManifestDigest {
		t.Fatalf("manifest digest drift: %s vs %s", ref1.ManifestDigest, ref2.ManifestDigest)
	}
}

func TestResolveUniqueCandidate(t *testing.T) {
	root := filepath.Join("..", "..", "configs", "workflows")
	reg := NewRegistry(nil)
	if err := reg.LoadRoots(context.Background(), []string{root}); err != nil {
		t.Fatal(err)
	}

	decision := &domain.PromotionDecision{
		DecisionID:          "dec_1",
		RequestID:           "req_1",
		IntentKind:          "issue_delivery",
		ProposedWorkflowIDs: []string{"issue-delivery"},
		RiskLevel:           "medium",
		Confidence:          0.9,
		ProducedAt:          time.Now().UTC(),
	}
	evt := &domain.ExternalEvent{RepoRef: "github:alice/repo", IssueRef: "12"}
	candidates, err := reg.ResolveCandidate(context.Background(), decision, evt)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	ref, ok := UniqueCandidate(candidates)
	if !ok || ref.WorkflowID != "issue-delivery" {
		t.Fatalf("unexpected candidate: %+v", ref)
	}
}

func TestResolveCandidateRejectsWhenAllowedMCPLimitsViolated(t *testing.T) {
	root := filepath.Join("..", "..", "configs", "workflows")
	reg := NewRegistry(nil)
	if err := reg.LoadRoots(context.Background(), []string{root}); err != nil {
		t.Fatal(err)
	}

	decision := &domain.PromotionDecision{
		DecisionID:          "dec_1",
		RequestID:           "req_1",
		IntentKind:          "workflow_management",
		ProposedWorkflowIDs: []string{"issue-delivery"},
		RiskLevel:           "medium",
		Confidence:          0.9,
		ProducedAt:          time.Now().UTC(),
	}
	evt := &domain.ExternalEvent{RepoRef: "github:alice/repo", IssueRef: "12"}
	candidates, err := reg.ResolveCandidate(context.Background(), decision, evt)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no candidate due to mcp/tool mismatch, got %d", len(candidates))
	}
}

func TestUnknownRequiredRefDoesNotPass(t *testing.T) {
	entry := EntrySpec{RequiredRefs: []string{"unknown_ref"}}
	evt := &domain.ExternalEvent{}
	if entryRefsSatisfied(entry, evt) {
		t.Fatalf("unknown required ref should not pass")
	}
	entry2 := EntrySpec{Requires: []string{"unknown_ref"}}
	if entryRequiresSatisfied(entry2, evt) {
		t.Fatalf("unknown requires should not pass")
	}
}
