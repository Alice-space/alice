package automation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	llm "github.com/Alice-space/alice/internal/llm"
)

func TestGoalStatus_IsTerminal(t *testing.T) {
	if GoalStatusActive.IsTerminal() {
		t.Fatal("active should not be terminal")
	}
	if GoalStatusPaused.IsTerminal() {
		t.Fatal("paused should not be terminal")
	}
	if !GoalStatusComplete.IsTerminal() {
		t.Fatal("complete should be terminal")
	}
	if !GoalStatusTimeout.IsTerminal() {
		t.Fatal("timeout should be terminal")
	}
}

func TestNormalizeGoal_DefaultsStatus(t *testing.T) {
	goal := NormalizeGoal(GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Scope:     Scope{Kind: ScopeKindChat, ID: "chat1"},
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if goal.Status != GoalStatusActive {
		t.Fatalf("expected active status, got %s", goal.Status)
	}
}

func TestNormalizeGoal_TrimsFields(t *testing.T) {
	goal := NormalizeGoal(GoalTask{
		ID:         "  goal_1  ",
		Objective:  "  do stuff  ",
		ThreadID:   "  thread_1  ",
		SessionKey: "  sk1  ",
		Scope:      Scope{Kind: "  chat  ", ID: "  chat1  "},
		Route:      Route{ReceiveIDType: "  chat_id  ", ReceiveID: "  chat1  "},
		Creator:    Actor{UserID: "  u1  ", OpenID: "  o1  ", Name: "  test  "},
	})
	if goal.ID != "goal_1" {
		t.Fatalf("expected trimmed id, got %q", goal.ID)
	}
	if goal.Objective != "do stuff" {
		t.Fatalf("expected trimmed objective, got %q", goal.Objective)
	}
	if goal.ThreadID != "thread_1" {
		t.Fatalf("expected trimmed thread_id, got %q", goal.ThreadID)
	}
	if goal.SessionKey != "sk1" {
		t.Fatalf("expected trimmed session_key, got %q", goal.SessionKey)
	}
	if goal.Scope.ID != "chat1" {
		t.Fatalf("expected trimmed scope id, got %q", goal.Scope.ID)
	}
}

func TestValidateGoal_RequiresID(t *testing.T) {
	goal := GoalTask{
		Objective: "test",
		Scope:     Scope{Kind: ScopeKindChat, ID: "c1"},
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "c1"},
		Creator:   Actor{UserID: "u1"},
	}
	if err := ValidateGoal(goal); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestValidateGoal_RequiresObjective(t *testing.T) {
	goal := GoalTask{
		ID:      "goal_1",
		Scope:   Scope{Kind: ScopeKindChat, ID: "c1"},
		Route:   Route{ReceiveIDType: "chat_id", ReceiveID: "c1"},
		Creator: Actor{UserID: "u1"},
	}
	if err := ValidateGoal(goal); err == nil {
		t.Fatal("expected error for empty objective")
	}
}

func TestValidateGoal_RequiresScope(t *testing.T) {
	goal := GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "c1"},
		Creator:   Actor{UserID: "u1"},
	}
	if err := ValidateGoal(goal); err == nil {
		t.Fatal("expected error for empty scope")
	}
}

func TestValidateGoal_RejectsInvalidStatus(t *testing.T) {
	goal := GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Status:    "invalid_status",
		Scope:     Scope{Kind: ScopeKindChat, ID: "c1"},
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "c1"},
		Creator:   Actor{UserID: "u1"},
	}
	if err := ValidateGoal(goal); err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestValidateGoal_AcceptsValidStatuses(t *testing.T) {
	for _, status := range []GoalStatus{GoalStatusActive, GoalStatusPaused, GoalStatusComplete, GoalStatusTimeout} {
		goal := GoalTask{
			ID:        "goal_1",
			Objective: "test",
			Status:    status,
			Scope:     Scope{Kind: ScopeKindChat, ID: "c1"},
			Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "c1"},
			Creator:   Actor{UserID: "u1"},
		}
		if err := ValidateGoal(goal); err != nil {
			t.Fatalf("expected valid for status %s, got: %v", status, err)
		}
	}
}

func TestStoreGoal_ReplaceAndGet(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	goal := GoalTask{
		ID:        "goal_test1",
		Objective: "finish project A",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	}
	created, err := store.ReplaceGoal(goal)
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}
	if created.ID != "goal_test1" {
		t.Fatalf("expected id goal_test1, got %s", created.ID)
	}
	if created.Status != GoalStatusActive {
		t.Fatalf("expected active status, got %s", created.Status)
	}

	retrieved, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if retrieved.Objective != "finish project A" {
		t.Fatalf("expected 'finish project A', got %s", retrieved.Objective)
	}
}

func TestStoreGoal_GetGoal_NotFound(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	_, err := store.GetGoal(Scope{Kind: ScopeKindChat, ID: "nonexistent"})
	if !errors.Is(err, ErrGoalNotFound) {
		t.Fatalf("expected ErrGoalNotFound, got %v", err)
	}
}

func TestStoreGoal_ReplaceGoal_FailsOnActiveGoalExists(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "first goal",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("first ReplaceGoal: %v", err)
	}
	_, err = store.ReplaceGoal(GoalTask{
		ID:        "goal_2",
		Objective: "second goal",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err == nil {
		t.Fatal("expected error when active goal exists")
	}
}

func TestStoreGoal_ReplaceGoal_SucceedsWhenPreviousCompleted(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "done goal",
		Status:    GoalStatusComplete,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("first ReplaceGoal: %v", err)
	}
	created, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_2",
		Objective: "new goal",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("second ReplaceGoal: %v", err)
	}
	if created.Objective != "new goal" {
		t.Fatalf("expected 'new goal', got %s", created.Objective)
	}
}

func TestStoreGoal_PatchGoal_UpdatesStatus(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}
	updated, err := store.PatchGoal(scope, func(goal *GoalTask) error {
		goal.Status = GoalStatusPaused
		return nil
	})
	if err != nil {
		t.Fatalf("PatchGoal: %v", err)
	}
	if updated.Status != GoalStatusPaused {
		t.Fatalf("expected paused status, got %s", updated.Status)
	}
	if updated.Revision == 0 {
		t.Fatal("expected revision incremented")
	}
}

func TestStoreGoal_PatchGoal_UpdatesThreadID(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}
	updated, err := store.PatchGoal(scope, func(goal *GoalTask) error {
		goal.ThreadID = "thread_abc123"
		return nil
	})
	if err != nil {
		t.Fatalf("PatchGoal: %v", err)
	}
	if updated.ThreadID != "thread_abc123" {
		t.Fatalf("expected thread_abc123, got %s", updated.ThreadID)
	}
}

func TestStoreGoal_PatchGoal_NotFound(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	_, err := store.PatchGoal(Scope{Kind: ScopeKindChat, ID: "nonexistent"}, func(goal *GoalTask) error {
		return nil
	})
	if !errors.Is(err, ErrGoalNotFound) {
		t.Fatalf("expected ErrGoalNotFound, got %v", err)
	}
}

func TestStoreGoal_DeleteGoal(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}
	if err := store.DeleteGoal(scope); err != nil {
		t.Fatalf("DeleteGoal: %v", err)
	}
	_, err = store.GetGoal(scope)
	if !errors.Is(err, ErrGoalNotFound) {
		t.Fatalf("expected ErrGoalNotFound after delete, got %v", err)
	}
}

func TestStoreGoal_DeleteGoal_NotFound(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	err := store.DeleteGoal(Scope{Kind: ScopeKindChat, ID: "nonexistent"})
	if !errors.Is(err, ErrGoalNotFound) {
		t.Fatalf("expected ErrGoalNotFound, got %v", err)
	}
}

func TestFormatDurationHMS(t *testing.T) {
	if s := formatDurationHMS(0); s != "0s" {
		t.Fatalf("expected 0s, got %s", s)
	}
	if s := formatDurationHMS(30 * time.Second); s != "30s" {
		t.Fatalf("expected 30s, got %s", s)
	}
	if s := formatDurationHMS(5 * time.Minute); s != "5m0s" {
		t.Fatalf("expected 5m0s, got %s", s)
	}
	if s := formatDurationHMS(2*time.Hour + 30*time.Minute); s != "2h30m" {
		t.Fatalf("expected 2h30m, got %s", s)
	}
	if s := formatDurationHMS(-5 * time.Second); s != "0s" {
		t.Fatalf("expected 0s for negative, got %s", s)
	}
}

func TestEngine_ExecuteGoal_SessionBusySkips(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{result: llm.RunResult{Reply: "done"}}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	gate := &sessionGateStub{busy: true}
	engine.SetSessionActivityChecker(gate)

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	runner.mu.Lock()
	if runner.calls > 0 {
		t.Fatal("expected no LLM calls when session busy")
	}
	runner.mu.Unlock()
}

func TestEngine_ExecuteGoal_MarksCompleteOnGoalDone(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{
		result: llm.RunResult{Reply: "done", GoalDone: true, NextThreadID: "thread_1"},
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected complete status, got %s", goal.Status)
	}
}

func TestEngine_ExecuteGoal_MarksTimeout(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base.Add(168 * time.Hour) }
	engine.SetLLMRunner(&llmRunnerStub{result: llm.RunResult{Reply: "ok"}})

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "test",
		Status:     GoalStatusActive,
		DeadlineAt: base.Add(1 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	engine.ExecuteGoal(t.Context(), scope)

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusTimeout {
		t.Fatalf("expected timeout status, got %s", goal.Status)
	}
}

func TestEngine_ExecuteGoal_SkipsPausedGoal(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }
	engine.SetLLMRunner(&llmRunnerStub{result: llm.RunResult{Reply: "ok"}})

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Status:    GoalStatusPaused,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}
}

func TestEngine_ExecuteGoal_PersistsThreadID(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{
		result: llm.RunResult{Reply: "ok", NextThreadID: "new_thread_xyz", GoalDone: true},
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "test",
		Status:    GoalStatusActive,
		ThreadID:  "old_thread",
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.ThreadID != "new_thread_xyz" {
		t.Fatalf("expected thread new_thread_xyz, got %s", goal.ThreadID)
	}
}

func TestEngine_ExecuteGoal_FirstRunUsesStartTemplate(t *testing.T) {
	SetGoalTemplates("START|{{.Objective}}", "CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{result: llm.RunResult{Reply: "ok", GoalDone: true}}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "test objective",
		Status:     GoalStatusActive,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	runner.mu.Lock()
	prompt := runner.lastReq.UserText
	runner.mu.Unlock()
	if !contains(prompt, "START|test objective") {
		t.Fatalf("expected start template, got: %s", prompt)
	}
}

func TestEngine_ExecuteGoal_SecondRunUsesContinueTemplate(t *testing.T) {
	SetGoalTemplates("START|{{.Objective}}", "CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{
		result: llm.RunResult{Reply: "ok", NextThreadID: "thread_existing", GoalDone: true},
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "test objective",
		Status:     GoalStatusActive,
		ThreadID:   "thread_existing",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	runner.mu.Lock()
	prompt := runner.lastReq.UserText
	runner.mu.Unlock()
	if !contains(prompt, "CONT|test objective") {
		t.Fatalf("expected continue template, got: %s", prompt)
	}
}

func TestEngine_ExecuteGoal_EventDrivenContinuation(t *testing.T) {
	SetGoalTemplates("START|{{.Objective}}", "CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{
		results: []llm.RunResult{
			{Reply: "iteration 1", NextThreadID: "thread_1"},
			{Reply: "iteration 2", NextThreadID: "thread_2"},
			{Reply: "done", GoalDone: true},
		},
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "event driven test",
		Status:     GoalStatusActive,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 3 {
		t.Fatalf("expected 3 LLM calls (event-driven loop), got %d", calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected complete status, got %s", goal.Status)
	}
	if goal.ThreadID != "thread_2" {
		t.Fatalf("expected thread_2, got %s", goal.ThreadID)
	}
}

func TestEngine_ExecuteGoal_ContinuePromptAfterFirstRun(t *testing.T) {
	SetGoalTemplates("START|{{.Objective}}", "CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{
		results: []llm.RunResult{
			{Reply: "step 1", NextThreadID: "t1"},
			{Reply: "step 2", NextThreadID: "t2", GoalDone: true},
		},
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "continue test",
		Status:     GoalStatusActive,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	runner.mu.Lock()
	calls := runner.calls
	step1Req := runner.lastReq
	runner.mu.Unlock()
	_ = step1Req

	if calls != 2 {
		t.Fatalf("expected 2 LLM calls (continue after first), got %d", calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected complete status, got %s", goal.Status)
	}
	if goal.ThreadID != "t2" {
		t.Fatalf("expected t2, got %s", goal.ThreadID)
	}
}

func TestEngine_ExecuteGoal_InterruptedByUserMessage(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	gate := &sessionGateStub{}
	engine.SetSessionActivityChecker(gate)

	runner := &interruptibleRunnerStub{}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "interrupt test",
		Status:     GoalStatusActive,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- engine.ExecuteGoal(context.Background(), scope)
	}()

	time.Sleep(100 * time.Millisecond)
	gate.mu.Lock()
	if gate.cancel != nil {
		gate.cancel(context.Canceled)
	}
	gate.mu.Unlock()

	err = <-done
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusActive {
		t.Fatalf("expected goal to remain active after interrupt, got %s", goal.Status)
	}
}

func TestEngine_ExecuteGoal_SessionBusyRetriedByTick(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{
		result: llm.RunResult{Reply: "ok", GoalDone: true},
	}
	engine.SetLLMRunner(runner)

	gate := &sessionGateStub{}
	engine.SetSessionActivityChecker(gate)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "tick retry test",
		Status:     GoalStatusActive,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	gate.busy = true
	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal busy: %v", err)
	}
	runner.mu.Lock()
	if runner.calls > 0 {
		t.Fatal("expected no LLM calls when session busy")
	}
	runner.mu.Unlock()

	gate.busy = false
	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal free: %v", err)
	}

	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected 1 LLM call after session freed, got %d", calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected complete, got %s", goal.Status)
	}
}

func TestEngine_ExecuteGoal_RunningFlagPreventsDuplicateExecution(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	iterStarted := make(chan struct{}, 2)
	iterUnblock := make(chan struct{})
	runner := &blockingGoalRunner{
		results: []llm.RunResult{
			{Reply: "step 1", NextThreadID: "t1"},
			{Reply: "step 2", NextThreadID: "t2", GoalDone: true},
		},
		started: iterStarted,
		unblock: iterUnblock,
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "running flag test",
		Status:     GoalStatusActive,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	g1, _ := store.GetGoal(scope)
	if g1.Running {
		t.Fatal("expected Running=false before execution")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = engine.ExecuteGoal(context.Background(), scope)
	}()

	<-iterStarted
	g2, _ := store.GetGoal(scope)
	if !g2.Running {
		t.Fatal("expected Running=true during execution")
	}

	engine.runGoals(context.Background())

	close(iterUnblock)
	<-done

	g3, _ := store.GetGoal(scope)
	if g3.Running {
		t.Fatal("expected Running=false after completion")
	}
	if g3.Status != GoalStatusComplete {
		t.Fatalf("expected complete, got %s", g3.Status)
	}

	if runner.calls != 2 {
		t.Fatalf("expected exactly 2 LLM calls, got %d", runner.calls)
	}
}

func TestEngine_ExecuteGoal_PersistsThreadIDOnInterruption(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	gate := &sessionGateStub{}
	engine.SetSessionActivityChecker(gate)

	iterStarted := make(chan struct{}, 2)
	iterUnblock := make(chan struct{})
	runner := &blockingGoalRunner{
		results: []llm.RunResult{
			{Reply: "working...", NextThreadID: "ses_interrupted"},
			{Reply: "resumed! done", NextThreadID: "ses_interrupted", GoalDone: true},
		},
		started: iterStarted,
		unblock: iterUnblock,
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_1",
		Objective:  "interrupt persist test",
		Status:     GoalStatusActive,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = engine.ExecuteGoal(context.Background(), scope)
	}()

	<-iterStarted

	gate.mu.Lock()
	if gate.cancel != nil {
		gate.cancel(context.Canceled)
	}
	gate.mu.Unlock()

	close(iterUnblock)
	<-done

	g, _ := store.GetGoal(scope)
	if g.ThreadID != "ses_interrupted" {
		t.Fatalf("expected ThreadID=ses_interrupted after interruption, got %q", g.ThreadID)
	}
	if g.Status != GoalStatusActive {
		t.Fatalf("expected goal to remain active after interruption, got %s", g.Status)
	}

	engine.SetSessionActivityChecker(&sessionGateStub{})
	engine.SetLLMRunner(&llmRunnerStub{
		result: llm.RunResult{Reply: "resumed", NextThreadID: "ses_resumed", GoalDone: true},
	})
	err = engine.ExecuteGoal(context.Background(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal retry: %v", err)
	}

	g2, _ := store.GetGoal(scope)
	if g2.Status != GoalStatusComplete {
		t.Fatalf("expected complete, got %s", g2.Status)
	}
	if g2.ThreadID != "ses_resumed" {
		t.Fatalf("expected ThreadID=ses_resumed, got %q", g2.ThreadID)
	}
}

func TestStore_ResetRunningGoals(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	scope := Scope{Kind: ScopeKindChat, ID: "chat1"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_1",
		Objective: "reset test",
		Status:    GoalStatusActive,
		Scope:     scope,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "chat1"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	_, _ = store.PatchGoal(scope, func(g *GoalTask) error {
		g.Running = true
		return nil
	})

	g, _ := store.GetGoal(scope)
	if !g.Running {
		t.Fatal("expected Running=true before reset")
	}

	if err := store.ResetRunningGoals(); err != nil {
		t.Fatalf("ResetRunningGoals: %v", err)
	}

	g, _ = store.GetGoal(scope)
	if g.Running {
		t.Fatal("expected Running=false after reset")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
