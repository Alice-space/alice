package connector

import (
	"context"
	"strings"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApp_EnqueueJobAssignsVersion(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	app.state.latest["chat_id:oc_chat"] = 1

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
	if app.state.latest[job.SessionKey] != 2 {
		t.Fatalf("latest version should be 2, got %d", app.state.latest[job.SessionKey])
	}
	if canceledEventID != "" {
		t.Fatalf("expected empty canceled event id, got %q", canceledEventID)
	}
	if cancelActive != nil {
		t.Fatal("expected no active cancel func")
	}
}

func TestApp_EnqueueJobInterruptsActiveSession(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	canceled := false
	app.state.latest["chat_id:oc_chat|thread:omt_thread_1"] = 1
	app.state.pending["session:chat_id:oc_chat|thread:omt_thread_1#1"] = Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		SessionVersion:  1,
		EventID:         "evt_active",
		SourceMessageID: "om_active",
		Text:            "first",
	}
	app.state.active["chat_id:oc_chat|thread:omt_thread_1"] = activeSessionRun{
		eventID: "evt_active",
		version: 1,
		cancel: func() {
			canceled = true
		},
	}

	job := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_new",
		SourceMessageID: "om_new",
		Text:            "latest",
	}
	queued, cancelActive, canceledEventID := app.enqueueJob(job)
	if !queued {
		t.Fatal("expected job to be queued")
	}
	if cancelActive == nil {
		t.Fatal("expected active session cancel func")
	}
	if canceledEventID != "evt_active" {
		t.Fatalf("unexpected canceled event id: %q", canceledEventID)
	}
	if job.SessionVersion != 2 {
		t.Fatalf("unexpected new session version: %d", job.SessionVersion)
	}
	cancelActive()
	if !canceled {
		t.Fatal("expected active cancel func to be invoked")
	}
	if got := app.state.superseded[job.SessionKey]; got != 2 {
		t.Fatalf("expected superseded version 2, got %d", got)
	}
	if _, ok := app.state.pending["session:chat_id:oc_chat|thread:omt_thread_1#1"]; ok {
		t.Fatal("expected older pending job to be removed")
	}
}

func TestApp_EnqueueJobSupersedesQueuedSessionJobs(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	first := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_first",
		SourceMessageID: "om_first",
		Text:            "first",
	}
	if queued, cancelActive, canceledEventID := app.enqueueJob(first); !queued {
		t.Fatal("expected first job to be queued")
	} else {
		if cancelActive != nil {
			t.Fatal("expected first job not to interrupt an active run")
		}
		if canceledEventID != "" {
			t.Fatalf("expected no canceled event id, got %q", canceledEventID)
		}
	}

	second := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_second",
		SourceMessageID: "om_second",
		Text:            "second",
	}
	queued, cancelActive, canceledEventID := app.enqueueJob(second)
	if !queued {
		t.Fatal("expected second job to be queued")
	}
	if cancelActive != nil {
		t.Fatal("expected queued supersession not to return active cancel func")
	}
	if canceledEventID != "" {
		t.Fatalf("expected empty canceled event id, got %q", canceledEventID)
	}
	if second.SessionVersion != 2 {
		t.Fatalf("unexpected second session version: %d", second.SessionVersion)
	}
	if got := app.state.superseded[second.SessionKey]; got != 2 {
		t.Fatalf("expected superseded version 2, got %d", got)
	}
	if _, ok := app.state.pending[pendingJobKey(*first)]; ok {
		t.Fatal("expected older queued pending job to be removed")
	}
	if pendingJob, ok := app.state.pending[pendingJobKey(*second)]; !ok {
		t.Fatal("expected latest queued job to remain pending")
	} else if pendingJob.SessionVersion != 2 {
		t.Fatalf("unexpected pending session version: %d", pendingJob.SessionVersion)
	}
	if got := len(app.queue); got != 2 {
		t.Fatalf("expected both buffered jobs to remain in queue, got %d", got)
	}

	firstQueued := <-app.queue
	if firstQueued.EventID != first.EventID {
		t.Fatalf("unexpected first queued event id: %s", firstQueued.EventID)
	}
	if !app.isSupersededJob(firstQueued.SessionKey, firstQueued.SessionVersion) {
		t.Fatal("expected older buffered job to be marked superseded")
	}

	secondQueued := <-app.queue
	if secondQueued.EventID != second.EventID {
		t.Fatalf("unexpected second queued event id: %s", secondQueued.EventID)
	}
	if app.isSupersededJob(secondQueued.SessionKey, secondQueued.SessionVersion) {
		t.Fatal("expected latest buffered job not to be superseded")
	}
}

func TestApp_WorkerLoopInterruptsSameSessionAndResumesLatest(t *testing.T) {
	cfg := configForTest()
	interruptibleCodex := newInterruptibleResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		interruptibleCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)
	app := NewApp(cfg, processor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.workerLoop(ctx, 0)

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
	if queued, cancelActive, _ := app.enqueueJob(first); !queued {
		t.Fatal("expected first job to be queued")
	} else if cancelActive != nil {
		t.Fatal("expected first job not to interrupt an active run")
	}
	interruptibleCodex.WaitForCall(t, 1)

	queued, cancelActive, canceledEventID := app.enqueueJob(second)
	if !queued {
		t.Fatal("expected second job to be queued")
	}
	if cancelActive == nil {
		t.Fatal("expected second job to interrupt the active run")
	}
	if canceledEventID != first.EventID {
		t.Fatalf("unexpected canceled event id: %q", canceledEventID)
	}
	cancelActive()
	waitForCondition(t, 2*time.Second, func() bool {
		return interruptibleCodex.CallCount() == 2
	}, "expected latest same-session call to resume after interruption")
	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()
		return len(app.state.pending) == 0
	}, "expected interrupted and latest jobs to clear pending state")

	threadIDs := interruptibleCodex.ThreadIDs()
	if len(threadIDs) != 2 {
		t.Fatalf("expected 2 codex calls, got %#v", threadIDs)
	}
	if threadIDs[0] != "" {
		t.Fatalf("expected first call to start a new thread, got %q", threadIDs[0])
	}
	if threadIDs[1] != "thread_after_interrupt" {
		t.Fatalf("expected second call to resume interrupted thread, got %q", threadIDs[1])
	}
	if sender.replyCardCalls != 4 {
		t.Fatalf("expected interrupted flow to send 4 reply cards, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 4 {
		t.Fatalf("unexpected reply card history: %#v", sender.replyCards)
	}
	if !strings.Contains(sender.replyCards[1], "已中断") {
		t.Fatalf("expected interrupted card for first reply, got %q", sender.replyCards[1])
	}
	if !strings.Contains(sender.replyCards[3], "latest answer") {
		t.Fatalf("expected final card for latest reply, got %q", sender.replyCards[3])
	}
}

func TestApp_WorkerLoopInterruptsGroupThreadReplyMappedBackToRootSession(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"

	interruptibleCodex := newInterruptibleResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		interruptibleCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)
	app := NewApp(cfg, processor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.workerLoop(ctx, 0)

	firstEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_group_root"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_group_root"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> first"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
				Mentions: []*larkim.MentionEvent{
					{
						Id: &larkim.UserId{
							OpenId: strPtr("ou_bot"),
						},
					},
				},
			},
		},
	}
	if err := app.onMessageReceive(context.Background(), firstEvent); err != nil {
		t.Fatalf("unexpected first event error: %v", err)
	}
	interruptibleCodex.WaitForCall(t, 1)

	secondEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_group_thread_reply"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_group_reply"),
				ParentId:    strPtr("om_reply_card"),
				RootId:      strPtr("om_reply_card"),
				ThreadId:    strPtr("omt_group_thread"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> second"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
				Mentions: []*larkim.MentionEvent{
					{
						Id: &larkim.UserId{
							OpenId: strPtr("ou_bot"),
						},
					},
				},
			},
		},
	}
	if err := app.onMessageReceive(context.Background(), secondEvent); err != nil {
		t.Fatalf("unexpected second event error: %v", err)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		return interruptibleCodex.CallCount() == 2
	}, "expected group thread reply to interrupt and trigger a resumed second call")
	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()
		return len(app.state.pending) == 0
	}, "expected group thread interrupt flow to clear pending state")

	threadIDs := interruptibleCodex.ThreadIDs()
	if len(threadIDs) != 2 {
		t.Fatalf("expected 2 codex calls, got %#v", threadIDs)
	}
	if threadIDs[0] != "" {
		t.Fatalf("expected first call to start a new thread, got %q", threadIDs[0])
	}
	if threadIDs[1] != "thread_after_interrupt" {
		t.Fatalf("expected second call to resume interrupted thread, got %q", threadIDs[1])
	}
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
