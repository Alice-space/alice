package automation

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_CreateListPatchClaim(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
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
	base := time.Date(2026, 2, 23, 8, 0, 0, 0, time.Local)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
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

	none, err := store.ClaimDueTasks(time.Date(2026, 2, 23, 8, 59, 0, 0, time.Local), 10)
	if err != nil {
		t.Fatalf("claim before due failed: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no claimed tasks before due, got %+v", none)
	}

	claimedAt := time.Date(2026, 2, 23, 9, 0, 0, 0, time.Local)
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
	wantNext := time.Date(2026, 2, 24, 9, 0, 0, 0, time.Local)
	if !updated.NextRunAt.Equal(wantNext) {
		t.Fatalf("unexpected cron next run at: got=%s want=%s", updated.NextRunAt.Format(time.RFC3339), wantNext.Format(time.RFC3339))
	}
}

func TestStore_RecordTaskSignal_PausesTask(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title: "等待人工确认",
		Scope: Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route: Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{
			UserID: "ou_actor",
		},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeSendText, Text: "hello"},
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	if _, err := store.ClaimDueTasks(base.Add(2*time.Minute), 10); err != nil {
		t.Fatalf("claim due tasks failed: %v", err)
	}
	if err := store.RecordTaskSignal(created.ID, base.Add(2*time.Minute), "needs_human", "waiting for approval", true); err != nil {
		t.Fatalf("record signal failed: %v", err)
	}

	updated, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if updated.Status != TaskStatusPaused {
		t.Fatalf("expected paused task, got %s", updated.Status)
	}
	if updated.Running {
		t.Fatalf("expected running flag cleared, task=%+v", updated)
	}
	if !updated.NextRunAt.IsZero() {
		t.Fatalf("expected cleared next_run_at, got %s", updated.NextRunAt.Format(time.RFC3339))
	}
	if updated.LastResult != "needs_human: waiting for approval" {
		t.Fatalf("unexpected last_result: %q", updated.LastResult)
	}
}

func TestStore_ClaimDueTasks_MaxRunsPausesTask(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
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

func TestStore_ClaimDueTasks_SkipsRunningTaskUntilResultRecorded(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title: "串行执行",
		Scope: Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route: Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{
			UserID: "ou_actor",
		},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeSendText, Text: "hello"},
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	firstClaimAt := base.Add(2 * time.Minute)
	firstClaim, err := store.ClaimDueTasks(firstClaimAt, 10)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if len(firstClaim) != 1 || firstClaim[0].ID != created.ID {
		t.Fatalf("unexpected first claim: %+v", firstClaim)
	}

	running, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get running task failed: %v", err)
	}
	if !running.Running {
		t.Fatalf("expected task to be marked running, task=%+v", running)
	}

	secondClaimAt := base.Add(3 * time.Minute)
	secondClaim, err := store.ClaimDueTasks(secondClaimAt, 10)
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	if len(secondClaim) != 0 {
		t.Fatalf("expected running task to be skipped, got %+v", secondClaim)
	}

	if err := store.RecordTaskResult(created.ID, secondClaimAt, nil); err != nil {
		t.Fatalf("record result failed: %v", err)
	}

	cleared, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get cleared task failed: %v", err)
	}
	if cleared.Running {
		t.Fatalf("expected running flag to be cleared, task=%+v", cleared)
	}

	thirdClaim, err := store.ClaimDueTasks(secondClaimAt, 10)
	if err != nil {
		t.Fatalf("third claim failed: %v", err)
	}
	if len(thirdClaim) != 1 || thirdClaim[0].ID != created.ID {
		t.Fatalf("expected task to become claimable after result, got %+v", thirdClaim)
	}
}

func TestStore_ResetRunningTasks(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	created, err := store.CreateTask(Task{
		Title: "恢复中的任务",
		Scope: Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route: Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator: Actor{
			UserID: "ou_actor",
		},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeSendText, Text: "hello"},
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	if _, err := store.ClaimDueTasks(base.Add(2*time.Minute), 10); err != nil {
		t.Fatalf("claim due tasks failed: %v", err)
	}

	if err := store.ResetRunningTasks(); err != nil {
		t.Fatalf("reset running tasks failed: %v", err)
	}

	task, err := store.GetTask(created.ID)
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if task.Running {
		t.Fatalf("expected running flag to be reset, task=%+v", task)
	}
}

func TestStore_DeletedTaskRetention(t *testing.T) {
	base := time.Date(2026, 2, 23, 10, 0, 0, 0, time.UTC)
	store := NewStore(filepath.Join(t.TempDir(), "automation.db"))
	store.now = func() time.Time { return base }

	expired, err := store.CreateTask(Task{
		Title:    "过期已删除任务",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeSendText, Text: "old"},
	})
	if err != nil {
		t.Fatalf("create expired task failed: %v", err)
	}
	recent, err := store.CreateTask(Task{
		Title:    "近期已删除任务",
		Scope:    Scope{Kind: ScopeKindUser, ID: "ou_actor"},
		Route:    Route{ReceiveIDType: "user_id", ReceiveID: "ou_actor"},
		Creator:  Actor{UserID: "ou_actor"},
		Schedule: Schedule{Type: ScheduleTypeInterval, EverySeconds: 60},
		Action:   Action{Type: ActionTypeSendText, Text: "recent"},
	})
	if err != nil {
		t.Fatalf("create recent task failed: %v", err)
	}

	store.now = func() time.Time { return base.Add(time.Minute) }
	if _, err := store.PatchTask(expired.ID, func(task *Task) error {
		task.Status = TaskStatusDeleted
		task.DeletedAt = base.Add(-deletedTaskRetention - time.Hour)
		return nil
	}); err != nil {
		t.Fatalf("patch expired task failed: %v", err)
	}
	deletedRecent, err := store.PatchTask(recent.ID, func(task *Task) error {
		task.Status = TaskStatusDeleted
		return nil
	})
	if err != nil {
		t.Fatalf("patch recent task failed: %v", err)
	}
	if deletedRecent.DeletedAt.IsZero() {
		t.Fatal("expected deleted_at to be recorded")
	}
	if !deletedRecent.NextRunAt.IsZero() {
		t.Fatalf("expected deleted task next_run_at to be cleared, got %s", deletedRecent.NextRunAt.Format(time.RFC3339))
	}

	if _, err := store.ClaimDueTasks(base.Add(2*time.Minute), 10); err != nil {
		t.Fatalf("claim due tasks failed: %v", err)
	}

	if _, err := store.GetTask(expired.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("expected expired deleted task to be purged, got err=%v", err)
	}
	preserved, err := store.GetTask(recent.ID)
	if err != nil {
		t.Fatalf("get preserved deleted task failed: %v", err)
	}
	if preserved.Status != TaskStatusDeleted {
		t.Fatalf("expected preserved task to stay deleted, got %s", preserved.Status)
	}
	if preserved.DeletedAt.IsZero() {
		t.Fatal("expected preserved deleted task to keep deleted_at")
	}
}
