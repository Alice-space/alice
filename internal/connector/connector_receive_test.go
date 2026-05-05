package connector

import (
	"context"
	"strings"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApp_OnMessageReceive_GroupWithoutMentionNotQueued(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_no_mention"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_no_mention"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"群里随便说说"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected queue len 0, got %d", got)
	}
}

func TestApp_OnMessageReceive_GroupMentionQueued(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)
	app.SetBotOpenID("ou_bot")

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_with_mention"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_with_mention"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 你好"}`),
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

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
}

func TestApp_OnMessageReceive_ActiveSessionSteersInsteadOfQueue(t *testing.T) {
	cfg := configForTest()
	backend := &steerCaptureStub{}
	sender := &senderStub{}
	processor := NewProcessor(
		backend,
		sender,
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)
	app := NewApp(cfg, processor)

	sessionKey := "chat_id:oc_chat"
	processor.setThreadID(sessionKey, "thread_active")
	app.state.latest[sessionKey] = 1
	app.state.pending[pendingJobKey(Job{
		SessionKey:     sessionKey,
		SessionVersion: 1,
		EventID:        "evt_active",
	})] = Job{
		ReceiveID:      "oc_chat",
		ReceiveIDType:  "chat_id",
		SessionKey:     sessionKey,
		SessionVersion: 1,
		EventID:        "evt_active",
		Text:           "first",
	}
	app.state.active[sessionKey] = activeSessionRun{
		eventID: "evt_active",
		version: 1,
		cancel:  func(error) {},
	}

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_steer"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_steer"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"please enqueue this"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected event error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected steered message not to be queued, got queue len %d", got)
	}
	if got := backend.SteerCalls(); got != 1 {
		t.Fatalf("expected one steer call, got %d", got)
	}
	if backend.lastSessionKey != sessionKey {
		t.Fatalf("unexpected steer session key: %q", backend.lastSessionKey)
	}
	if backend.lastThreadID != "thread_active" {
		t.Fatalf("unexpected steer thread id: %q", backend.lastThreadID)
	}
	if !strings.Contains(backend.lastInput, "please enqueue this") {
		t.Fatalf("expected steer input to contain message text, got %q", backend.lastInput)
	}
	if sender.replyCardCalls != 1 || !strings.Contains(sender.replyCards[0], "收到！") {
		t.Fatalf("expected steer ack card, got cards=%#v", sender.replyCards)
	}
}

func TestApp_OnMessageReceive_GroupMentionWithoutBotIDConfigNotQueued(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_mention_without_botid"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_mention_without_botid"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_other\">Tom</at> hi"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
				Mentions: []*larkim.MentionEvent{
					{
						Id: &larkim.UserId{
							OpenId: strPtr("ou_other"),
						},
					},
				},
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected queue len 0, got %d", got)
	}
}

func TestApp_OnMessageReceive_GroupPrefixModeRequiresPrefix(t *testing.T) {
	cfg := configForTest()
	cfg.TriggerMode = "prefix"
	cfg.TriggerPrefix = "!alice"
	app := NewApp(cfg, nil)

	withoutPrefix := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_prefix_miss"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_prefix_miss"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"帮我总结今天的进展"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}
	withPrefix := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_prefix_hit"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_prefix_hit"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"!alice 帮我总结今天的进展"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), withoutPrefix); err != nil {
		t.Fatalf("unexpected without-prefix error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), withPrefix); err != nil {
		t.Fatalf("unexpected with-prefix error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	job := <-app.queue
	if job.Text != "帮我总结今天的进展" {
		t.Fatalf("expected prefix removed from queued text, got %q", job.Text)
	}
}

func TestApp_OnMessageReceive_SameFeishuThreadSharesSessionKey(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event1 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_thread_1"),
				ThreadId:    strPtr("omt_thread_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"first"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}
	event2 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_thread_2"),
				ThreadId:    strPtr("omt_thread_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"second"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event1); err != nil {
		t.Fatalf("unexpected first event error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), event2); err != nil {
		t.Fatalf("unexpected second event error: %v", err)
	}

	job1 := <-app.queue
	job2 := <-app.queue
	if job1.SessionKey != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected first session key: %s", job1.SessionKey)
	}
	if job2.SessionKey != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected second session key: %s", job2.SessionKey)
	}
	if job1.ResourceScopeKey != "chat_id:oc_chat" || job2.ResourceScopeKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected resource scope keys: %q %q", job1.ResourceScopeKey, job2.ResourceScopeKey)
	}
	if job1.SessionVersion != 1 {
		t.Fatalf("unexpected first session version: %d", job1.SessionVersion)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected second session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_ThreadReplyReusesExistingRootSessionKey(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event1 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_root_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"first"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}
	event2 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_reply_1"),
				ThreadId:    strPtr("omt_thread_1"),
				RootId:      strPtr("om_root_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"second"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event1); err != nil {
		t.Fatalf("unexpected first event error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), event2); err != nil {
		t.Fatalf("unexpected second event error: %v", err)
	}

	job1 := <-app.queue
	job2 := <-app.queue
	if job1.SessionKey != "chat_id:oc_chat|message:om_root_1" {
		t.Fatalf("unexpected first session key: %s", job1.SessionKey)
	}
	if job2.SessionKey != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected second session key: %s", job2.SessionKey)
	}
	if job1.ResourceScopeKey != "chat_id:oc_chat" || job2.ResourceScopeKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected resource scope keys: %q %q", job1.ResourceScopeKey, job2.ResourceScopeKey)
	}
	if job1.SessionVersion != 1 {
		t.Fatalf("unexpected first session version: %d", job1.SessionVersion)
	}
	if job2.SessionVersion != 1 {
		t.Fatalf("unexpected second session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_ParentReplyReusesExistingRootSessionKey(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event1 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_root_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"first"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}
	event2 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_reply_1"),
				ParentId:    strPtr("om_root_1"),
				ThreadId:    strPtr("omt_thread_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"second"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event1); err != nil {
		t.Fatalf("unexpected first event error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), event2); err != nil {
		t.Fatalf("unexpected second event error: %v", err)
	}

	job1 := <-app.queue
	job2 := <-app.queue
	if job1.SessionKey != "chat_id:oc_chat|message:om_root_1" {
		t.Fatalf("unexpected first session key: %s", job1.SessionKey)
	}
	if job2.SessionKey != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected second session key: %s", job2.SessionKey)
	}
	if job2.SessionVersion != 1 {
		t.Fatalf("unexpected second session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_ExistingThreadSessionPreferredWhenRootAppears(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event1 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_thread_first"),
				ThreadId:    strPtr("omt_thread_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"first"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}
	event2 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_thread_reply"),
				ThreadId:    strPtr("omt_thread_1"),
				RootId:      strPtr("om_root_any"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"second"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event1); err != nil {
		t.Fatalf("unexpected first event error: %v", err)
	}
	if err := app.onMessageReceive(context.Background(), event2); err != nil {
		t.Fatalf("unexpected second event error: %v", err)
	}

	job1 := <-app.queue
	job2 := <-app.queue
	if job1.SessionKey != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected first session key: %s", job1.SessionKey)
	}
	if job2.SessionKey != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected second session key: %s", job2.SessionKey)
	}
	if job1.ResourceScopeKey != "chat_id:oc_chat" || job2.ResourceScopeKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected resource scope keys: %q %q", job1.ResourceScopeKey, job2.ResourceScopeKey)
	}
	if job1.SessionVersion != 1 {
		t.Fatalf("unexpected first session version: %d", job1.SessionVersion)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected second session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_BuiltinCommandNotSteeredInActiveSession(t *testing.T) {
	cfg := configForTest()
	backend := &steerCaptureStub{}
	sender := &senderStub{}
	processor := NewProcessor(
		backend,
		sender,
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)
	app := NewApp(cfg, processor)

	sessionKey := "chat_id:oc_chat"
	processor.setThreadID(sessionKey, "thread_active")
	app.state.latest[sessionKey] = 1
	app.state.pending[pendingJobKey(Job{
		SessionKey:     sessionKey,
		SessionVersion: 1,
		EventID:        "evt_active",
	})] = Job{
		ReceiveID:      "oc_chat",
		ReceiveIDType:  "chat_id",
		SessionKey:     sessionKey,
		SessionVersion: 1,
		EventID:        "evt_active",
		Text:           "first",
	}
	app.state.active[sessionKey] = activeSessionRun{
		eventID: "evt_active",
		version: 1,
		cancel:  func(error) {},
	}

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_builtin"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_builtin"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/goal"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected event error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected builtin command to be processed inline, got queue len %d", got)
	}
	if got := backend.SteerCalls(); got != 0 {
		t.Fatalf("expected no steer calls for builtin command, got %d", got)
	}
	if len(sender.replyMarkdownTexts) == 0 && len(sender.replyCards) == 0 {
		t.Fatal("expected builtin command to produce a reply")
	}
}

func TestApp_OnMessageReceive_ReadOnlyBuiltinInlineDoesNotQueue(t *testing.T) {
	cfg := configForTest()
	sender := &senderStub{}
	processor := NewProcessor(codexStub{resp: "ok"}, sender, "failed", "thinking")
	app := NewApp(cfg, processor)

	sessionKey := "chat_id:oc_chat|work:om_test"
	processor.setThreadID(sessionKey, "thread_active")
	app.state.latest[sessionKey] = 1
	app.state.active[sessionKey] = activeSessionRun{
		eventID: "evt_running",
		version: 1,
		cancel:  func(error) {},
	}

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_inline_pwd"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_test_pwd"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"/pwd"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected event error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected inline processing for /pwd, got queue len %d", got)
	}
	if len(sender.replyMarkdownTexts) == 0 {
		t.Fatal("expected /pwd to reply")
	}
}

func TestApp_OnMessageReceive_P2PThreadReplyUsesChatSessionKey(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_reply_1"),
				ThreadId:    strPtr("omt_thread_1"),
				RootId:      strPtr("om_root_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"second"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("p2p"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected event error: %v", err)
	}

	job := <-app.queue
	if job.SessionKey != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected session key: %s", job.SessionKey)
	}
	if job.SessionVersion != 1 {
		t.Fatalf("unexpected session version: %d", job.SessionVersion)
	}
}
