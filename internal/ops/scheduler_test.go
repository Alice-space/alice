package ops

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"alice/internal/bus"
	"alice/internal/domain"
	"alice/internal/policy"
	"alice/internal/store"
	"alice/internal/workflow"
)

func TestSchedulerTickUsesStableFireWindowAndNoDuplicate(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	reg := workflow.NewRegistry(nil)
	workflowRoot := filepath.Join("..", "..", "configs", "workflows")
	if err := reg.LoadRoots(ctx, []string{workflowRoot}); err != nil {
		t.Fatal(err)
	}
	runtime := bus.NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		domain.NewULIDGenerator(),
		bus.Config{ShardCount: 4},
	)
	nextFire := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	payload := domain.ScheduledTaskRegisteredPayload{
		ScheduledTaskID:      "sch_1",
		SpecKind:             "cron",
		SpecText:             "*/5 * * * *",
		Timezone:             "UTC",
		ScheduleRevision:     "rev-1",
		TargetWorkflowID:     "issue-delivery",
		TargetWorkflowSource: "file://configs/workflows/issue-delivery/manifest.yaml",
		TargetWorkflowRev:    "v1",
		Enabled:              true,
		NextFireAt:           nextFire,
		RegisteredAt:         time.Now().UTC(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	registerEvt := domain.EventEnvelope{
		EventID:         "evt_register_1",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     "sch_1",
		EventType:       domain.EventTypeScheduledTaskRegistered,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T09:00:00.000000000Z#0001",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.scheduled_task_registered",
		PayloadVersion:  "v1alpha1",
		Payload:         raw,
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{registerEvt}); err != nil {
		t.Fatal(err)
	}

	s := NewScheduler(runtime, st.Indexes, 30*time.Second)
	if err := s.Tick(ctx, nextFire.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx, nextFire.Add(40*time.Second)); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeScheduleTriggered {
			count++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one schedule trigger event, got %d", count)
	}

	src, ok, err := st.Indexes.GetScheduleSource(ctx, "sch_1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("missing schedule source")
	}
	if !src.NextFireAt.After(nextFire) {
		t.Fatalf("next fire should advance after trigger, got %s <= %s", src.NextFireAt, nextFire)
	}
	wantNext := time.Date(2026, 3, 10, 9, 5, 0, 0, time.UTC)
	if !src.NextFireAt.Equal(wantNext) {
		t.Fatalf("next fire should follow cron semantics: got=%s want=%s", src.NextFireAt, wantNext)
	}
}

func TestScheduleFireReconcilerBackfillsMissedWindows(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(store.Config{RootDir: t.TempDir(), SnapshotInterval: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	reg := workflow.NewRegistry(nil)
	workflowRoot := filepath.Join("..", "..", "configs", "workflows")
	if err := reg.LoadRoots(ctx, []string{workflowRoot}); err != nil {
		t.Fatal(err)
	}
	runtime := bus.NewRuntime(
		st,
		policy.NewEngine(policy.Config{MinConfidence: 0.6, DirectAllowlist: []string{"direct_query"}}),
		workflow.NewRuntime(reg),
		domain.NewULIDGenerator(),
		bus.Config{ShardCount: 4},
	)
	nextFire := time.Now().UTC().Add(-3 * time.Minute).Truncate(time.Minute)
	payload := domain.ScheduledTaskRegisteredPayload{
		ScheduledTaskID:      "sch_backfill",
		SpecKind:             "cron",
		SpecText:             "* * * * *",
		Timezone:             "UTC",
		ScheduleRevision:     "rev-1",
		TargetWorkflowID:     "issue-delivery",
		TargetWorkflowSource: "file://configs/workflows/issue-delivery/manifest.yaml",
		TargetWorkflowRev:    "v1",
		Enabled:              true,
		NextFireAt:           nextFire,
		RegisteredAt:         time.Now().UTC(),
	}
	raw, _ := json.Marshal(payload)
	registerEvt := domain.EventEnvelope{
		EventID:         "evt_register_backfill",
		AggregateKind:   domain.AggregateKindTask,
		AggregateID:     "sch_backfill",
		EventType:       domain.EventTypeScheduledTaskRegistered,
		Sequence:        1,
		GlobalHLC:       "2026-03-10T09:00:00.000000000Z#0002",
		ProducedAt:      time.Now().UTC(),
		Producer:        "test",
		PayloadSchemaID: "event.scheduled_task_registered",
		PayloadVersion:  "v1alpha1",
		Payload:         raw,
	}
	if err := st.AppendBatch(ctx, []domain.EventEnvelope{registerEvt}); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(runtime, st.Indexes, time.Minute)
	reconciler := NewScheduleFireReconciler(s, st.Indexes, time.Minute, 3)
	if err := reconciler.Reconcile(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := st.Replay(ctx, "", func(evt domain.EventEnvelope) error {
		if evt.EventType == domain.EventTypeScheduleTriggered {
			count++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatalf("expected backfill triggers, got %d", count)
	}
}
