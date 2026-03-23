package statusview

import (
	"errors"
	"testing"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
)

type automationStoreStub struct {
	tasks []automation.Task
	err   error
	scope automation.Scope
}

func (s *automationStoreStub) ListTasks(scope automation.Scope, _ string, _ int) ([]automation.Task, error) {
	s.scope = scope
	if s.err != nil {
		return nil, s.err
	}
	return append([]automation.Task(nil), s.tasks...), nil
}

type campaignStoreStub struct {
	items         []campaign.Campaign
	err           error
	visibilityKey string
}

func (s *campaignStoreStub) ListCampaigns(visibilityKey, _ string, _ int) ([]campaign.Campaign, error) {
	s.visibilityKey = visibilityKey
	if s.err != nil {
		return nil, s.err
	}
	return append([]campaign.Campaign(nil), s.items...), nil
}

type usageProviderStub struct {
	total UsageStats
	items []BotUsage
	err   error
	scope string
}

func (s *usageProviderStub) UsageForScope(scopeKey string) (UsageStats, []BotUsage, error) {
	s.scope = scopeKey
	return s.total, append([]BotUsage(nil), s.items...), s.err
}

func TestServiceQuery_GroupScopeAndCampaignFiltering(t *testing.T) {
	automationStore := &automationStoreStub{
		tasks: []automation.Task{{ID: "task_1"}},
	}
	campaignStore := &campaignStoreStub{
		items: []campaign.Campaign{
			{ID: "camp_running", Status: campaign.StatusRunning},
			{ID: "camp_hold", Status: campaign.StatusHold},
			{ID: "camp_merged", Status: campaign.StatusMerged},
		},
	}
	usageProvider := &usageProviderStub{
		total: UsageStats{InputTokens: 10, OutputTokens: 5},
		items: []BotUsage{{BotID: "alice", Usage: UsageStats{Turns: 1}}},
	}

	result := Service{
		Automation: automationStore,
		Campaigns:  campaignStore,
		Usage:      usageProvider,
	}.Query(Request{
		ChatType:      "group",
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		SenderOpenID:  "ou_actor",
		SessionKey:    "chat_id:oc_chat|thread:omt_1",
	})

	if got := result.ScopeLabel; got != "chat_id:oc_chat" {
		t.Fatalf("unexpected scope label: %q", got)
	}
	if got := automationStore.scope; got != (automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"}) {
		t.Fatalf("unexpected automation scope: %#v", got)
	}
	if got := campaignStore.visibilityKey; got != "chat_id:oc_chat" {
		t.Fatalf("unexpected visibility key: %q", got)
	}
	if got := usageProvider.scope; got != "chat_id:oc_chat" {
		t.Fatalf("unexpected usage scope: %q", got)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "task_1" {
		t.Fatalf("unexpected tasks: %#v", result.Tasks)
	}
	if len(result.Campaigns) != 2 {
		t.Fatalf("expected only active campaigns, got %#v", result.Campaigns)
	}
	if result.Campaigns[0].ID != "camp_running" || result.Campaigns[1].ID != "camp_hold" {
		t.Fatalf("unexpected active campaigns: %#v", result.Campaigns)
	}
	if result.TotalUsage.TotalTokens() != 15 {
		t.Fatalf("unexpected total usage: %#v", result.TotalUsage)
	}
	if len(result.BotUsages) != 1 || result.BotUsages[0].BotID != "alice" {
		t.Fatalf("unexpected bot usages: %#v", result.BotUsages)
	}
}

func TestServiceQuery_PrivateScopeRequiresActorID(t *testing.T) {
	result := Service{
		Automation: &automationStoreStub{},
	}.Query(Request{
		ChatType:      "p2p",
		ReceiveIDType: "user_id",
		ReceiveID:     "ou_actor",
	})

	if result.TaskError == nil {
		t.Fatal("expected missing actor id error")
	}
	if result.TaskError.Error() != "missing actor id" {
		t.Fatalf("unexpected task error: %v", result.TaskError)
	}
}

func TestServiceQuery_PropagatesProviderErrors(t *testing.T) {
	taskErr := errors.New("task store failed")
	campaignErr := errors.New("campaign store failed")
	usageErr := errors.New("usage failed")

	result := Service{
		Automation: &automationStoreStub{err: taskErr},
		Campaigns:  &campaignStoreStub{err: campaignErr},
		Usage:      &usageProviderStub{err: usageErr},
	}.Query(Request{
		ChatType:      "group",
		ReceiveIDType: "chat_id",
		ReceiveID:     "oc_chat",
		SenderOpenID:  "ou_actor",
	})

	if !errors.Is(result.TaskError, taskErr) {
		t.Fatalf("unexpected task error: %v", result.TaskError)
	}
	if !errors.Is(result.CampaignError, campaignErr) {
		t.Fatalf("unexpected campaign error: %v", result.CampaignError)
	}
	if !errors.Is(result.UsageError, usageErr) {
		t.Fatalf("unexpected usage error: %v", result.UsageError)
	}
}
