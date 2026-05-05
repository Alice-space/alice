package statusview

import (
	"errors"
	"testing"

	"github.com/Alice-space/alice/internal/automation"
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

func TestServiceQuery_GroupScope(t *testing.T) {
	automationStore := &automationStoreStub{
		tasks: []automation.Task{{ID: "task_1"}},
	}
	usageProvider := &usageProviderStub{
		total: UsageStats{InputTokens: 10, OutputTokens: 5},
		items: []BotUsage{{BotID: "alice", Usage: UsageStats{Turns: 1}}},
	}

	result := Service{
		Automation: automationStore,
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
	if got := automationStore.scope; got != (automation.Scope{Kind: automation.ScopeKindChat, ID: "chat_id:oc_chat|thread:omt_1"}) {
		t.Fatalf("unexpected automation scope: %#v", got)
	}
	if got := usageProvider.scope; got != "chat_id:oc_chat" {
		t.Fatalf("unexpected usage scope: %q", got)
	}
	if len(result.Tasks) != 1 || result.Tasks[0].ID != "task_1" {
		t.Fatalf("unexpected tasks: %#v", result.Tasks)
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
	usageErr := errors.New("usage failed")

	result := Service{
		Automation: &automationStoreStub{err: taskErr},
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
	if !errors.Is(result.UsageError, usageErr) {
		t.Fatalf("unexpected usage error: %v", result.UsageError)
	}
}
