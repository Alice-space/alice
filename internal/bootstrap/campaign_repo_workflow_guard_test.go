package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
)

func TestGuardCampaignRepoWorkflowTask_BlocksGenericReconcileDuringPlanning(t *testing.T) {
	repoDir := t.TempDir()
	campaignDoc := `---
campaign_id: camp_demo
title: Demo
objective: Demo objective
status: planned
campaign_repo_path: ` + repoDir + `
plan_round: 1
plan_status: planning
---
`
	if err := os.WriteFile(filepath.Join(repoDir, "campaign.md"), []byte(campaignDoc), 0o644); err != nil {
		t.Fatalf("write campaign.md failed: %v", err)
	}

	store := campaign.NewStore(filepath.Join(t.TempDir(), "campaigns.db"))
	if _, err := store.CreateCampaign(campaign.Campaign{
		ID:               "camp_demo",
		Title:            "Demo",
		Objective:        "Demo objective",
		CampaignRepoPath: repoDir,
		Session:          campaign.SessionRoute{ScopeKey: "chat_id:oc_chat|scene:work|thread:omt_demo", ReceiveIDType: "chat_id", ReceiveID: "oc_chat", ChatType: "group"},
		Creator:          campaign.Actor{OpenID: "ou_actor"},
		Status:           campaign.StatusPlanned,
	}); err != nil {
		t.Fatalf("create campaign failed: %v", err)
	}

	builder := &connectorRuntimeBuilder{campaignStore: store}
	decision, err := builder.guardCampaignRepoWorkflowTask(context.Background(), automation.Task{
		Action: automation.Action{
			Type:     automation.ActionTypeRunWorkflow,
			Workflow: "code_army",
			StateKey: "code_army:camp_demo:heartbeat",
			Prompt:   "/alice reconcile campaign camp_demo",
		},
	})
	if err != nil {
		t.Fatalf("guard returned err=%v", err)
	}
	if !decision.Block {
		t.Fatal("expected generic reconcile worker to be blocked")
	}
	if !strings.Contains(decision.Message, "repo-reconcile camp_demo") {
		t.Fatalf("expected actionable repo-reconcile hint, got %q", decision.Message)
	}
}

func TestGuardCampaignRepoWorkflowTask_AllowsOfficialDispatchTask(t *testing.T) {
	builder := &connectorRuntimeBuilder{}
	decision, err := builder.guardCampaignRepoWorkflowTask(context.Background(), automation.Task{
		Action: automation.Action{
			Type:     automation.ActionTypeRunWorkflow,
			Workflow: "code_army",
			StateKey: "campaign_dispatch:camp_demo:planner:r1",
			Prompt:   "/alice reconcile campaign camp_demo",
		},
	})
	if err != nil {
		t.Fatalf("guard returned err=%v", err)
	}
	if decision.Block {
		t.Fatal("expected official dispatch task to bypass the generic plan gate")
	}
}
