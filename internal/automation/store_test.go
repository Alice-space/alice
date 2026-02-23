package automation

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_CreateListPatchClaim(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title: "每分钟提醒",
		Scope: Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route: Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{
			UserID: "ou_actor",
			Name:   "Alice",
		},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeSendText, Text: "ping", MentionUserIDs: []string{"ou_actor"}},
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated task id")
	}

	list, err := store.ListTasks(Scope{Kind: ScopeKindUser, ID: "ou_actor"}, "", 20)
	if err != nil {
		t.Fatalf("list task failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 task, got %d", len(list))
	}

	store.now = func() time.Time { return base.Add(2 * time.Minute) }
	claimed, err := store.ClaimDueTasks(base.Add(2*time.Minute), 10)
	if err != nil {
		t.Fatalf("claim due tasks failed: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != created.ID {
		t.Fatalf("unexpected claimed tasks: %+v", claimed)
	}

	if err := store.RecordTaskResult(created.ID, base.Add(2*time.Minute), errors.New("x")); err != nil {
		t.Fatalf("record result failed: %v", err)
	}
	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get updated task failed: %v", err)
	}
	if updated.ConsecutiveFailures != 1 {
		t.Fatalf("unexpected failure count: %d", updated.ConsecutiveFailures)
	}

	patched, err := store.PatchTask(created.ID, func(task *Task) error {
		task.Status = TaskStatusPaused
		return nil
	})
	if err != nil {
		t.Fatalf("patch task failed: %v", err)
	}
	if patched.Status != TaskStatusPaused {
		t.Fatalf("unexpected patched status: %s", patched.Status)
	}
}

func TestStore_ClaimCronTask(t *testing.T) {
	base := time.Date(2026, 2, 23, 8, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title: "9点简报",
		Scope: Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route: Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{
			UserID: "ou_actor",
			Name:   "Alice",
		},
		Schedule: Schedule{Type: ScheduleTypeCron, CronExpr: "0 9 * * *"},
		Action:   Action{Type: ActionTypeSendText, Text: "daily brief"},
	})
	if err != nil {
		t.Fatalf("create cron task failed: %v", err)
	}

	none, err := store.ClaimDueTasks(time.Date(2026, 2, 23, 8, 59, 0, 0, time.UTC), 10)
	if err != nil {
		t.Fatalf("claim before due failed: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no claimed tasks before due, got %+v", none)
	}

	claimedAt := time.Date(2026, 2, 23, 9, 0, 0, 0, time.UTC)
	claimed, err := store.ClaimDueTasks(claimedAt, 10)
	if err != nil {
		t.Fatalf("claim cron due tasks failed: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != created.ID {
		t.Fatalf("unexpected claimed cron tasks: %+v", claimed)
	}

	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get cron task failed: %v", err)
	}
	wantNext := time.Date(2026, 2, 24, 9, 0, 0, 0, time.UTC)
	if !updated.NextRunAt.Equal(wantNext) {
		t.Fatalf("unexpected cron next run at: got=%s want=%s", updated.NextRunAt.Format(time.RFC3339), wantNext.Format(time.RFC3339))
	}
}

func TestStore_ClaimDueTasks_MaxRunsPausesTask(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title: "单次触发",
		Scope: Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route: Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{
			UserID: "ou_actor",
		},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeSendText, Text: "run once"},
		MaxRuns:  1,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	claimedAt := base.Add(2 * time.Minute)
	claimed, err := store.ClaimDueTasks(claimedAt, 10)
	if err != nil {
		t.Fatalf("claim due tasks failed: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != created.ID {
		t.Fatalf("unexpected claimed tasks: %+v", claimed)
	}

	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if updated.RunCount != 1 {
		t.Fatalf("unexpected run_count: %d", updated.RunCount)
	}
	if updated.Status != TaskStatusPaused {
		t.Fatalf("expected task paused after reaching max_runs, got %s", updated.Status)
	}
	if !updated.NextRunAt.IsZero() {
		t.Fatalf("expected next_run_at to be cleared, got %s", updated.NextRunAt.Format(time.RFC3339))
	}

	secondClaim, err := store.ClaimDueTasks(claimedAt.Add(10*time.Minute), 10)
	if err != nil {
		t.Fatalf("second claim due tasks failed: %v", err)
	}
	if len(secondClaim) != 0 {
		t.Fatalf("expected no tasks after max_runs reached, got %+v", secondClaim)
	}
}
