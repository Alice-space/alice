package automation

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
)

type senderStub struct {
	mu                sync.Mutex
	sendTextCalls     int
	sendCardCalls     int
	urgentAppCalls    int
	lastReceiveType   string
	lastReceiveID     string
	lastText          string
	texts             []string
	lastCard          string
	urgentMessageID   string
	urgentUserIDType  string
	urgentUserIDs     []string
	sendTextErr       error
	sendCardErr       error
	sendTextMessageID string
	sendCardMessageID string
	urgentAppErr      error
}

func (s *senderStub) SendText(_ context.Context, receiveIDType, receiveID, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendTextCalls++
	s.lastReceiveType = receiveIDType
	s.lastReceiveID = receiveID
	s.lastText = text
	s.texts = append(s.texts, text)
	return s.sendTextErr
}

func (s *senderStub) SendCard(_ context.Context, receiveIDType, receiveID, cardContent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendCardCalls++
	s.lastReceiveType = receiveIDType
	s.lastReceiveID = receiveID
	s.lastCard = cardContent
	return s.sendCardErr
}

func (s *senderStub) SendTextMessage(ctx context.Context, receiveIDType, receiveID, text string) (string, error) {
	if err := s.SendText(ctx, receiveIDType, receiveID, text); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.sendTextMessageID) != "" {
		return s.sendTextMessageID, nil
	}
	return "om_text", nil
}

func (s *senderStub) SendCardMessage(ctx context.Context, receiveIDType, receiveID, cardContent string) (string, error) {
	if err := s.SendCard(ctx, receiveIDType, receiveID, cardContent); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.sendCardMessageID) != "" {
		return s.sendCardMessageID, nil
	}
	return "om_card", nil
}

func (s *senderStub) UrgentApp(_ context.Context, messageID, userIDType string, userIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urgentAppCalls++
	s.urgentMessageID = messageID
	s.urgentUserIDType = userIDType
	s.urgentUserIDs = append([]string(nil), userIDs...)
	return s.urgentAppErr
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

func TestTaskUrgentRecipient_PrefersOpenID(t *testing.T) {
	userIDType, userID, ok := taskUrgentRecipient(Actor{
		UserID: "u_actor",
		OpenID: "ou_actor",
	})
	if !ok {
		t.Fatal("expected urgent recipient to resolve")
	}
	if userIDType != "open_id" {
		t.Fatalf("expected open_id, got %q", userIDType)
	}
	if userID != "ou_actor" {
		t.Fatalf("unexpected urgent recipient id: %q", userID)
	}
}

type llmRunnerStub struct {
	mu       sync.Mutex
	calls    int
	lastReq  agentbridge.RunRequest
	progress []string
	result   agentbridge.RunResult
	err      error
}

func (s *llmRunnerStub) Run(_ context.Context, req agentbridge.RunRequest) (agentbridge.RunResult, error) {
	s.mu.Lock()
	s.calls++
	s.lastReq = req
	progress := append([]string(nil), s.progress...)
	result := s.result
	err := s.err
	s.mu.Unlock()
	for _, step := range progress {
		if req.OnProgress != nil {
			req.OnProgress(step)
		}
	}
	return result, err
}

type sessionCheckerStub struct {
	mu             sync.Mutex
	activeSessions map[string]bool
}

func (s *sessionCheckerStub) IsSessionActive(sessionKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s == nil || s.activeSessions == nil {
		return false
	}
	return s.activeSessions[sessionKey]
}

func TestRunUserTask_SkipsWhenSessionBusy(t *testing.T) {
	base := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{result: agentbridge.RunResult{Reply: "hello"}}
	engine.SetLLMRunner(runner)

	created, err := store.CreateTask(Task{
		Title:    "busy-session test",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_chat1"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat1"},
		Creator:  Actor{OpenID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeRunLLM, Prompt: "test"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	claimAt := base.Add(2 * time.Minute)
	store.now = func() time.Time { return claimAt }
	claimed, err := store.ClaimDueTasks(claimAt, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim failed: err=%v len=%d", err, len(claimed))
	}

	engine.SetSessionActivityChecker(&sessionCheckerStub{
		activeSessions: map[string]bool{"chat_id:oc_chat1": true},
	})

	engine.runUserTask(context.Background(), claimed[0])

	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected 0 LLM calls (session busy), got %d", calls)
	}

	task, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task after skip: %v", err)
	}
	if task.Running {
		t.Fatal("expected Running=false after skip")
	}
	if task.RunCount != 0 {
		t.Fatalf("expected RunCount=0 after unclaim, got %d", task.RunCount)
	}

	sender.mu.Lock()
	textCalls := sender.sendTextCalls
	sender.mu.Unlock()
	if textCalls != 0 {
		t.Fatalf("expected 0 send calls, got %d", textCalls)
	}
}

func TestRunUserTask_SkipsWhenSessionBusy_DecoratedKey(t *testing.T) {
	base := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }
	engine.SetLLMRunner(&llmRunnerStub{result: agentbridge.RunResult{Reply: "hello"}})

	created, err := store.CreateTask(Task{
		Title:    "decorated-key test",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_chat2"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat2"},
		Creator:  Actor{OpenID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeRunLLM, Prompt: "test"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	claimAt := base.Add(2 * time.Minute)
	store.now = func() time.Time { return claimAt }
	claimed, err := store.ClaimDueTasks(claimAt, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim failed: err=%v len=%d", err, len(claimed))
	}

	engine.SetSessionActivityChecker(&sessionCheckerStub{
		activeSessions: map[string]bool{"chat_id:oc_chat2": true},
	})

	engine.runUserTask(context.Background(), claimed[0])

	task, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task after skip: %v", err)
	}
	if task.Running {
		t.Fatal("expected Running=false after skip with decorated active key")
	}
	if task.RunCount != 0 {
		t.Fatalf("expected RunCount=0 after unclaim, got %d", task.RunCount)
	}
}

// TestRunUserTask_SkipLog_RateLimit verifies that the "session busy" log is
// suppressed for subsequent skips within the rate-limit window, and re-emitted
// once the window expires.
func TestRunUserTask_SkipLog_RateLimit(t *testing.T) {
	base := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	clock := base
	engine.now = func() time.Time { return clock }
	engine.SetLLMRunner(&llmRunnerStub{result: agentbridge.RunResult{Reply: "hello"}})

	checker := &sessionCheckerStub{activeSessions: map[string]bool{"chat_id:oc_rl": true}}
	engine.SetSessionActivityChecker(checker)

	created, err := store.CreateTask(Task{
		Title:    "rate-limit-test",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_rl"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_rl"},
		Creator:  Actor{OpenID: "ou_test"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeRunLLM, Prompt: "ping"},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Helper: re-claim the task at a given time and run it.
	claimAndRun := func(at time.Time) {
		clock = at
		store.now = func() time.Time { return at }
		claimed, err := store.ClaimDueTasks(at, 10)
		if err != nil || len(claimed) == 0 {
			t.Fatalf("claim at %v: err=%v len=%d", at, err, len(claimed))
		}
		engine.runUserTask(context.Background(), claimed[0])
	}

	// First skip at t1: lastSkipLog must be set to t1.
	t1 := base.Add(61 * time.Second)
	claimAndRun(t1)
	if last, ok := engine.lastSkipLog.Load(created.ID); !ok {
		t.Fatal("expected lastSkipLog to be set after first skip")
	} else if !last.(time.Time).Equal(t1) {
		t.Fatalf("expected lastSkipLog=%v, got %v", t1, last.(time.Time))
	}

	// Second skip at t2 (within 1-minute window): lastSkipLog must NOT advance.
	t2 := t1.Add(30 * time.Second)
	claimAndRun(t2)
	if last, ok := engine.lastSkipLog.Load(created.ID); !ok {
		t.Fatal("lastSkipLog was deleted unexpectedly")
	} else if !last.(time.Time).Equal(t1) {
		t.Fatalf("lastSkipLog must not advance within rate-limit window: want %v, got %v", t1, last.(time.Time))
	}

	// Third skip at t3 (outside 1-minute window): lastSkipLog must be updated to t3.
	t3 := t1.Add(61 * time.Second)
	claimAndRun(t3)
	if last, ok := engine.lastSkipLog.Load(created.ID); !ok {
		t.Fatal("lastSkipLog was deleted unexpectedly")
	} else if !last.(time.Time).Equal(t3) {
		t.Fatalf("lastSkipLog must update after rate-limit window: want %v, got %v", t3, last.(time.Time))
	}
}

type sessionGateStub struct {
	mu       sync.Mutex
	busy     bool
	cancel   context.CancelCauseFunc
	acquired int
	released int
}

func (s *sessionGateStub) IsSessionActive(_ string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

func (s *sessionGateStub) TryAcquireSession(_ string, cancel context.CancelCauseFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	s.cancel = cancel
	s.acquired++
	return true
}

func (s *sessionGateStub) ReleaseSession(_ string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		s.busy = false
		s.cancel = nil
		s.released++
	}
}

func TestRunUserTask_SessionGateAcquiresAndReleases(t *testing.T) {
	base := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	runner := &llmRunnerStub{result: agentbridge.RunResult{Reply: "ok"}}
	gate := &sessionGateStub{}

	engine := NewEngine(store, sender)
	engine.SetLLMRunner(runner)
	engine.SetSessionActivityChecker(gate)
	engine.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title:    "gate test",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_gate"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_gate"},
		Creator:  Actor{OpenID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeRunLLM, Prompt: "test"},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	claimed, err := store.ClaimDueTasks(base.Add(61*time.Second), 1)
	if err != nil || len(claimed) == 0 {
		t.Fatalf("claim: err=%v len=%d", err, len(claimed))
	}

	engine.runUserTask(context.Background(), claimed[0])

	gate.mu.Lock()
	acquired := gate.acquired
	released := gate.released
	gate.mu.Unlock()

	if acquired != 1 {
		t.Fatalf("expected 1 acquire, got %d", acquired)
	}
	if released != 1 {
		t.Fatalf("expected 1 release, got %d", released)
	}

	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 llm call, got %d", calls)
	}

	stored, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if stored.LastResult == "" {
		t.Fatal("expected last result recorded")
	}
}

func TestRunUserTask_SessionGateBusySkipsAndUnclaims(t *testing.T) {
	base := time.Date(2026, 4, 30, 11, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	runner := &llmRunnerStub{result: agentbridge.RunResult{Reply: "ok"}}
	gate := &sessionGateStub{busy: true}

	engine := NewEngine(store, sender)
	engine.SetLLMRunner(runner)
	engine.SetSessionActivityChecker(gate)
	engine.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title:    "busy gate test",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_busy"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_busy"},
		Creator:  Actor{OpenID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeRunLLM, Prompt: "test"},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	claimed, err := store.ClaimDueTasks(base.Add(61*time.Second), 1)
	if err != nil || len(claimed) == 0 {
		t.Fatalf("claim: err=%v len=%d", err, len(claimed))
	}
	if !claimed[0].Running {
		t.Fatal("expected Running=true after claim")
	}

	engine.runUserTask(context.Background(), claimed[0])

	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected 0 llm calls (busy gate), got %d", calls)
	}

	task, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Running {
		t.Fatal("expected Running=false after busy skip")
	}
}

func TestRunUserTask_SessionGateInterruptedUnclaims(t *testing.T) {
	base := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	gate := &sessionGateStub{}

	engine := NewEngine(store, sender)
	engine.SetSessionActivityChecker(gate)
	engine.now = func() time.Time { return base }

	runner := &interruptibleRunnerStub{}
	engine.SetLLMRunner(runner)

	created, err := store.CreateTask(Task{
		Title:    "interrupt test",
		Scope:    Scope{Kind: ScopeKindChat, ID: "oc_int"},
		Route:    Route{ReceiveIDType: "chat_id", ReceiveID: "oc_int"},
		Creator:  Actor{OpenID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeRunLLM, Prompt: "test"},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	claimed, err := store.ClaimDueTasks(base.Add(61*time.Second), 1)
	if err != nil || len(claimed) == 0 {
		t.Fatalf("claim: err=%v len=%d", err, len(claimed))
	}

	// Run the task in a goroutine, then simulate user interruption by calling
	// the stored cancel function.
	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.runUserTask(context.Background(), claimed[0])
	}()

	// Wait for gate to be acquired, then interrupt.
	time.Sleep(100 * time.Millisecond)
	gate.mu.Lock()
	if gate.cancel != nil {
		gate.cancel(context.Canceled)
	}
	gate.mu.Unlock()

	<-done

	task, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Running {
		t.Fatal("expected Running=false after interrupt")
	}
	if task.RunCount > 0 {
		t.Fatalf("expected RunCount=0 after unclaim on interrupt, got %d", task.RunCount)
	}
	if task.ConsecutiveFailures > 0 {
		t.Fatalf("expected no failures after interrupt, got %d", task.ConsecutiveFailures)
	}
}

// interruptibleRunnerStub blocks until its context is cancelled, then returns an error.
// This simulates an LLM runner that gets interrupted by a user message.
type interruptibleRunnerStub struct {
	called bool
}

func (r *interruptibleRunnerStub) Run(ctx context.Context, _ agentbridge.RunRequest) (agentbridge.RunResult, error) {
	r.called = true
	<-ctx.Done()
	return agentbridge.RunResult{}, ctx.Err()
}
