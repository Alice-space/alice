package automation

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStore_ScanWatchdogAlertsFindsOverdueAndStuckTasks(t *testing.T) {
	base := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	overdue, err := store.CreateTask(Task{
		Title:     "overdue task",
		Scope:     Scope{Kind: ScopeKindChat, ID: "oc_chat"},
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:   Actor{OpenID: "ou_actor"},
		Schedule:  Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:    Action{Type: ActionTypeRunLLM, Prompt: "ping"},
		NextRunAt: base.Add(-5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create overdue task failed: %v", err)
	}
	stuck, err := store.CreateTask(Task{
		Title:     "stuck task",
		Scope:     Scope{Kind: ScopeKindChat, ID: "oc_chat"},
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:   Actor{OpenID: "ou_actor"},
		Schedule:  Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:    Action{Type: ActionTypeRunLLM, Prompt: "ping"},
		NextRunAt: base.Add(time.Hour),
		LastRunAt: base.Add(-20 * time.Minute),
		Running:   true,
	})
	if err != nil {
		t.Fatalf("create stuck task failed: %v", err)
	}

	alerts, err := store.ScanWatchdogAlerts(base, 2*time.Minute, 10*time.Minute)
	if err != nil {
		t.Fatalf("scan watchdog alerts failed: %v", err)
	}
	if len(alerts) != 2 {
		t.Fatalf("expected two alerts, got %+v", alerts)
	}
	byID := map[string]TaskWatchdogAlert{}
	for _, alert := range alerts {
		byID[alert.Task.ID] = alert
	}
	if byID[overdue.ID].Kind != TaskWatchdogAlertOverdue || byID[overdue.ID].OverdueBy < 5*time.Minute {
		t.Fatalf("unexpected overdue alert: %+v", byID[overdue.ID])
	}
	if byID[stuck.ID].Kind != TaskWatchdogAlertStuck || byID[stuck.ID].RunningFor < 20*time.Minute {
		t.Fatalf("unexpected stuck alert: %+v", byID[stuck.ID])
	}
}

func TestEngine_RunWatchdogOnceSendsCooldownLimitedAlert(t *testing.T) {
	base := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }
	if _, err := store.CreateTask(Task{
		Title:     "daily report",
		Scope:     Scope{Kind: ScopeKindChat, ID: "oc_chat"},
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:   Actor{OpenID: "ou_actor"},
		Schedule:  Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:    Action{Type: ActionTypeRunLLM, Prompt: "ping"},
		NextRunAt: base.Add(-5 * time.Minute),
	}); err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	engine.RunWatchdogOnce(context.Background())
	engine.RunWatchdogOnce(context.Background())

	if sender.sendCardCalls != 1 {
		t.Fatalf("expected one cooldown-limited watchdog card, got %d", sender.sendCardCalls)
	}
	if !strings.Contains(sender.lastCard, "定时任务可能没有按时触发") || !strings.Contains(sender.lastCard, "daily report") {
		t.Fatalf("unexpected watchdog card: %q", sender.lastCard)
	}
}
