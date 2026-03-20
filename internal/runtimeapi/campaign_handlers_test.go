package runtimeapi

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/mcpbridge"
)

func TestCampaignHandlers_CreateListAndMutate(t *testing.T) {
	store := campaign.NewStore(filepath.Join(t.TempDir(), "campaigns.db"))
	server := NewServer("", "", nil, nil, store)
	httpServer := httptest.NewServer(server.engine)
	defer httpServer.Close()

	client := NewClient(httpServer.URL, "")
	session := mcpbridge.SessionContext{
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		ActorUserID:   "ou_user",
		ChatType:      "group",
		SessionKey:    "chat_id:oc_chat|thread:omt_1",
	}
	otherThread := session
	otherThread.SessionKey = "chat_id:oc_chat|thread:omt_2"
	otherChat := session
	otherChat.ReceiveID = "oc_other"
	otherChat.SessionKey = "chat_id:oc_other|thread:omt_3"

	created, err := client.CreateCampaign(t.Context(), session, CreateCampaignRequest{
		Title:             "Optimize Model-X",
		Objective:         "improve speed and quality",
		Repo:              "lizhihao/fastecalsim",
		IssueIID:          "218",
		MaxParallelTrials: 3,
	})
	if err != nil {
		t.Fatalf("create campaign failed: %v", err)
	}
	createdCampaign, ok := created["campaign"].(map[string]any)
	if !ok {
		t.Fatalf("expected campaign object, got %#v", created)
	}
	campaignID, _ := createdCampaign["id"].(string)
	if campaignID == "" {
		t.Fatalf("expected campaign id, got %#v", createdCampaign)
	}

	listed, err := client.ListCampaigns(t.Context(), otherThread, "", 20)
	if err != nil {
		t.Fatalf("list campaigns failed: %v", err)
	}
	if got := int(listed["count"].(float64)); got != 1 {
		t.Fatalf("expected one campaign, got %d", got)
	}

	hidden, err := client.ListCampaigns(t.Context(), otherChat, "", 20)
	if err != nil {
		t.Fatalf("list campaigns in other chat failed: %v", err)
	}
	if got := int(hidden["count"].(float64)); got != 0 {
		t.Fatalf("expected other chat to see zero campaigns, got %d", got)
	}

	fetched, err := client.GetCampaign(t.Context(), otherThread, campaignID)
	if err != nil {
		t.Fatalf("get campaign from sibling thread failed: %v", err)
	}
	if fetched["campaign"] == nil {
		t.Fatalf("expected campaign payload, got %#v", fetched)
	}

	updated, err := client.UpsertTrial(t.Context(), otherThread, campaignID, UpsertTrialRequest{
		Trial: campaign.Trial{
			ID:         "trial-1",
			Hypothesis: "distill smaller model",
			Status:     campaign.TrialStatusRunning,
		},
	})
	if err != nil {
		t.Fatalf("upsert trial failed: %v", err)
	}
	if updated["trial"] == nil {
		t.Fatalf("expected trial in response, got %#v", updated)
	}

	guidance, err := client.AddGuidance(t.Context(), otherThread, campaignID, AddGuidanceRequest{
		Guidance: campaign.Guidance{
			Source:  "feishu",
			Command: "/alice hold",
		},
	})
	if err != nil {
		t.Fatalf("add guidance failed: %v", err)
	}
	if guidance["guidance"] == nil {
		t.Fatalf("expected guidance in response, got %#v", guidance)
	}
}
