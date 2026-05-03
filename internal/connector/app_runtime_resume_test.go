package connector

import (
	"context"
	"testing"
	"time"
)

func TestApp_WorkerLoopAllowsParallelDifferentSessions(t *testing.T) {
	cfg := configForTest()
	cfg.WorkerConcurrency = 2
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)
	app := NewApp(cfg, processor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.workerLoop(ctx, 0)
	go app.workerLoop(ctx, 1)

	first := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_parallel_1",
		SourceMessageID: "om_parallel_1",
		Text:            "first",
	}
	second := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_2",
		EventID:         "evt_parallel_2",
		SourceMessageID: "om_parallel_2",
		Text:            "second",
	}
	if queued, _, _ := app.enqueueJob(first); !queued {
		t.Fatal("expected first job to be queued")
	}
	if queued, _, _ := app.enqueueJob(second); !queued {
		t.Fatal("expected second job to be queued")
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 2
	}, "expected different sessions to run in parallel")

	blockingCodex.Release()
	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()
		return len(app.state.pending) == 0
	}, "expected parallel jobs to complete and clear pending state")
}

func TestApp_RuntimeStatePersistAndRestorePendingJob(t *testing.T) {
	cfg := configForTest()
	statePath := t.TempDir() + "/runtime_state.json"

	app := NewApp(cfg, nil)
	if err := app.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load runtime state failed: %v", err)
	}

	job := &Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		EventID:       "evt_runtime_restore",
		Text:          "hello",
		ReceivedAt:    time.Date(2026, 2, 21, 18, 0, 0, 0, time.UTC),
	}
	queued, _, _ := app.enqueueJob(job)
	if !queued {
		t.Fatal("expected job to be queued")
	}
	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}

	if got := restored.state.latest["chat_id:oc_chat"]; got != 1 {
		t.Fatalf("expected latest version 1 after restore, got %d", got)
	}
	if got := len(restored.queue); got != 1 {
		t.Fatalf("expected restored queue len 1, got %d", got)
	}

	recovered := <-restored.queue
	if recovered.EventID != "evt_runtime_restore" {
		t.Fatalf("unexpected recovered event id: %s", recovered.EventID)
	}
	if recovered.SessionVersion != 1 {
		t.Fatalf("unexpected recovered session version: %d", recovered.SessionVersion)
	}
}

func TestApp_RuntimeStateRestoreKeepsLatestPendingJobWithinSession(t *testing.T) {
	cfg := configForTest()
	statePath := t.TempDir() + "/runtime_state.json"
	base := time.Date(2026, 2, 22, 12, 0, 0, 0, time.UTC)

	app := NewApp(cfg, nil)
	if err := app.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load runtime state failed: %v", err)
	}

	first := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_restore_serial_1",
		SourceMessageID: "om_restore_serial_1",
		Text:            "first",
		ReceivedAt:      base,
	}
	second := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_restore_serial_2",
		SourceMessageID: "om_restore_serial_2",
		Text:            "second",
		ReceivedAt:      base.Add(1 * time.Second),
	}
	if queued, _, _ := app.enqueueJob(first); !queued {
		t.Fatal("expected first job to be queued")
	}
	if queued, _, _ := app.enqueueJob(second); !queued {
		t.Fatal("expected second job to be queued")
	}
	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	if got := len(restored.queue); got != 1 {
		t.Fatalf("expected restored queue len 1, got %d", got)
	}
	recovered := <-restored.queue
	if recovered.EventID != "evt_restore_serial_2" || recovered.SessionVersion != 2 {
		t.Fatalf("unexpected recovered job: %+v", recovered)
	}
	if got := restored.state.latest["chat_id:oc_chat|thread:omt_thread_1"]; got != 2 {
		t.Fatalf("expected latest version 2 after restore, got %d", got)
	}
}

func TestApp_InterruptedJobRemainsPendingForRetryAfterRestart(t *testing.T) {
	cfg := configForTest()
	statePath := t.TempDir() + "/runtime_state.json"
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)
	app := NewApp(cfg, processor)
	if err := app.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load runtime state failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.workerLoop(ctx, 0)

	job := &Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		EventID:       "evt_interrupt",
		Text:          "need resume",
	}
	queued, _, _ := app.enqueueJob(job)
	if !queued {
		t.Fatal("expected job to be queued")
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 1
	}, "expected codex call to start")

	cancel()
	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()
		pendingJob, ok := app.state.pending[pendingJobKey(*job)]
		if !ok {
			return false
		}
		return normalizeJobWorkflowPhase(pendingJob.WorkflowPhase) == jobWorkflowPhaseNormal
	}, "interrupted in-progress job should remain pending in normal phase")

	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	if got := len(restored.queue); got != 1 {
		t.Fatalf("expected one restored queue job, got %d", got)
	}
	recovered := <-restored.queue
	if recovered.EventID != job.EventID {
		t.Fatalf("unexpected recovered event id: %s", recovered.EventID)
	}
	if normalizeJobWorkflowPhase(recovered.WorkflowPhase) != jobWorkflowPhaseNormal {
		t.Fatalf("expected recovered job in normal phase, got %q", recovered.WorkflowPhase)
	}
}

func TestApp_RestartKeepsOnlyLatestSameSessionJobPending(t *testing.T) {
	cfg := configForTest()
	statePath := t.TempDir() + "/runtime_state.json"
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)
	app := NewApp(cfg, processor)
	if err := app.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load runtime state failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.workerLoop(ctx, 0)

	inProgress := &Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		EventID:       "evt_in_progress_drop",
		Text:          "first",
	}
	queued := &Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		EventID:       "evt_queued_keep",
		Text:          "second",
	}
	if queuedOK, _, _ := app.enqueueJob(inProgress); !queuedOK {
		t.Fatal("expected in-progress job to be queued")
	}
	if queuedOK, _, _ := app.enqueueJob(queued); !queuedOK {
		t.Fatal("expected queued job to be queued")
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 1
	}, "expected first codex call to start")

	cancel()
	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()

		_, firstExists := app.state.pending[pendingJobKey(*inProgress)]
		secondPending, secondExists := app.state.pending[pendingJobKey(*queued)]
		if firstExists || !secondExists {
			return false
		}
		return normalizeJobWorkflowPhase(secondPending.WorkflowPhase) == jobWorkflowPhaseNormal
	}, "expected only latest same-session job kept pending in normal phase")

	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	if got := len(restored.queue); got != 1 {
		t.Fatalf("expected one restored job, got queue len %d", got)
	}
	recovered := <-restored.queue
	if recovered.EventID != queued.EventID {
		t.Fatalf("unexpected recovered event id: %s", recovered.EventID)
	}
	if normalizeJobWorkflowPhase(recovered.WorkflowPhase) != jobWorkflowPhaseNormal {
		t.Fatalf("expected recovered job in normal phase, got %q", recovered.WorkflowPhase)
	}
}
