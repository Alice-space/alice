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

func TestApp_EnqueueJobQueuesBehindActiveSession(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

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
		cancel:  func(error) {},
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
	if cancelActive != nil {
		t.Fatal("expected regular message not to interrupt the active run")
	}
	if canceledEventID != "" {
		t.Fatalf("expected empty canceled event id, got %q", canceledEventID)
	}
	if job.SessionVersion != 2 {
		t.Fatalf("unexpected new session version: %d", job.SessionVersion)
	}
	if got := app.state.superseded[job.SessionKey]; got != 0 {
		t.Fatalf("expected no superseded marker, got %d", got)
	}
	// Active pending work stays recorded until the worker completes it.
	if _, ok := app.state.pending["session:chat_id:oc_chat|thread:omt_thread_1#1"]; !ok {
		t.Fatal("expected active pending job to remain recorded")
	}
	if _, ok := app.state.pending[pendingJobKey(*job)]; !ok {
		t.Fatal("expected queued followup to remain pending")
	}
}

func TestApp_EnqueueStopJobUsesStopCause(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	var cancelCause error
	app.state.latest["chat_id:oc_chat|thread:omt_thread_1"] = 1
	app.state.active["chat_id:oc_chat|thread:omt_thread_1"] = activeSessionRun{
		eventID: "evt_active",
		version: 1,
		cancel: func(err error) {
			cancelCause = err
		},
	}

	job := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_stop",
		SourceMessageID: "om_stop",
		Text:            "/stop",
	}
	queued, cancelActive, canceledEventID := app.enqueueJob(job)
	if !queued {
		t.Fatal("expected stop job to be queued")
	}
	if cancelActive == nil {
		t.Fatal("expected stop job to interrupt the active run")
	}
	if canceledEventID != "evt_active" {
		t.Fatalf("unexpected canceled event id: %q", canceledEventID)
	}
	cancelActive()
	if cancelCause != errSessionStopped {
		t.Fatalf("expected stop cause, got %v", cancelCause)
	}
}

func TestApp_EnqueueJobInterruptsAutomationSession(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	var cancelCause error
	app.state.active["chat_id:oc_chat"] = activeSessionRun{
		version: 0,
		cancel: func(err error) {
			cancelCause = err
		},
	}

	job := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat",
		EventID:         "evt_user_message",
		SourceMessageID: "om_user_message",
		Text:            "user message",
	}
	queued, cancelActive, canceledEventID := app.enqueueJob(job)
	if !queued {
		t.Fatal("expected user message to be queued")
	}
	if cancelActive == nil {
		t.Fatal("expected user message to interrupt automation session")
	}
	if canceledEventID != "" {
		t.Fatalf("expected empty canceled event id for automation session, got %q", canceledEventID)
	}
	cancelActive()
	if cancelCause != errSessionInterrupted {
		t.Fatalf("expected interrupt cause, got %v", cancelCause)
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

func TestApp_WorkerLoopQueuesSameSessionWithoutInterrupting(t *testing.T) {
	cfg := configForTest()
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
	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 1
	}, "expected first same-session call to start")

	queued, cancelActive, canceledEventID := app.enqueueJob(second)
	if !queued {
		t.Fatal("expected second job to be queued")
	}
	if cancelActive != nil {
		t.Fatal("expected second job not to interrupt the active run")
	}
	if canceledEventID != "" {
		t.Fatalf("unexpected canceled event id: %q", canceledEventID)
	}
	if got := blockingCodex.CallCount(); got != 1 {
		t.Fatalf("expected queued followup not to start before first completes, got %d calls", got)
	}

	blockingCodex.Release()
	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 2
	}, "expected queued same-session call to run after first completes")
	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()
		return len(app.state.pending) == 0 && len(app.state.active) == 0
	}, "expected queued jobs to clear pending state")

	if len(sender.replyCards) != 4 {
		t.Fatalf("unexpected reply card history: %#v", sender.replyCards)
	}
	for _, card := range sender.replyCards {
		if strings.Contains(card, "已中断") {
			t.Fatalf("queued flow should not send interrupted card, got %#v", sender.replyCards)
		}
	}
	if !strings.Contains(sender.replyCards[1], "- summary") {
		t.Fatalf("expected first final card for queued flow, got %q", sender.replyCards[1])
	}
	if !strings.Contains(sender.replyCards[3], "- summary") {
		t.Fatalf("expected second final card for queued flow, got %q", sender.replyCards[3])
	}
}

func TestApp_WorkerLoopQueuesGroupThreadReplyMappedBackToRootSession(t *testing.T) {
	cfg := configForTest()
	blockingCodex := newBlockingResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		blockingCodex,
		sender,
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)
	app := NewApp(cfg, processor)
	app.SetBotOpenID("ou_bot")

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
	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 1
	}, "expected first group thread call to start")

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
	if got := blockingCodex.CallCount(); got != 1 {
		t.Fatalf("expected group thread followup not to start before first completes, got %d calls", got)
	}

	blockingCodex.Release()
	waitForCondition(t, 2*time.Second, func() bool {
		return blockingCodex.CallCount() == 2
	}, "expected group thread followup to run after first completes")
	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()
		return len(app.state.pending) == 0 && len(app.state.active) == 0
	}, "expected group thread queued flow to clear pending state")
	for _, card := range sender.replyCards {
		if strings.Contains(card, "已中断") {
			t.Fatalf("queued group thread flow should not send interrupted card, got %#v", sender.replyCards)
		}
	}
}

func TestApp_WorkerLoopStopCommandKeepsInterruptedThreadForLaterResume(t *testing.T) {
	cfg := configForTest()

	interruptibleCodex := newInterruptibleResumableCodexStub()
	sender := &senderStub{}
	processor := NewProcessor(
		interruptibleCodex,
		sender,
		"暂时不可用，请稍后重试。",
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
		EventID:         "evt_stop_first",
		SourceMessageID: "om_stop_first",
		Text:            "first",
	}
	stop := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_stop_command",
		SourceMessageID: "om_stop_command",
		Text:            "/stop",
	}
	resume := &Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SessionKey:      "chat_id:oc_chat|thread:omt_thread_1",
		EventID:         "evt_stop_resume",
		SourceMessageID: "om_stop_resume",
		Text:            "resume after stop",
	}

	if queued, _, _ := app.enqueueJob(first); !queued {
		t.Fatal("expected first job to be queued")
	}
	interruptibleCodex.WaitForCall(t, 1)

	queued, cancelActive, canceledEventID := app.enqueueJob(stop)
	if !queued {
		t.Fatal("expected stop job to be queued")
	}
	if cancelActive == nil {
		t.Fatal("expected stop job to interrupt the active run")
	}
	if canceledEventID != first.EventID {
		t.Fatalf("unexpected canceled event id: %q", canceledEventID)
	}
	cancelActive()

	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()
		return len(app.state.pending) == 0 && len(app.state.active) == 0
	}, "expected stop command to finish without leaving pending work in session")

	if queued, _, _ := app.enqueueJob(resume); !queued {
		t.Fatal("expected resume job to be queued")
	}
	waitForCondition(t, 2*time.Second, func() bool {
		return interruptibleCodex.CallCount() == 2
	}, "expected later message to resume interrupted thread after stop")
	waitForCondition(t, 2*time.Second, func() bool {
		app.state.mu.Lock()
		defer app.state.mu.Unlock()
		return len(app.state.pending) == 0
	}, "expected resume flow to clear pending state")

	threadIDs := interruptibleCodex.ThreadIDs()
	if len(threadIDs) != 2 {
		t.Fatalf("expected 2 codex calls, got %#v", threadIDs)
	}
	if threadIDs[0] != "" {
		t.Fatalf("expected first call to start a new thread, got %q", threadIDs[0])
	}
	if threadIDs[1] != "thread_after_interrupt" {
		t.Fatalf("expected resumed call to reuse interrupted thread, got %q", threadIDs[1])
	}

	for _, card := range sender.replyCards {
		if strings.Contains(card, "已中断") {
			t.Fatalf("stop flow should not send interrupted card, got %#v", sender.replyCards)
		}
	}
	foundStopReply := false
	for _, markdown := range sender.replyMarkdownTexts {
		if strings.Contains(markdown, "Codex session") && strings.Contains(markdown, "会保留") {
			foundStopReply = true
		}
	}
	if !foundStopReply {
		t.Fatalf("expected stop reply confirmation, got markdown=%#v", sender.replyMarkdownTexts)
	}
}
