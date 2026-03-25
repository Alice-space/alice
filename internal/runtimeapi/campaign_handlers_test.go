package runtimeapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/config"
	"github.com/Alice-space/alice/internal/mcpbridge"
)

func TestCampaignHandlers_CreateListAndMutate(t *testing.T) {
	store := campaign.NewStore(filepath.Join(t.TempDir(), "campaigns.db"))
	automationStore := automation.NewStore(filepath.Join(t.TempDir(), "automation.db"))
	server := NewServer("", "", nil, automationStore, store, config.Config{})
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

	repoDir := filepath.Join(t.TempDir(), "campaign-repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo dir failed: %v", err)
	}
	second, err := client.CreateCampaign(t.Context(), session, CreateCampaignRequest{
		Title:            "Delete Model-X",
		Objective:        "delete the campaign cleanly",
		CampaignRepoPath: repoDir,
	})
	if err != nil {
		t.Fatalf("create second campaign failed: %v", err)
	}
	secondCampaign, ok := second["campaign"].(map[string]any)
	if !ok {
		t.Fatalf("expected second campaign object, got %#v", second)
	}
	deleteID, _ := secondCampaign["id"].(string)
	if deleteID == "" {
		t.Fatalf("expected second campaign id, got %#v", secondCampaign)
	}

	task, err := automationStore.CreateTask(automation.Task{
		Title:      "campaign heartbeat " + deleteID,
		Scope:      automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		Route:      automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    automation.Actor{UserID: "ou_user"},
		ManageMode: automation.ManageModeCreatorOnly,
		Schedule:   automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 60},
		Action: automation.Action{
			Type:     automation.ActionTypeRunWorkflow,
			Workflow: "code_army",
			Prompt:   "/alice reconcile campaign " + deleteID,
			StateKey: "campaign_dispatch:" + deleteID + ":planner:r1",
		},
	})
	if err != nil {
		t.Fatalf("create related automation task failed: %v", err)
	}

	deleted, err := client.DeleteCampaign(t.Context(), otherThread, deleteID, true)
	if err != nil {
		t.Fatalf("delete campaign failed: %v", err)
	}
	deletedIDs, ok := deleted["deleted_automation_task_ids"].([]any)
	if !ok || len(deletedIDs) != 1 {
		t.Fatalf("expected one deleted automation task id, got %#v", deleted["deleted_automation_task_ids"])
	}
	if _, err := client.GetCampaign(t.Context(), otherThread, deleteID); err == nil {
		t.Fatal("expected deleted campaign to be missing")
	}
	persistedTask, err := automationStore.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get deleted task failed: %v", err)
	}
	if persistedTask.Status != automation.TaskStatusDeleted {
		t.Fatalf("expected related task to be deleted, got %s", persistedTask.Status)
	}
	if _, err := os.Stat(repoDir); !os.IsNotExist(err) {
		t.Fatalf("expected repo dir to be removed, stat err=%v", err)
	}
}
