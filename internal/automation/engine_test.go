package automation

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/llm"
)

type senderStub struct {
	mu              sync.Mutex
	sendTextCalls   int
	sendCardCalls   int
	lastReceiveType string
	lastReceiveID   string
	lastText        string
	lastCard        string
}

func (s *senderStub) SendText(_ context.Context, receiveIDType, receiveID, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendTextCalls++
	s.lastReceiveType = receiveIDType
	s.lastReceiveID = receiveID
	s.lastText = text
	return nil
}

func (s *senderStub) SendCard(_ context.Context, receiveIDType, receiveID, cardContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCardCalls++
	s.lastReceiveType = receiveIDType
	s.lastReceiveID = receiveID
	s.lastCard = cardContent
	return nil
}

func cardTitleFromJSON(t *testing.T, raw string) string {
	t.Helper()
	var card struct {
		Header struct {
			Title struct {
				Content string `json:"content"`
			} `json:"title"`
		} `json:"header"`
	}
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		t.Fatalf("unmarshal card failed: %v, raw=%q", err, raw)
	}
	return card.Header.Title.Content
}

type deadlineSenderStub struct {
	mu          sync.Mutex
	deadlineSet bool
	deadline    time.Time
}

func (s *deadlineSenderStub) SendText(ctx context.Context, _, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlineSet = true
		s.deadline = deadline
	}
	return nil
}

func (s *deadlineSenderStub) SendCard(ctx context.Context, _, _, _ string) error {
	return s.SendText(ctx, "", "", "")
}

type llmRunnerStub struct {
	mu      sync.Mutex
	calls   int
	lastReq llm.RunRequest
	result  llm.RunResult
	err     error
}

func (s *llmRunnerStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastReq = req
	return s.result, s.err
}

type workflowRunnerStub struct {
	mu          sync.Mutex
	calls       int
	lastReq     WorkflowRunRequest
	result      WorkflowRunResult
	err         error
	deadlineSet bool
	deadline    time.Time
}

func (s *workflowRunnerStub) Run(ctx context.Context, req WorkflowRunRequest) (WorkflowRunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastReq = req
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlineSet = true
		s.deadline = deadline
	}
	return s.result, s.err
}

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

func TestEngine_RunUserTask(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 1},
		Action:   Action{Type: ActionTypeSendText, Text: "hello", MentionUserIDs: []string{"ou_actor"}},
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(2 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	sender.mu.Lock()
	if sender.sendTextCalls == 0 {
		sender.mu.Unlock()
		t.Fatal("expected user task to send text")
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

func TestEngine_RunUserTask_RunLLM(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 1, 2, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 1},
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
		result: llm.RunResult{Reply: "现在是 2026-02-23T10:01:02Z"},
	}
	engine := NewEngine(store, sender)
	engine.SetLLMRunner(runner)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(2 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	engine.Run(ctx)

	runner.mu.Lock()
	if runner.calls == 0 {
		runner.mu.Unlock()
		t.Fatal("expected run_llm task to invoke llm runner")
	}
	wantPrompt := "请回复当前时间 " + base.Add(2*time.Second).Local().Format(time.RFC3339)
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
	if got := runner.lastReq.Env["ALICE_MCP_RECEIVE_ID"]; got != "ou_actor" {
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
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 1},
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
		result: llm.RunResult{Reply: "work 已完成"},
	}
	engine := NewEngine(store, sender)
	engine.SetLLMRunner(runner)
	engine.tick = 10 * time.Millisecond
	engine.now = func() time.Time { return base.Add(2 * time.Second) }

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
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
	if got := runner.lastReq.Env["ALICE_MCP_SESSION_KEY"]; got != "chat_id:oc_chat|scene:work|thread:omt_alpha" {
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

func TestEngine_RunUserTask_UsesConfiguredTimeout(t *testing.T) {
	sender := &deadlineSenderStub{}
	engine := NewEngine(nil, sender)
	engine.SetUserTaskTimeout(2 * time.Minute)

	start := time.Now()
	engine.runUserTask(context.Background(), Task{
		Scope:   Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:   Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{UserID: "ou_actor"},
		Action:  Action{Type: ActionTypeSendText, Text: "hello"},
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
	if runner.deadlineSet {
		t.Fatalf("expected workflow runner context to skip automation deadline, got %s", runner.deadline)
	}
}

func TestEngine_SetUserTaskTimeout_NonPositiveFallsBackToDefault(t *testing.T) {
	engine := NewEngine(nil, nil)
	engine.SetUserTaskTimeout(0)
	if got := engine.userTaskTimeoutDuration(); got != defaultUserTaskTimeout {
		t.Fatalf("unexpected default timeout: %s", got)
	}
}

func TestRenderActionTemplate_InvalidTemplateReturnsError(t *testing.T) {
	_, err := renderActionTemplate("{{ if }}", time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected renderActionTemplate to return error for invalid template")
	}
	if !strings.Contains(err.Error(), "render action template failed") {
		t.Fatalf("unexpected template error: %v", err)
	}
}

func TestRenderActionTemplate_EmptyInputReturnsEmpty(t *testing.T) {
	got, err := renderActionTemplate("   ", time.Time{})
	if err != nil {
		t.Fatalf("expected nil error for empty template, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty rendered text, got %q", got)
	}
}
