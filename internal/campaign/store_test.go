package campaign

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_CreateListAndGetCampaign(t *testing.T) {
	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "campaigns.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateCampaign(Campaign{
		Objective:         "improve speed and quality",
		Session:           SessionRoute{ScopeKey: "chat_id:oc_chat|thread:omt_1"},
		Creator:           Actor{UserID: "ou_user"},
		MaxParallelTrials: 3,
		Tags:              []string{"latency", "quality", "latency"},
	})
	if err != nil {
		t.Fatalf("create campaign failed: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected campaign id to be assigned")
	}
	if len(created.Tags) != 2 {
		t.Fatalf("expected tags to be deduplicated, got %#v", created.Tags)
	}

	listed, err := store.ListCampaigns("chat_id:oc_chat|thread:omt_1", "", 20)
	if err != nil {
		t.Fatalf("list campaigns failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected one campaign, got %d", len(listed))
	}

	loaded, err := store.GetCampaign(created.ID)
	if err != nil {
		t.Fatalf("get campaign failed: %v", err)
	}
	if loaded.Objective != "improve speed and quality" {
		t.Fatalf("unexpected objective: %q", loaded.Objective)
	}
}

func TestStore_UpsertTrialAndAppendRecords(t *testing.T) {
	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "campaigns.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateCampaign(Campaign{
		Objective:         "improve speed and quality",
		Session:           SessionRoute{ScopeKey: "chat_id:oc_chat|thread:omt_1"},
		Creator:           Actor{UserID: "ou_user"},
		MaxParallelTrials: 3,
	})
	if err != nil {
		t.Fatalf("create campaign failed: %v", err)
	}

	updated, trial, err := store.UpsertTrial(created.ID, Trial{
		ID:         "trial_1",
		Hypothesis: "distill smaller model",
		Status:     TrialStatusRunning,
	})
	if err != nil {
		t.Fatalf("upsert trial failed: %v", err)
	}
	if trial.ID != "trial_1" {
		t.Fatalf("unexpected trial id: %q", trial.ID)
	}
	if len(updated.Trials) != 1 {
		t.Fatalf("expected one trial, got %d", len(updated.Trials))
	}

	updated, guidance, err := store.AppendGuidance(created.ID, Guidance{
		Source:  "feishu",
		Command: "/alice hold",
	})
	if err != nil {
		t.Fatalf("append guidance failed: %v", err)
	}
	if guidance.ID == "" || len(updated.Guidance) != 1 {
		t.Fatalf("unexpected guidance append result: %#v", updated.Guidance)
	}

	updated, review, err := store.AppendReview(created.ID, Review{
		ReviewerID: "skeptic-reviewer",
		Verdict:    VerdictConcern,
		Summary:    "need long-context regression",
	})
	if err != nil {
		t.Fatalf("append review failed: %v", err)
	}
	if review.ID == "" || len(updated.Reviews) != 1 {
		t.Fatalf("unexpected review append result: %#v", updated.Reviews)
	}

	updated, pitfall, err := store.AppendPitfall(created.ID, Pitfall{
		Summary:        "spec decoding regresses long context",
		RelatedTrialID: "trial_1",
	})
	if err != nil {
		t.Fatalf("append pitfall failed: %v", err)
	}
	if pitfall.ID == "" || len(updated.Pitfalls) != 1 {
		t.Fatalf("unexpected pitfall append result: %#v", updated.Pitfalls)
	}
}
