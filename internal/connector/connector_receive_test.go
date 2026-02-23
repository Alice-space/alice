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
	cfg.FeishuBotOpenID = "ou_bot"
	app := NewApp(cfg, nil)

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
	if job1.SessionVersion != 1 {
		t.Fatalf("unexpected first session version: %d", job1.SessionVersion)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected second session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_ThreadReplyReusesRootMessageSessionKey(t *testing.T) {
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
	if job2.SessionKey != "chat_id:oc_chat|message:om_root_1" {
		t.Fatalf("unexpected second session key: %s", job2.SessionKey)
	}
	if job1.SessionVersion != 1 {
		t.Fatalf("unexpected first session version: %d", job1.SessionVersion)
	}
	if job2.SessionVersion != 2 {
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
	if job1.SessionVersion != 1 {
		t.Fatalf("unexpected first session version: %d", job1.SessionVersion)
	}
	if job2.SessionVersion != 2 {
		t.Fatalf("unexpected second session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_NonThreadMessagesUseNewSessionKey(t *testing.T) {
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
	if job1.SessionKey != "chat_id:oc_chat|message:om_msg_1" {
		t.Fatalf("unexpected first session key: %s", job1.SessionKey)
	}
	if job2.SessionKey != "chat_id:oc_chat|message:om_msg_2" {
		t.Fatalf("unexpected second session key: %s", job2.SessionKey)
	}
	if job1.SessionVersion != 1 {
		t.Fatalf("unexpected first session version: %d", job1.SessionVersion)
	}
	if job2.SessionVersion != 1 {
		t.Fatalf("unexpected second session version: %d", job2.SessionVersion)
	}
}

func TestApp_OnMessageReceive_GroupMediaWithoutMentionCachedNotQueued(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	app := NewApp(cfg, nil)

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_media_no_mention"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId: strPtr("ou_user_1"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_media"),
				MessageType: strPtr("image"),
				Content:     strPtr(`{"image_key":"img_123"}`),
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

	windowKey := buildMediaWindowKey("oc_chat", "open_id:ou_user_1")
	app.mu.Lock()
	entries := app.mediaWindow[windowKey]
	app.mu.Unlock()
	if len(entries) != 1 {
		t.Fatalf("expected 1 cached media entry, got %d", len(entries))
	}
	if entries[0].SourceMessageID != "om_media" {
		t.Fatalf("unexpected cached source message id: %s", entries[0].SourceMessageID)
	}
	if !strings.Contains(entries[0].Speaker, "ou_user_1") {
		t.Fatalf("expected cached speaker identity, got %q", entries[0].Speaker)
	}
}

func TestApp_OnMessageReceive_GroupMentionMergesRecentMediaWindow(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	app := NewApp(cfg, nil)

	mediaEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_media"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId: strPtr("ou_user_1"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_media"),
				MessageType: strPtr("image"),
				Content:     strPtr(`{"image_key":"img_123"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}
	if err := app.onMessageReceive(context.Background(), mediaEvent); err != nil {
		t.Fatalf("unexpected media event error: %v", err)
	}

	mentionEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_mention"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId: strPtr("ou_user_1"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_mention"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 帮我处理刚发的图片"}`),
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
	if err := app.onMessageReceive(context.Background(), mentionEvent); err != nil {
		t.Fatalf("unexpected mention event error: %v", err)
	}

	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	job := <-app.queue
	if len(job.Attachments) != 1 {
		t.Fatalf("expected merged attachments count 1, got %d", len(job.Attachments))
	}
	if job.Attachments[0].SourceMessageID != "om_media" {
		t.Fatalf("unexpected merged attachment source message id: %s", job.Attachments[0].SourceMessageID)
	}
	if !strings.Contains(job.Text, "已自动合并你在过去5分钟发送的1条多媒体消息") {
		t.Fatalf("expected merge hint in text, got: %q", job.Text)
	}

	windowKey := buildMediaWindowKey("oc_chat", "open_id:ou_user_1")
	app.mu.Lock()
	remaining := len(app.mediaWindow[windowKey])
	app.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected media window consumed after merge, remaining=%d", remaining)
	}
}

func TestApp_OnMessageReceive_MentionOnlyBuildsSyntheticJobAndMergesMedia(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	app := NewApp(cfg, nil)

	mediaEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_media_synth"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId: strPtr("ou_user_1"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_media_synth"),
				MessageType: strPtr("file"),
				Content:     strPtr(`{"file_key":"file_123","file_name":"spec.txt"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}
	if err := app.onMessageReceive(context.Background(), mediaEvent); err != nil {
		t.Fatalf("unexpected media event error: %v", err)
	}

	mentionOnlyEvent := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_mention_only"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId: strPtr("ou_user_1"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_mention_only"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at>"}`),
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
	if err := app.onMessageReceive(context.Background(), mentionOnlyEvent); err != nil {
		t.Fatalf("unexpected mention-only event error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	job := <-app.queue
	if len(job.Attachments) != 1 {
		t.Fatalf("expected merged attachments count 1, got %d", len(job.Attachments))
	}
	if job.Attachments[0].SourceMessageID != "om_media_synth" {
		t.Fatalf("unexpected merged synthetic attachment source message id: %s", job.Attachments[0].SourceMessageID)
	}
	if !strings.Contains(job.Text, "用户@了你，请结合其最近发送的消息继续处理。") {
		t.Fatalf("expected synthetic mention hint in text, got: %q", job.Text)
	}
}
