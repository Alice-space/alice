package connector

import (
	"context"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApp_EnqueueJobAssignsVersion(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	app.latest["chat_id:oc_chat"] = 1

	job := &Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		EventID:       "evt_new",
	}

	queued, cancelActive, canceledEventID := app.enqueueJob(job)
	if !queued {
		t.Fatal("expected job to be queued")
	}
	if job.SessionKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected session key: %s", job.SessionKey)
	}
	if job.SessionVersion != 2 {
		t.Fatalf("unexpected session version: %d", job.SessionVersion)
	}
	if app.latest[job.SessionKey] != 2 {
		t.Fatalf("latest version should be 2, got %d", app.latest[job.SessionKey])
	}
	if canceledEventID != "" {
		t.Fatalf("expected empty canceled event id, got %q", canceledEventID)
	}
	if cancelActive != nil {
		t.Fatal("expected no active cancel func")
	}
}

func TestApp_WorkerLoopSerializesSameSession(t *testing.T) {
	cfg := configForTest()
	cfg.WorkerConcurrency = 2
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
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
		EventID:         "evt_serial_1",
		SourceMessageID: "om_serial_1",
		Text:            "first",
	}
	second := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_serial_2",
		SourceMessageID: "om_serial_2",
		Text:            "second",
	}
	if queued, _, _ := app.enqueueJob(first); !queued {
		t.Fatal("expected first job to be queued")
	}
	if queued, _, _ := app.enqueueJob(second); !queued {
		t.Fatal("expected second job to be queued")
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 1
	}, "expected first same-session call to start")
	time.Sleep(150 * time.Millisecond)
	if got := blockingCodex.CallCount(); got != 1 {
		t.Fatalf("expected same session to stay serialized while first call is running, got %d calls", got)
	}

	blockingCodex.Release()
	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 2
	}, "expected second same-session call to start after first finishes")
	waitForCondition(t, 2*time.Second, func() bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return len(app.pending) == 0
	}, "expected serialized jobs to complete and clear pending state")
}

func TestApp_WorkerLoopAllowsParallelDifferentSessions(t *testing.T) {
	cfg := configForTest()
	cfg.WorkerConcurrency = 2
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
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
		app.mu.Lock()
		defer app.mu.Unlock()
		return len(app.pending) == 0
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

	if got := restored.latest["chat_id:oc_chat"]; got != 1 {
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

func TestApp_RuntimeStateRestoreKeepsPendingQueueOrderWithinSession(t *testing.T) {
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
	if got := len(restored.queue); got != 2 {
		t.Fatalf("expected restored queue len 2, got %d", got)
	}
	firstRecovered := <-restored.queue
	secondRecovered := <-restored.queue
	if firstRecovered.EventID != "evt_restore_serial_1" || firstRecovered.SessionVersion != 1 {
		t.Fatalf("unexpected first recovered job: %+v", firstRecovered)
	}
	if secondRecovered.EventID != "evt_restore_serial_2" || secondRecovered.SessionVersion != 2 {
		t.Fatalf("unexpected second recovered job: %+v", secondRecovered)
	}
}

func TestApp_InterruptedJobDroppedOnRestart(t *testing.T) {
	cfg := configForTest()
	statePath := t.TempDir() + "/runtime_state.json"
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
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
		app.mu.Lock()
		defer app.mu.Unlock()
		_, ok := app.pending[pendingJobKey(*job)]
		return !ok
	}, "interrupted in-progress job should be dropped")

	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	if got := len(restored.queue); got != 0 {
		t.Fatalf("expected no restored queue job for interrupted task, got %d", got)
	}
}

func TestApp_RestartDropsInProgressJobButKeepsQueuedJobs(t *testing.T) {
	cfg := configForTest()
	statePath := t.TempDir() + "/runtime_state.json"
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
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
		app.mu.Lock()
		defer app.mu.Unlock()
		_, firstExists := app.pending[pendingJobKey(*inProgress)]
		_, secondExists := app.pending[pendingJobKey(*queued)]
		return !firstExists && secondExists
	}, "expected in-progress dropped but queued job kept pending")

	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	if got := len(restored.queue); got != 1 {
		t.Fatalf("expected one restored queued job, got queue len %d", got)
	}
	recovered := <-restored.queue
	if recovered.EventID != queued.EventID {
		t.Fatalf("unexpected recovered event id: %s", recovered.EventID)
	}
}

func TestApp_RuntimeStatePersistAndRestoreMediaWindow(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	statePath := t.TempDir() + "/runtime_state.json"
	base := time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC)

	app := NewApp(cfg, nil)
	app.now = func() time.Time { return base }
	if err := app.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load runtime state failed: %v", err)
	}

	mediaEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_media_state"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId: strPtr("ou_user_1"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_media_state"),
				MessageType: strPtr("image"),
				Content:     strPtr(`{"image_key":"img_state"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}
	if err := app.onMessageReceive(context.Background(), mediaEvent); err != nil {
		t.Fatalf("unexpected media event error: %v", err)
	}
	windowKey := buildMediaWindowKey("oc_chat", "open_id:ou_user_1")
	app.mu.Lock()
	originalCount := len(app.mediaWindow[windowKey])
	app.mu.Unlock()
	if originalCount != 1 {
		t.Fatalf("expected 1 cached entry before flush, got %d", originalCount)
	}

	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	restored.now = func() time.Time { return base.Add(1 * time.Minute) }
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	restored.mu.Lock()
	restoredCount := len(restored.mediaWindow[windowKey])
	restored.mu.Unlock()
	if restoredCount != 1 {
		t.Fatalf("expected 1 restored media entry, got %d", restoredCount)
	}
}

func TestApp_SelfUpdateInterruptedJobDroppedOnRestart(t *testing.T) {
	cfg := configForTest()
	statePath := t.TempDir() + "/runtime_state.json"
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
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
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_self_update",
		EventID:         "evt_self_update",
		Text:            "修改完后更新并重启你自己",
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
		app.mu.Lock()
		defer app.mu.Unlock()
		_, ok := app.pending[pendingJobKey(*job)]
		return !ok
	}, "self-update in-progress job should be dropped after shutdown cancellation")

	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	if got := len(restored.queue); got != 0 {
		t.Fatalf("expected no restored queue job for interrupted self-update task, got %d", got)
	}
}
