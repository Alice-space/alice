package automation

import (
	"context"
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
	lastReceiveType string
	lastReceiveID   string
	lastText        string
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
