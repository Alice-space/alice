package campaign

import "testing"

func TestNormalizeCampaign_Defaults(t *testing.T) {
	item := NormalizeCampaign(Campaign{
		Objective: " improve latency ",
		Session:   SessionRoute{ScopeKey: " chat_id:oc_thread "},
		Creator:   Actor{UserID: " ou_user "},
	})
	if item.ManageMode != ManageModeCreatorOnly {
		t.Fatalf("unexpected manage mode: %q", item.ManageMode)
	}
	if item.Status != StatusPlanned {
		t.Fatalf("unexpected status: %q", item.Status)
	}
	if item.MaxParallelTrials != 1 {
		t.Fatalf("unexpected max parallel trials: %d", item.MaxParallelTrials)
	}
	if item.Objective != "improve latency" {
		t.Fatalf("unexpected objective: %q", item.Objective)
	}
	if item.Session.ScopeKey != "chat_id:oc_thread" {
		t.Fatalf("unexpected scope key: %q", item.Session.ScopeKey)
	}
	if item.Creator.UserID != "ou_user" {
		t.Fatalf("unexpected creator user id: %q", item.Creator.UserID)
	}
}

func TestSessionRouteVisibilityKey(t *testing.T) {
	if got := (SessionRoute{
		ScopeKey:      "chat_id:oc_chat|thread:omt_1",
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
	}).VisibilityKey(); got != "chat_id:oc_chat" {
		t.Fatalf("unexpected receive-id visibility key: %q", got)
	}

	if got := (SessionRoute{
		ScopeKey: "chat_id:oc_chat|scene:work|seed:om_root",
	}).VisibilityKey(); got != "chat_id:oc_chat" {
		t.Fatalf("unexpected scope fallback visibility key: %q", got)
	}
}

func TestValidateCampaign(t *testing.T) {
	item := Campaign{
		ID:                "camp_1",
		Objective:         "improve speed and quality",
		Session:           SessionRoute{ScopeKey: "chat_id:oc_thread|thread:omt_1"},
		Creator:           Actor{UserID: "ou_user"},
		MaxParallelTrials: 3,
		Trials: []Trial{
			{ID: "trial_1", Status: TrialStatusRunning},
			{ID: "trial_2", Status: TrialStatusCandidate},
		},
		CurrentWinnerTrialID: "trial_2",
	}
	if err := ValidateCampaign(item); err != nil {
		t.Fatalf("expected campaign to be valid, got %v", err)
	}
}

func TestValidateCampaign_RejectsUnknownWinner(t *testing.T) {
	item := Campaign{
		ID:                   "camp_1",
		Objective:            "improve speed and quality",
		Session:              SessionRoute{ScopeKey: "chat_id:oc_thread|thread:omt_1"},
		Creator:              Actor{UserID: "ou_user"},
		MaxParallelTrials:    3,
		CurrentWinnerTrialID: "missing",
	}
	if err := ValidateCampaign(item); err == nil {
		t.Fatal("expected missing winner trial to be rejected")
	}
}
