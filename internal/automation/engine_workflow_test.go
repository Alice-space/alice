package automation

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEngine_RunUserTask_RunWorkflow(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 2, 3, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 1},
		Action: Action{
			Type:            ActionTypeRunWorkflow,
			Text:            "流程播报",
			Prompt:          "请推进代码军队流程",
			Workflow:        "code_army",
			StateKey:        "project_alpha",
			SessionKey:      "chat_id:oc_chat|thread:omt_alpha",
			Model:           "gpt-4.1-mini",
			Profile:         "workflow-runner",
			ReasoningEffort: "high",
			Personality:     "pragmatic",
		},
	})
	if err != nil {
		t.Fatalf("create run_workflow task failed: %v", err)
	}

	sender := &senderStub{}
	runner := &workflowRunnerStub{
		result: WorkflowRunResult{Message: "workflow 已完成"},
	}
	engine := NewEngine(store, sender)
	engine.SetWorkflowRunner(runner)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(2 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	runner.mu.Lock()
	if runner.calls == 0 {
		runner.mu.Unlock()
		t.Fatal("expected run_workflow task to invoke workflow runner")
	}
	if runner.lastReq.Workflow != "code_army" {
		runner.mu.Unlock()
		t.Fatalf("unexpected workflow name: %q", runner.lastReq.Workflow)
	}
	if runner.lastReq.StateKey != "project_alpha" {
		runner.mu.Unlock()
		t.Fatalf("unexpected workflow state key: %q", runner.lastReq.StateKey)
	}
	if runner.lastReq.SessionKey != "chat_id:oc_chat|thread:omt_alpha" {
		runner.mu.Unlock()
		t.Fatalf("unexpected workflow session key: %q", runner.lastReq.SessionKey)
	}
	if runner.lastReq.ReasoningEffort != "high" {
		runner.mu.Unlock()
		t.Fatalf("unexpected workflow reasoning effort: %q", runner.lastReq.ReasoningEffort)
	}
	if runner.lastReq.Personality != "pragmatic" {
		runner.mu.Unlock()
		t.Fatalf("unexpected workflow personality: %q", runner.lastReq.Personality)
	}
	if runner.lastReq.Scene != "chat" {
		runner.mu.Unlock()
		t.Fatalf("unexpected workflow scene: %q", runner.lastReq.Scene)
	}
	if got := runner.lastReq.Env["ALICE_MCP_SESSION_KEY"]; got != "chat_id:oc_chat|thread:omt_alpha" {
		runner.mu.Unlock()
		t.Fatalf("unexpected workflow session key env: %q", got)
	}
	runner.mu.Unlock()

	sender.mu.Lock()
	if sender.sendTextCalls == 0 {
		sender.mu.Unlock()
		t.Fatal("expected run_workflow task to send text")
	}
	if sender.lastText == "" {
		sender.mu.Unlock()
		t.Fatal("expected non-empty run_workflow dispatch text")
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

func TestEngine_RunUserTask_RunWorkflow_WorkSceneUsesCard(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 2, 3, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title:    "issue8 reconcile",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_chat"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 1},
		Action: Action{
			Type:            ActionTypeRunWorkflow,
			Prompt:          "请推进代码军队流程",
			Workflow:        "code_army",
			SessionKey:      "chat_id:oc_chat|scene:work|thread:omt_alpha",
			ReasoningEffort: "high",
		},
	})
	if err != nil {
		t.Fatalf("create run_workflow task failed: %v", err)
	}

	sender := &senderStub{}
	runner := &workflowRunnerStub{
		result: WorkflowRunResult{Message: "workflow 已完成"},
	}
	engine := NewEngine(store, sender)
	engine.SetWorkflowRunner(runner)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(2 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	runner.mu.Lock()
	if runner.lastReq.Scene != "work" {
		runner.mu.Unlock()
		t.Fatalf("unexpected workflow scene: %q", runner.lastReq.Scene)
	}
	runner.mu.Unlock()

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.sendCardCalls != 1 {
		t.Fatalf("expected run_workflow task to send one card, got %d", sender.sendCardCalls)
	}
	if sender.sendTextCalls != 0 {
		t.Fatalf("expected run_workflow task to avoid text send in work scene, got %d", sender.sendTextCalls)
	}
	if !strings.Contains(sender.lastCard, "workflow 已完成") {
		t.Fatalf("unexpected card content: %q", sender.lastCard)
	}
	if got := cardTitleFromJSON(t, sender.lastCard); got != "issue8 reconcile" {
		t.Fatalf("unexpected card title: %q", got)
	}
	if sender.lastReceiveType != "chat_id" || sender.lastReceiveID != "oc_chat" {
		t.Fatalf("unexpected route: %s %s", sender.lastReceiveType, sender.lastReceiveID)
	}

	stored, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if stored.LastResult == "" {
		t.Fatalf("expected last result to be recorded, task=%+v", stored)
	}
}

func TestEngine_RunUserTask_RunWorkflow_NeedsHumanPausesTaskAndWarns(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 2, 3, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title:    "issue8 reconcile",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_chat"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 1},
		Action: Action{
			Type:       ActionTypeRunWorkflow,
			Prompt:     "请推进代码军队流程",
			Workflow:   "code_army",
			SessionKey: "chat_id:oc_chat|scene:work|thread:omt_alpha",
		},
	})
	if err != nil {
		t.Fatalf("create run_workflow task failed: %v", err)
	}

	sender := &senderStub{}
	runner := &workflowRunnerStub{
		result: WorkflowRunResult{
			Commands: []WorkflowCommand{
				{Text: "/alice needs-human waiting for user confirmation"},
			},
		},
	}
	engine := NewEngine(store, sender)
	engine.SetWorkflowRunner(runner)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(2 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.sendCardCalls != 1 {
		t.Fatalf("expected warning card, got %d cards", sender.sendCardCalls)
	}
	if sender.sendTextCalls != 0 {
		t.Fatalf("expected no text fallback, got %d texts", sender.sendTextCalls)
	}
	if !strings.Contains(sender.lastCard, "需要人工介入") {
		t.Fatalf("unexpected warning card content: %q", sender.lastCard)
	}
	if !strings.Contains(sender.lastCard, "waiting for user confirmation") {
		t.Fatalf("warning card missing reason: %q", sender.lastCard)
	}

	stored, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if stored.Status != TaskStatusPaused {
		t.Fatalf("expected paused task, got %s", stored.Status)
	}
	if !stored.NextRunAt.IsZero() {
		t.Fatalf("expected cleared next_run_at, got %s", stored.NextRunAt.Format(time.RFC3339))
	}
	if stored.LastResult != "needs_human: waiting for user confirmation" {
		t.Fatalf("unexpected last result: %q", stored.LastResult)
	}
}

func TestEngine_RunUserTask_RunWorkflow_SkipsAutomationTimeout(t *testing.T) {
	sender := &senderStub{}
	runner := &workflowRunnerStub{
		result: WorkflowRunResult{Message: "workflow finished"},
	}
	engine := NewEngine(nil, sender)
	engine.SetWorkflowRunner(runner)
	engine.SetUserTaskTimeout(2 * time.Minute)
	start := time.Now()

	engine.runUserTask(context.Background(), Task{
		Scope:   Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:   Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{UserID: "ou_actor"},
		Action: Action{
			Type:     ActionTypeRunWorkflow,
			Prompt:   "推进代码军队流程",
			Workflow: "code_army",
			StateKey: "project_alpha",
		},
	})

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.calls != 1 {
		t.Fatalf("expected workflow runner to be called once, got %d", runner.calls)
	}
	if !runner.deadlineSet {
		t.Fatal("expected workflow runner context to carry watchdog deadline")
	}
	remaining := runner.deadline.Sub(start)
	if remaining < defaultWorkflowTaskTimeout-time.Minute || remaining > defaultWorkflowTaskTimeout+time.Minute {
		t.Fatalf("unexpected workflow timeout window: %s", remaining)
	}
}

func TestEngine_RunUserTask_CallsCompletionHook(t *testing.T) {
	sender := &senderStub{}
	engine := NewEngine(nil, sender)

	var (
		mu       sync.Mutex
		calls    int
		gotTask  Task
		gotError error
	)
	engine.SetUserTaskCompletionHook(func(task Task, err error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		gotTask = task
		gotError = err
	})

	engine.runUserTask(context.Background(), Task{
		ID:      "task_done",
		Scope:   Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:   Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{UserID: "ou_actor"},
		Action: Action{
			Type:     ActionTypeSendText,
			Text:     "hello",
			StateKey: " campaign_dispatch:camp_demo:executor:T001:x1 ",
		},
	})

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected completion hook to be called once, got %d", calls)
	}
	if gotTask.ID != "task_done" {
		t.Fatalf("unexpected hook task id: %q", gotTask.ID)
	}
	if gotTask.Action.StateKey != "campaign_dispatch:camp_demo:executor:T001:x1" {
		t.Fatalf("unexpected normalized state key: %q", gotTask.Action.StateKey)
	}
	if gotError != nil {
		t.Fatalf("expected nil hook error, got %v", gotError)
	}
}
