package automation

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
)

func TestEngine_RunSystemTask(t *testing.T) {
	engine := NewEngine(nil, nil)
	engine.tick = 10 * time.Millisecond

	var mu sync.Mutex
	called := 0
	if err := engine.RegisterSystemTask("sys.test", 10*time.Millisecond, func(context.Context) {
		mu.Lock()
		called++
		mu.Unlock()
	}); err != nil {
		t.Fatalf("register system task failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if called == 0 {
		t.Fatal("expected system task to be called")
	}
}

func TestEngine_RunUserTask_RunLLM(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 1, 2, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type:            ActionTypeRunLLM,
			Text:            "定时播报",
			Prompt:          "请回复当前时间 {{now}}",
			Model:           "gpt-4.1-mini",
			Profile:         "worker-cheap",
			ReasoningEffort: "xhigh",
			Personality:     "pragmatic",
			MentionUserIDs:  []string{"ou_actor"},
		},
	})
	if err != nil {
		t.Fatalf("create run_llm task failed: %v", err)
	}

	sender := &senderStub{}
	runner := &llmRunnerStub{
		result: agentbridge.RunResult{Reply: "现在是 2026-02-23T10:01:02Z"},
	}
	engine := NewEngine(store, sender)
	engine.SetLLMRunner(runner)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(61 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	runner.mu.Lock()
	if runner.calls == 0 {
		runner.mu.Unlock()
		t.Fatal("expected run_llm task to invoke llm runner")
	}
	rawPrompt := "请回复当前时间 " + base.Add(61*time.Second).Local().Format(time.RFC3339)
	wantPrompt := "Preferred response style/personality: pragmatic.\n\n" + rawPrompt
	if runner.lastReq.UserText != wantPrompt {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm prompt: %q", runner.lastReq.UserText)
	}
	if runner.lastReq.Model != "gpt-4.1-mini" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm model: %q", runner.lastReq.Model)
	}
	if runner.lastReq.Profile != "worker-cheap" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm profile: %q", runner.lastReq.Profile)
	}
	if runner.lastReq.ReasoningEffort != "xhigh" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm reasoning effort: %q", runner.lastReq.ReasoningEffort)
	}
	if runner.lastReq.Personality != "pragmatic" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm personality: %q", runner.lastReq.Personality)
	}
	if runner.lastReq.Scene != "chat" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm scene: %q", runner.lastReq.Scene)
	}
	if got := runner.lastReq.Env["ALICE_RECEIVE_ID"]; got != "ou_actor" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm env receive id: %q", got)
	}
	runner.mu.Unlock()

	sender.mu.Lock()
	if sender.sendTextCalls == 0 {
		sender.mu.Unlock()
		t.Fatal("expected run_llm task to send text")
	}
	if sender.lastText == "" {
		sender.mu.Unlock()
		t.Fatal("expected non-empty run_llm dispatch text")
	}
	sender.mu.Unlock()

	stored, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if stored.LastResult == "" {
		t.Fatalf("expected last result to be recorded, task=%+v", stored)
	}
}

func TestEngine_RunUserTask_RunLLM_WorkSceneUsesCardAndWorkScene(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 1, 2, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title:    "daily summary",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_chat"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type:       ActionTypeRunLLM,
			Prompt:     "请总结当前状态",
			SessionKey: "chat_id:oc_chat|scene:work|thread:omt_alpha",
		},
	})
	if err != nil {
		t.Fatalf("create run_llm task failed: %v", err)
	}

	sender := &senderStub{}
	runner := &llmRunnerStub{
		result: agentbridge.RunResult{Reply: "work 已完成"},
	}
	engine := NewEngine(store, sender)
	engine.SetLLMRunner(runner)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(61 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	runner.mu.Lock()
	if runner.calls == 0 {
		runner.mu.Unlock()
		t.Fatal("expected run_llm task to invoke llm runner")
	}
	if runner.lastReq.Scene != "work" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm scene: %q", runner.lastReq.Scene)
	}
	if got := runner.lastReq.Env["ALICE_SESSION_KEY"]; got != "chat_id:oc_chat|scene:work|thread:omt_alpha" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm session key env: %q", got)
	}
	runner.mu.Unlock()

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.sendCardCalls != 1 {
		t.Fatalf("expected run_llm work task to send one card, got %d", sender.sendCardCalls)
	}
	if sender.sendTextCalls != 0 {
		t.Fatalf("expected run_llm work task to avoid text send, got %d", sender.sendTextCalls)
	}
	if !strings.Contains(sender.lastCard, "work 已完成") {
		t.Fatalf("unexpected card content: %q", sender.lastCard)
	}
	if got := cardTitleFromJSON(t, sender.lastCard); got != "daily summary" {
		t.Fatalf("unexpected card title: %q", got)
	}

	stored, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if stored.LastResult == "" {
		t.Fatalf("expected last result to be recorded, task=%+v", stored)
	}
}

func TestEngine_RunUserTask_RunLLM_PersistsStickyThreadID(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 1, 2, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action: Action{
			Type:           ActionTypeRunLLM,
			Prompt:         "hello",
			Provider:       "  CoDeX ",
			StateKey:       "campaign_dispatch:camp_demo:reviewer:T008:r1",
			ResumeThreadID: "thread_old",
		},
	})
	if err != nil {
		t.Fatalf("create run_llm task failed: %v", err)
	}

	sender := &senderStub{}
	runner := &llmRunnerStub{
		result: agentbridge.RunResult{
			Reply:        "updated",
			NextThreadID: "thread_new",
		},
	}
	engine := NewEngine(store, sender)
	engine.SetLLMRunner(runner)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(61 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	runner.mu.Lock()
	if runner.calls == 0 {
		runner.mu.Unlock()
		t.Fatal("expected run_llm task to invoke llm runner")
	}
	if runner.lastReq.ThreadID != "campaign_dispatch:camp_demo:reviewer:T008:r1" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm thread id: %q", runner.lastReq.ThreadID)
	}
	runner.mu.Unlock()

	stored, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if stored.Action.ResumeThreadID != "thread_new" {
		t.Fatalf("expected sticky thread id to persist, got %q", stored.Action.ResumeThreadID)
	}
}

func TestEngine_RunUserTask_UsesConfiguredTimeout(t *testing.T) {
	sender := &deadlineSenderStub{}
	engine := NewEngine(nil, sender)
	engine.SetUserTaskTimeout(2 * time.Minute)
	engine.SetLLMRunner(&llmRunnerStub{
		result: agentbridge.RunResult{Reply: "hello"},
	})

	start := time.Now()
	engine.runUserTask(context.Background(), Task{
		Scope:   Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:   Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{UserID: "ou_actor"},
		Action:  Action{Type: ActionTypeRunLLM, Prompt: "hello"},
	})

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if !sender.deadlineSet {
		t.Fatal("expected run context deadline to be set")
	}
	remaining := sender.deadline.Sub(start)
	if remaining < 119*time.Second || remaining > 121*time.Second {
		t.Fatalf("unexpected configured timeout window: %s", remaining)
	}
}
