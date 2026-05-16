package automation

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
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
	for _, status := range []GoalStatus{GoalStatusActive, GoalStatusPaused, GoalStatusComplete, GoalStatusTimeout, GoalStatusWaitingForSession} {
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
		ThreadID:  "thread_0",
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
		ThreadID:   "thread_0",
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

func TestEngine_ExecuteGoal_SkipsWhenNoThreadID(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

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
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected 0 LLM calls when ThreadID is empty, got %d", calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusActive {
		t.Fatalf("expected goal to remain active, got %s", goal.Status)
	}
}

func TestEngine_ExecuteGoal_UsesContinueTemplate(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

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
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

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
		ThreadID:   "thread_0",
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
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

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
		ThreadID:   "t0",
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
		ThreadID:   "thread_0",
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

	// Wait for ExecuteGoal to acquire the session gate, then cancel.
	for i := 0; i < 50; i++ {
		gate.mu.Lock()
		cancel := gate.cancel
		gate.mu.Unlock()
		if cancel != nil {
			cancel(context.Canceled)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

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
		ThreadID:   "thread_0",
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
		ThreadID:   "thread_0",
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
		ThreadID:   "thread_0",
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

func TestStoreGoal_ScopeIsolationBetweenWorkSessions(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))

	scope1 := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_seed_1"}
	scope2 := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_seed_2"}

	goal1, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_scope_1",
		Objective: "first session goal",
		Status:    GoalStatusActive,
		Scope:     scope1,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal scope1: %v", err)
	}

	goal2, err := store.ReplaceGoal(GoalTask{
		ID:        "goal_scope_2",
		Objective: "second session goal",
		Status:    GoalStatusActive,
		Scope:     scope2,
		Route:     Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:   Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal scope2: %v", err)
	}

	if goal1.ID == goal2.ID {
		t.Fatalf("expected different goal IDs, got %q and %q", goal1.ID, goal2.ID)
	}

	retrieved1, err := store.GetGoal(scope1)
	if err != nil {
		t.Fatalf("GetGoal scope1: %v", err)
	}
	if retrieved1.ID != goal1.ID {
		t.Fatalf("expected goal1 (%s) for scope1, got %s", goal1.ID, retrieved1.ID)
	}
	if retrieved1.Objective != "first session goal" {
		t.Fatalf("expected 'first session goal', got %q", retrieved1.Objective)
	}

	retrieved2, err := store.GetGoal(scope2)
	if err != nil {
		t.Fatalf("GetGoal scope2: %v", err)
	}
	if retrieved2.ID != goal2.ID {
		t.Fatalf("expected goal2 (%s) for scope2, got %s", goal2.ID, retrieved2.ID)
	}
	if retrieved2.Objective != "second session goal" {
		t.Fatalf("expected 'second session goal', got %q", retrieved2.Objective)
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

func TestEngine_ExecuteGoal_UsesGoalRunHelper(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	helper := &goalRunHelperStub{
		result: llm.RunResult{Reply: "done via helper", NextThreadID: "ths", GoalDone: true},
	}
	engine.SetGoalRunHelper(helper)
	engine.SetLLMRunner(&runLLMPanicStub{})

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_test_helper"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_helper_test",
		Objective:  "helper test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	if err := engine.ExecuteGoal(t.Context(), scope); err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	if helper.calls != 1 {
		t.Fatalf("expected 1 GoalRunHelper call, got %d", helper.calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected complete status, got %s", goal.Status)
	}
	if goal.ThreadID != "ths" {
		t.Fatalf("expected thread ths, got %s", goal.ThreadID)
	}
}

func TestEngine_ExecuteGoal_GoalRunHelper_MultipleIterations(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	helper := &goalRunHelperStub{
		results: []llm.RunResult{
			{Reply: "step 1", NextThreadID: "t1"},
			{Reply: "step 2", NextThreadID: "t2"},
			{Reply: "done", NextThreadID: "t3", GoalDone: true},
		},
	}
	engine.SetGoalRunHelper(helper)
	engine.SetLLMRunner(&runLLMPanicStub{})

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_test_multi"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_multi_test",
		Objective:  "multi iter test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	if err := engine.ExecuteGoal(t.Context(), scope); err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	if helper.calls != 3 {
		t.Fatalf("expected 3 GoalRunHelper calls, got %d", helper.calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected complete status, got %s", goal.Status)
	}
	if goal.ThreadID != "t3" {
		t.Fatalf("expected thread t3, got %s", goal.ThreadID)
	}
}

func TestRichTextCardContent_WrapsMarkdownInCardJSON(t *testing.T) {
	c := richTextCardContent("**hello** world")
	if !strings.Contains(c, `"schema":"2.0"`) {
		t.Fatalf("expected card schema, got %q", c)
	}
	if !strings.Contains(c, `"tag":"markdown"`) {
		t.Fatalf("expected markdown element tag, got %q", c)
	}
	if !strings.Contains(c, `**hello**`) {
		t.Fatalf("expected markdown content, got %q", c)
	}
}

func TestEngine_runGoals_SkipsCompletedGoals(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.SetLLMRunner(&runLLMPanicStub{})
	engine.SetUserTaskTimeout(time.Second)

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_seed"}
	if _, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_completed",
		Objective:  "already done",
		Status:     GoalStatusComplete,
		DeadlineAt: time.Now().Add(time.Hour),
		ThreadID:   "thread_abc",
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "ou_user"},
		CreatedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("create completed goal failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	engine.runGoals(ctx)
	cancel()

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("get goal failed: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected completed goal to remain complete, got %q", goal.Status)
	}
}

type runLLMPanicStub struct{}

func (s *runLLMPanicStub) Run(_ context.Context, _ llm.RunRequest) (llm.RunResult, error) {
	panic("LLM should not be called for completed goals")
}

func TestEngine_ExecuteGoal_PausesOnFastLoop(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	sender := &senderStub{}
	engine := NewEngine(store, sender)
	runner := &fastLoopRunnerStub{runCount: 0}
	engine.SetLLMRunner(runner)
	engine.now = func() time.Time {
		return time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	}

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_seed"}
	if _, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_fast",
		Objective:  "loop test",
		Status:     GoalStatusActive,
		DeadlineAt: time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC),
		ThreadID:   "thread_fast",
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "ou_user"},
		CreatedAt:  time.Date(2026, 5, 5, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("create goal failed: %v", err)
	}

	if err := engine.ExecuteGoal(context.Background(), scope); err != nil {
		t.Fatalf("ExecuteGoal failed: %v", err)
	}
	if runner.runCount != 5 {
		t.Fatalf("expected 5 fast runs before pause, got %d", runner.runCount)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("get goal failed: %v", err)
	}
	if goal.Status != GoalStatusPaused {
		t.Fatalf("expected status paused, got %q", goal.Status)
	}
	if sender.lastText == "" || !strings.Contains(sender.lastText, "快速循环") {
		t.Fatalf("expected fast loop notification, got %q", sender.lastText)
	}
}

func TestEngine_ExecuteGoal_WakesAgentAfterPause(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	helper := &goalRunHelperStub{
		results: []llm.RunResult{
			{Reply: "working on step 1", NextThreadID: "wake_t1"},
			{Reply: "still working", NextThreadID: "wake_t2"},
			{Reply: "all done", NextThreadID: "wake_t3", GoalDone: true},
		},
	}
	engine.SetGoalRunHelper(helper)
	engine.SetLLMRunner(&runLLMPanicStub{})

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_test_wake"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_wake_test",
		Objective:  "wake up after pause",
		Status:     GoalStatusActive,
		ThreadID:   "thread_wake_0",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	if err := engine.ExecuteGoal(t.Context(), scope); err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	if helper.calls != 3 {
		t.Fatalf("expected 3 GoalRunHelper calls, got %d", helper.calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected complete status, got %s", goal.Status)
	}
	if goal.ThreadID != "wake_t3" {
		t.Fatalf("expected thread wake_t3, got %s", goal.ThreadID)
	}

	helper.mu.Lock()
	onProgress := helper.lastReq.OnProgress
	helper.mu.Unlock()
	if onProgress == nil {
		t.Fatal("expected OnProgress to be set on goal run call")
	}

	sender.mu.Lock()
	lastBeforeProgress := sender.lastText
	sender.mu.Unlock()
	if !strings.Contains(lastBeforeProgress, "目标已完成") {
		t.Fatalf("expected completion notification, got %q", lastBeforeProgress)
	}

	sender.mu.Lock()
	sendBefore := sender.sendTextCalls
	sender.mu.Unlock()
	onProgress("agent is making progress on the goal")
	sender.mu.Lock()
	sendAfter := sender.sendTextCalls
	sender.mu.Unlock()
	if sendAfter <= sendBefore {
		t.Fatal("expected OnProgress to trigger a sender call")
	}
}

func TestEngine_ExecuteGoal_ContinuePromptContainsObjective(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	helper := &goalRunHelperStub{
		result: llm.RunResult{Reply: "done", GoalDone: true},
	}
	engine.SetGoalRunHelper(helper)
	engine.SetLLMRunner(&runLLMPanicStub{})

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_test_obj"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_obj_test",
		Objective:  "learn Go testing thoroughly",
		Status:     GoalStatusActive,
		ThreadID:   "thread_obj_0",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	if err := engine.ExecuteGoal(t.Context(), scope); err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	helper.mu.Lock()
	userText := helper.lastReq.UserText
	helper.mu.Unlock()
	if !strings.Contains(userText, "learn Go testing thoroughly") {
		t.Fatalf("expected userText to contain objective, got: %s", userText)
	}
}

type fastLoopRunnerStub struct {
	runCount int
}

func (s *fastLoopRunnerStub) Run(_ context.Context, _ llm.RunRequest) (llm.RunResult, error) {
	s.runCount++
	return llm.RunResult{GoalDone: false}, nil
}

func TestEngine_ExecuteGoal_DifferentWorkThreadsIsolated(t *testing.T) {
	base := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	gate := &threadScopedSessionGate{active: make(map[string]context.CancelCauseFunc)}
	engine.SetSessionActivityChecker(gate)
	engine.SetLLMRunner(&interruptibleRunnerStub{})

	scopeA := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_AAA"}
	scopeB := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_BBB"}

	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_A",
		Objective:  "work goal A",
		Status:     GoalStatusActive,
		ThreadID:   "thread_A",
		SessionKey: "chat_id:oc_chat|work:om_AAA",
		Scope:      scopeA,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
		DeadlineAt: base.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceGoal goal_A: %v", err)
	}

	// Start goal A in a goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = engine.ExecuteGoal(t.Context(), scopeA)
	}()

	time.Sleep(50 * time.Millisecond)

	// Goal A should have acquired the gate for its work thread
	gate.mu.Lock()
	_, aBusy := gate.active["chat_id:oc_chat|work:om_AAA"]
	gate.mu.Unlock()
	if !aBusy {
		t.Fatal("expected goal A to acquire session gate for its work thread")
	}

	// Run the tick — goal B should NOT be dispatched because its thread differs.
	// (It would be dispatched if thread scoping failed.)
	engine.runGoals(t.Context())

	_, err = store.ReplaceGoal(GoalTask{
		ID:         "goal_B",
		Objective:  "work goal B",
		Status:     GoalStatusActive,
		ThreadID:   "thread_B",
		SessionKey: "chat_id:oc_chat|work:om_BBB",
		Scope:      scopeB,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
		DeadlineAt: base.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceGoal goal_B: %v", err)
	}

	engine.runGoals(t.Context())

	gB, _ := store.GetGoal(scopeB)
	if gB.Running {
		t.Fatal("expected goal B to remain not running (different work thread isolated)")
	}

	// Cancel goal A to clean up
	gate.mu.Lock()
	if cancel, ok := gate.active["chat_id:oc_chat|work:om_AAA"]; ok && cancel != nil {
		cancel(context.Canceled)
	}
	gate.mu.Unlock()
	<-done
}

func TestGoalThreadIsolation_DifferentWorkThreadsRunConcurrently(t *testing.T) {
	base := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	gate := &threadScopedSessionGate{active: make(map[string]context.CancelCauseFunc)}
	engine.SetSessionActivityChecker(gate)

	scopeA := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_indie_A"}

	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_indie_A",
		Objective:  "independent goal A",
		Status:     GoalStatusActive,
		ThreadID:   "thread_A",
		SessionKey: "chat_id:oc_chat|work:om_indie_A",
		Scope:      scopeA,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
		DeadlineAt: base.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceGoal A: %v", err)
	}

	runnerA := &blockingGoalRunner{
		results: []llm.RunResult{
			{Reply: "done A", NextThreadID: "tA", GoalDone: true},
		},
		started: make(chan struct{}, 1),
		unblock: make(chan struct{}),
	}
	engine.SetLLMRunner(runnerA)

	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		_ = engine.ExecuteGoal(t.Context(), scopeA)
	}()

	<-runnerA.started

	// While goal A is running, verify goal B can acquire its OWN session
	if !gate.TryAcquireSession("chat_id:oc_chat|work:om_indie_B", func(error) {}) {
		t.Fatal("expected goal B's session to be acquirable (different work thread)")
	}
	gate.ReleaseSession("chat_id:oc_chat|work:om_indie_B")

	// But goal A's session is busy
	if gate.TryAcquireSession("chat_id:oc_chat|work:om_indie_A", func(error) {}) {
		t.Fatal("expected goal A's session to be blocked (held by running goal)")
	}

	close(runnerA.unblock)
	<-doneA

	gA, _ := store.GetGoal(scopeA)
	if gA.Status != GoalStatusComplete {
		t.Fatalf("expected goal A complete, got %s", gA.Status)
	}
}

type threadScopedSessionGate struct {
	mu     sync.Mutex
	active map[string]context.CancelCauseFunc
}

func (g *threadScopedSessionGate) IsSessionActive(sessionKey string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.active[sessionKey]
	return ok
}

func (g *threadScopedSessionGate) TryAcquireSession(sessionKey string, cancel context.CancelCauseFunc) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.active[sessionKey]; ok {
		return false
	}
	g.active[sessionKey] = cancel
	return true
}

func (g *threadScopedSessionGate) ReleaseSession(sessionKey string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.active, sessionKey)
}

var _ SessionActivityGate = (*threadScopedSessionGate)(nil)

func TestBuildTaskDispatch_UsesGoalRunHelper(t *testing.T) {
	base := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	helper := &goalRunHelperStub{
		result: llm.RunResult{Reply: "task done via helper", NextThreadID: "task_t1"},
	}
	engine.SetGoalRunHelper(helper)
	engine.SetLLMRunner(&runLLMPanicStub{})

	task := Task{
		ID:         "task_helper_test",
		Title:      "helper task",
		Scope:      Scope{Kind: ScopeKindChat, ID: "oc_chat"},
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{OpenID: "ou_test"},
		Prompt:     "do something",
		Fresh:      true,
		SessionKey: "chat_id:oc_chat|work:om_task_test",
	}

	dispatch, err := engine.executeUserTask(t.Context(), task)
	if err != nil {
		t.Fatalf("executeUserTask: %v", err)
	}
	if dispatch.text != "task done via helper" {
		t.Fatalf("expected 'task done via helper', got %q", dispatch.text)
	}
	if dispatch.nextThreadID != "task_t1" {
		t.Fatalf("expected thread task_t1, got %q", dispatch.nextThreadID)
	}
	if helper.calls != 1 {
		t.Fatalf("expected 1 GoalRunHelper call, got %d", helper.calls)
	}
}

func TestGoalRawEventDispatcher_OnRawEventIsSet(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &rawEventCapturingStub{}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_raw_test"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_raw_test",
		Objective:  "raw event test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_raw",
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
		DeadlineAt: base.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	if runner.onRawEvent == nil {
		t.Fatal("expected OnRawEvent to be set on goal RunRequest (goalRawEventDispatcher)")
	}

	// Verify dispatcher handles events without panicking.
	runner.onRawEvent(llm.RawEvent{Kind: "tool_use", Line: "{}", Detail: "tool_use tool=bash"})
	runner.onRawEvent(llm.RawEvent{Kind: "reasoning", Line: "{}", Detail: "thinking..."})
	runner.onRawEvent(llm.RawEvent{Kind: "tool_call", Line: "{}", Detail: "call"})
}

type rawEventCapturingStub struct {
	onRawEvent llm.RawEventFunc
}

func (r *rawEventCapturingStub) Run(_ context.Context, req llm.RunRequest) (llm.RunResult, error) {
	r.onRawEvent = req.OnRawEvent
	return llm.RunResult{Reply: "done", GoalDone: true}, nil
}

func TestEngine_ExecuteGoal_OutputGoesToGoalRoute(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	helper := &goalRunHelperStub{
		result: llm.RunResult{Reply: "done", NextThreadID: "t1", GoalDone: true},
	}
	engine.SetGoalRunHelper(helper)
	engine.SetLLMRunner(&runLLMPanicStub{})

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_target|work:om_route_test"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_route_test",
		Objective:  "route output test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_target"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	if err := engine.ExecuteGoal(t.Context(), scope); err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	helper.mu.Lock()
	onProgress := helper.lastReq.OnProgress
	helper.mu.Unlock()
	if onProgress == nil {
		t.Fatal("expected OnProgress to be set on goal run call")
	}

	sender.mu.Lock()
	sendBefore := sender.sendTextCalls
	sender.mu.Unlock()
	onProgress("agent is working...")
	sender.mu.Lock()
	sendAfter := sender.sendTextCalls
	receiveType := sender.lastReceiveType
	receiveID := sender.lastReceiveID
	sender.mu.Unlock()

	if sendAfter <= sendBefore {
		t.Fatal("expected OnProgress to trigger a sender call")
	}
	if receiveType != "chat_id" {
		t.Fatalf("expected lastReceiveType 'chat_id', got %q", receiveType)
	}
	if receiveID != "oc_target" {
		t.Fatalf("expected lastReceiveID 'oc_target', got %q", receiveID)
	}
}

func TestEngine_ExecuteGoal_ScopedToWorkThreadNotCrossSession(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	gate := &threadScopedSessionGate{active: make(map[string]context.CancelCauseFunc)}
	engine.SetSessionActivityChecker(gate)

	helper := &goalRunHelperStub{
		result: llm.RunResult{Reply: "done", NextThreadID: "t1", GoalDone: true},
	}
	engine.SetGoalRunHelper(helper)
	engine.SetLLMRunner(&runLLMPanicStub{})

	scopeA := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_A"}
	scopeB := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_B"}

	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_A",
		Objective:  "goal A objective",
		Status:     GoalStatusActive,
		ThreadID:   "thread_A",
		SessionKey: "chat_id:oc_chat|work:om_A",
		Scope:      scopeA,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat_A"},
		Creator:    Actor{UserID: "u1"},
		DeadlineAt: base.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceGoal A: %v", err)
	}
	_, err = store.ReplaceGoal(GoalTask{
		ID:         "goal_B",
		Objective:  "goal B objective",
		Status:     GoalStatusActive,
		ThreadID:   "thread_B",
		SessionKey: "chat_id:oc_chat|work:om_B",
		Scope:      scopeB,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat_B"},
		Creator:    Actor{UserID: "u1"},
		DeadlineAt: base.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceGoal B: %v", err)
	}

	if err := engine.ExecuteGoal(t.Context(), scopeA); err != nil {
		t.Fatalf("ExecuteGoal A: %v", err)
	}

	helper.mu.Lock()
	onProgressA := helper.lastReq.OnProgress
	helper.mu.Unlock()
	if onProgressA == nil {
		t.Fatal("expected OnProgress to be set after goal A run")
	}

	sender.mu.Lock()
	sendBefore := sender.sendTextCalls
	sender.mu.Unlock()
	onProgressA("progress from A")
	sender.mu.Lock()
	sendAfter := sender.sendTextCalls
	receiveIDafterA := sender.lastReceiveID
	sender.mu.Unlock()

	if sendAfter <= sendBefore {
		t.Fatal("expected OnProgress to trigger sender call for goal A")
	}
	if receiveIDafterA != "oc_chat_A" {
		t.Fatalf("expected lastReceiveID 'oc_chat_A' after goal A, got %q", receiveIDafterA)
	}

	gB, err := store.GetGoal(scopeB)
	if err != nil {
		t.Fatalf("GetGoal B: %v", err)
	}
	if gB.Status != GoalStatusActive {
		t.Fatalf("expected goal B to remain active after only running A, got %s", gB.Status)
	}

	if err := engine.ExecuteGoal(t.Context(), scopeB); err != nil {
		t.Fatalf("ExecuteGoal B: %v", err)
	}

	helper.mu.Lock()
	onProgressB := helper.lastReq.OnProgress
	helper.mu.Unlock()
	if onProgressB == nil {
		t.Fatal("expected OnProgress to be set after goal B run")
	}

	onProgressB("progress from B")
	sender.mu.Lock()
	receiveIDafterB := sender.lastReceiveID
	sender.mu.Unlock()

	if receiveIDafterB != "oc_chat_B" {
		t.Fatalf("expected lastReceiveID 'oc_chat_B' after goal B, got %q", receiveIDafterB)
	}

	helper.mu.Lock()
	calls := helper.calls
	helper.mu.Unlock()
	if calls != 2 {
		t.Fatalf("expected 2 helper calls (one per goal), got %d", calls)
	}
}

func TestExecuteGoal_PassesWorkspaceDirToGoalRunHelper(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	helper := &goalRunHelperStub{result: llm.RunResult{GoalDone: true}}
	engine.SetGoalRunHelper(helper)
	engine.SetSessionActivityChecker(&workspaceDirChecker{workDir: "/custom/project"})

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_wd"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_wd_test",
		Objective:  "workspace dir test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_wd",
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
		SessionKey: "chat_id:oc_chat|work:om_wd",
		DeadlineAt: base.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	helper.mu.Lock()
	gotWD := helper.lastReq.WorkspaceDir
	helper.mu.Unlock()
	if gotWD != "/custom/project" {
		t.Errorf("workspaceDir = %q, want %q", gotWD, "/custom/project")
	}
}

func TestExecuteGoal_SendsIterationStartNotification(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	helper := &goalRunHelperStub{result: llm.RunResult{GoalDone: true}}
	engine.SetGoalRunHelper(helper)

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_notif"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_notif_test",
		Objective:  "通知测试目标",
		Status:     GoalStatusActive,
		ThreadID:   "thread_notif",
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
		DeadlineAt: base.Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	sender.mu.Lock()
	sendTextCalls := sender.sendTextCalls
	texts := append([]string{}, sender.texts...)
	sender.mu.Unlock()

	if sendTextCalls == 0 {
		t.Fatal("expected at least one SendText call for goal iteration start notification")
	}

	found := false
	for _, text := range texts {
		if strings.Contains(text, "通知测试目标") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no notification text contains objective, texts=%v", texts)
	}
}

func TestExecuteGoal_SkipsWhenNextRunAtInFuture(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{
		result: llm.RunResult{Reply: "working...", NextThreadID: "thread_1"},
	}
	engine.SetLLMRunner(runner)

	futureTime := base.Add(30 * time.Minute)
	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_delay_future"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:          "goal_delay_future",
		Objective:   "delay future test",
		Status:      GoalStatusActive,
		ThreadID:    "thread_0",
		NextRunAt:   futureTime,
		DelayReason: "test delay set by agent",
		DeadlineAt:  base.Add(48 * time.Hour),
		Scope:       scope,
		Route:       Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:     Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	// ExecuteGoal should see NextRunAt in future and return immediately
	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	// No LLM calls should have been made
	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected 0 LLM calls when NextRunAt is in the future, got %d", calls)
	}

	// Goal should remain active (not changed to any other state)
	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusActive {
		t.Fatalf("expected status active, got %s", goal.Status)
	}
}

func TestExecuteGoal_ContinuesWhenNextRunAtIsPast(t *testing.T) {
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

	pastTime := base.Add(-10 * time.Minute)
	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_delay_past"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_delay_past",
		Objective:  "delay past test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		NextRunAt:  pastTime,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
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
	if calls != 1 {
		t.Fatalf("expected 1 LLM call when NextRunAt is in the past, got %d", calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected status complete, got %s", goal.Status)
	}
}

func TestExecuteGoal_ContinuesImmediatelyWhenNextRunAtIsZero(t *testing.T) {
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

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_delay_zero"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_delay_zero",
		Objective:  "delay zero test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
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
	if calls != 1 {
		t.Fatalf("expected 1 LLM call when NextRunAt is zero, got %d", calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected status complete, got %s", goal.Status)
	}
}

func TestRunGoals_SkipsGoalWithFutureNextRunAt(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }
	engine.SetLLMRunner(&llmRunnerStub{result: llm.RunResult{Reply: "should not run", GoalDone: true}})

	futureTime := base.Add(2 * time.Hour)
	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_tick_future"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:          "goal_tick_future",
		Objective:   "tick future test",
		Status:      GoalStatusActive,
		ThreadID:    "thread_tick",
		NextRunAt:   futureTime,
		DelayReason: "test tick delay",
		DeadlineAt:  base.Add(48 * time.Hour),
		Scope:       scope,
		Route:       Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:     Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	engine.runGoals(t.Context())

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusActive {
		t.Fatalf("expected status active (tick skipped due to NextRunAt), got %s", goal.Status)
	}
	if goal.Running {
		t.Fatal("expected Running=false (tick should have skipped this goal)")
	}
}

func TestRunGoals_LaunchesGoalWhenNextRunAtIsPast(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	runner := &llmRunnerStub{result: llm.RunResult{Reply: "done", GoalDone: true, NextThreadID: "t1"}}
	engine.SetLLMRunner(runner)

	pastTime := base.Add(-5 * time.Minute)
	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_tick_past"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_tick_past",
		Objective:  "tick past test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_tick",
		NextRunAt:  pastTime,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	engine.runGoals(t.Context())

	time.Sleep(100 * time.Millisecond)

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected status complete (tick ran goal), got %s", goal.Status)
	}
}

func TestExecuteGoal_DelayAfterIterationRespected(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	// First iteration returns normally, agent has set NextRunAt via delay API
	// Second iteration would require re-entry via engine tick
	runner := &llmRunnerStub{
		results: []llm.RunResult{
			{Reply: "step 1", NextThreadID: "t1"},
			{Reply: "step 2", NextThreadID: "t2", GoalDone: true},
		},
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_delay_iter"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_delay_iter",
		Objective:  "delay iteration test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	// Simulate agent calling delay 0s during iteration:
	// Patch NextRunAt to indicate immediate continuation
	_, err = store.PatchGoal(scope, func(g *GoalTask) error {
		g.NextRunAt = time.Time{} // zero = continue immediately
		g.DelayReason = "继续推进任务"
		return nil
	})
	if err != nil {
		t.Fatalf("PatchGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	runner.mu.Lock()
	calls := runner.calls
	runner.mu.Unlock()
	if calls != 2 {
		t.Fatalf("expected 2 LLM calls (immediate continuation via delay 0s), got %d", calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusComplete {
		t.Fatalf("expected complete, got %s", goal.Status)
	}
}

func TestExecuteGoal_DelayStopsInnerLoopForFutureNextRunAt(t *testing.T) {
	SetGoalTemplates("CONT|{{.Objective}}", "TIMEOUT|{{.Objective}}")

	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	futureTime := base.Add(30 * time.Minute)
	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_delay_stop"}

	// Custom runner that patches NextRunAt after returning (simulating agent
	// calling alice-goal delay during the iteration)
	runner := &delayPatchRunnerStub{
		store:      store,
		scope:      scope,
		futureTime: futureTime,
		result: llm.RunResult{
			Reply:        "step 1",
			NextThreadID: "t1",
		},
	}
	engine.SetLLMRunner(runner)

	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_delay_stop",
		Objective:  "delay stop loop test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	// Only 1 LLM call! After iter-1, the runner patched NextRunAt to future,
	// so the inner loop exited before calling runner again.
	if runner.calls != 1 {
		t.Fatalf("expected 1 LLM call (delay stopped inner loop), got %d", runner.calls)
	}

	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusActive {
		t.Fatalf("expected status active (not done, just delayed), got %s", goal.Status)
	}
	if !goal.NextRunAt.Equal(futureTime) {
		t.Fatalf("expected NextRunAt=%s, got %s", futureTime, goal.NextRunAt)
	}
	if goal.DelayReason != "等待 CI 完成" {
		t.Fatalf("expected DelayReason preserved, got %q", goal.DelayReason)
	}
}

func TestExecuteGoal_RunningFlagClearedOnDelay(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)
	engine.now = func() time.Time { return base }

	futureTime := base.Add(10 * time.Minute)
	runner := &llmRunnerStub{
		result: llm.RunResult{Reply: "working", NextThreadID: "tt"},
	}
	engine.SetLLMRunner(runner)

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_delay_running"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_delay_running",
		Objective:  "running flag test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		NextRunAt:  futureTime,
		DeadlineAt: base.Add(48 * time.Hour),
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
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
	if goal.Running {
		t.Fatal("expected Running=false after ExecuteGoal returned due to delay")
	}
}

func TestDelayRespectsDeadlineBeforeNextRunAt(t *testing.T) {
	base := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	sender := &senderStub{}
	engine := NewEngine(store, sender)

	// Set engine clock past both NextRunAt and DeadlineAt
	engine.now = func() time.Time { return base.Add(2 * time.Hour) }
	engine.SetLLMRunner(&llmRunnerStub{result: llm.RunResult{Reply: "nope"}})

	scope := Scope{Kind: ScopeKindChat, ID: "chat_id:oc_chat|work:om_delay_deadline"}
	_, err := store.ReplaceGoal(GoalTask{
		ID:         "goal_delay_deadline",
		Objective:  "deadline before delay test",
		Status:     GoalStatusActive,
		ThreadID:   "thread_0",
		NextRunAt:  base.Add(3 * time.Hour), // NextRunAt: 13:00
		DeadlineAt: base.Add(1 * time.Hour), // DeadlineAt: 11:00, engine.now = 12:00
		Scope:      scope,
		Route:      Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:    Actor{UserID: "u1"},
	})
	if err != nil {
		t.Fatalf("ReplaceGoal: %v", err)
	}

	err = engine.ExecuteGoal(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExecuteGoal: %v", err)
	}

	// Deadline is checked BEFORE NextRunAt, so goal should be timed out
	goal, err := store.GetGoal(scope)
	if err != nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if goal.Status != GoalStatusTimeout {
		t.Fatalf("expected status timeout (deadline takes priority over NextRunAt), got %s", goal.Status)
	}
}

type workspaceDirChecker struct {
	workDir string
}

func (c *workspaceDirChecker) IsSessionActive(sessionKey string) bool {
	return false
}

func (c *workspaceDirChecker) GetSessionWorkDir(sessionKey string) string {
	return c.workDir
}

type delayPatchRunnerStub struct {
	store      *Store
	scope      Scope
	futureTime time.Time
	result     llm.RunResult
	calls      int
}

func (s *delayPatchRunnerStub) Run(_ context.Context, _ llm.RunRequest) (llm.RunResult, error) {
	s.calls++
	// Simulate agent calling alice-goal delay 30m "等待 CI 完成" during iteration
	_, _ = s.store.PatchGoal(s.scope, func(g *GoalTask) error {
		g.NextRunAt = s.futureTime
		g.DelayReason = "等待 CI 完成"
		return nil
	})
	return s.result, nil
}
