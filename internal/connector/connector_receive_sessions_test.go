package connector

import (
	"context"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApp_OnMessageReceive_P2PMessagesReuseChatSessionKey(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event1 := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_msg_1"),
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
				MessageId:   strPtr("om_msg_2"),
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
	if job1.SessionKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected first session key: %s", job1.SessionKey)
	}
	if job2.SessionKey != "chat_id:oc_chat" {
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

func TestApp_OnMessageReceive_GroupThreadReplyToBotReplyReusesOriginalSessionKey(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"

	codex := &codexResumableCaptureStub{
		respByCall:   []string{"first reply", "second reply"},
		threadByCall: []string{"thread_1", "thread_1"},
	}
	sender := &senderStub{}
	processor := NewProcessor(codex, sender, "failed", "thinking")
	app := NewApp(cfg, processor)

	event1 := &larkim.P2MessageReceiveV1{
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
	if err := app.onMessageReceive(context.Background(), event1); err != nil {
		t.Fatalf("unexpected first event error: %v", err)
	}

	firstJob := <-app.queue
	if firstJob.SessionKey != "chat_id:oc_chat|message:om_group_root" {
		t.Fatalf("unexpected first session key: %s", firstJob.SessionKey)
	}
	if !processor.ProcessJob(context.Background(), firstJob) {
		t.Fatal("expected first job to complete")
	}

	event2 := &larkim.P2MessageReceiveV1{
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
	if err := app.onMessageReceive(context.Background(), event2); err != nil {
		t.Fatalf("unexpected second event error: %v", err)
	}

	secondJob := <-app.queue
	if secondJob.SessionKey != firstJob.SessionKey {
		t.Fatalf("expected thread reply to reuse original session, got %s vs %s", secondJob.SessionKey, firstJob.SessionKey)
	}
	if secondJob.SessionVersion != 2 {
		t.Fatalf("unexpected second session version: %d", secondJob.SessionVersion)
	}
	if !processor.ProcessJob(context.Background(), secondJob) {
		t.Fatal("expected second job to complete")
	}
	if len(codex.receivedThreadIDs) != 2 {
		t.Fatalf("expected 2 llm calls, got %d", len(codex.receivedThreadIDs))
	}
	if codex.receivedThreadIDs[1] != "thread_1" {
		t.Fatalf("expected second call to resume thread_1, got %q", codex.receivedThreadIDs[1])
	}
}
