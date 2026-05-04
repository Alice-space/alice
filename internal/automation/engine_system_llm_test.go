package automation

import (
	"context"
	"path/filepath"
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
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "请回复当前时间 {{now}}",
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
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
		t.Fatal("expected task to invoke llm runner")
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
		t.Fatal("expected task to send text")
	}
	if sender.lastText == "" {
		sender.mu.Unlock()
		t.Fatal("expected non-empty dispatch text")
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

func TestEngine_RunUserTask_RunLLM_ForwardsProgressMessages(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 1, 2, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "progress test",
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	claimAt := base.Add(61 * time.Second)
	store.now = func() time.Time { return claimAt }
	claimed, err := store.ClaimDueTasks(claimAt, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim task failed: err=%v len=%d", err, len(claimed))
	}

	sender := &senderStub{}
	runner := &llmRunnerStub{
		progress: []string{"first update", "first update", "[file_change] changed.txt", "final answer"},
		result:   agentbridge.RunResult{Reply: "final answer"},
	}
	engine := NewEngine(store, sender)
	engine.SetLLMRunner(runner)
	engine.now = func() time.Time { return claimAt }

	engine.runUserTask(context.Background(), claimed[0])

	sender.mu.Lock()
	gotTexts := append([]string(nil), sender.texts...)
	sender.mu.Unlock()
	wantTexts := []string{
		"定时任务「未命名任务」开始运行...",
		"first update",
		"final answer",
	}
	if len(gotTexts) != len(wantTexts) {
		t.Fatalf("sent texts = %#v, want %#v", gotTexts, wantTexts)
	}
	for i := range wantTexts {
		if gotTexts[i] != wantTexts[i] {
			t.Fatalf("sent texts = %#v, want %#v", gotTexts, wantTexts)
		}
	}

	stored, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if stored.LastResult == "" {
		t.Fatalf("expected last result to be recorded, task=%+v", stored)
	}
	if stored.SourceMessageID == "" {
		t.Fatalf("expected source_message_id to be bootstrapped from progress send, task=%+v", stored)
	}
}

func TestEngine_RunUserTask_RunLLM_WorkScene(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 1, 2, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title:      "daily summary",
		Scope:      Scope{Kind: ScopeKindChat, ID: "oc_chat"},
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "ou_actor"},
		Schedule:   Schedule{EverySeconds: 60},
		Prompt:     "请总结当前状态",
		SessionKey: "chat_id:oc_chat|scene:work|seed:om_alpha",
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
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
		t.Fatal("expected task to invoke llm runner")
	}
	if runner.lastReq.Scene != "work" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm scene: %q", runner.lastReq.Scene)
	}
	if got := runner.lastReq.Env["ALICE_SESSION_KEY"]; got != "chat_id:oc_chat|scene:work|seed:om_alpha" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm session key env: %q", got)
	}
	runner.mu.Unlock()

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.sendTextCalls < 1 {
		t.Fatalf("expected at least 1 text send, got %d", sender.sendTextCalls)
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
		Scope:          Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:          Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:        Actor{UserID: "ou_actor"},
		Schedule:       Schedule{EverySeconds: 60},
		Prompt:         "hello",
		ResumeThreadID: "thread_old",
		Fresh:          false,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
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
		t.Fatal("expected task to invoke llm runner")
	}
	if runner.lastReq.ThreadID != "thread_old" {
		runner.mu.Unlock()
		t.Fatalf("unexpected llm thread id: %q", runner.lastReq.ThreadID)
	}
	runner.mu.Unlock()

	stored, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if stored.ResumeThreadID != "thread_new" {
		t.Fatalf("expected sticky thread id to persist, got %q", stored.ResumeThreadID)
	}
}

func TestEngine_RunUserTask_RunLLM_FreshSkipsThread(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 1, 2, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	_, err := store.CreateTask(Task{
		Scope:          Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:          Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:        Actor{UserID: "ou_actor"},
		Schedule:       Schedule{EverySeconds: 60},
		Prompt:         "hello",
		ResumeThreadID: "thread_old",
		Fresh:          true,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	sender := &senderStub{}
	runner := &llmRunnerStub{
		result: agentbridge.RunResult{
			Reply:        "fresh",
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
		t.Fatal("expected task to invoke llm runner")
	}
	if runner.lastReq.ThreadID != "" {
		runner.mu.Unlock()
		t.Fatalf("expected empty thread id for fresh task, got %q", runner.lastReq.ThreadID)
	}
	runner.mu.Unlock()
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
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{EverySeconds: 60},
		Prompt:   "hello",
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
