package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestBuildJob_TextMessage(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_1"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_123"),
				ParentId:    strPtr("om_parent"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_x\">Tom</at> 你好"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.ReceiveID != "oc_chat" || job.ReceiveIDType != "chat_id" {
		t.Fatalf("unexpected receive target: %+v", job)
	}
	if job.Text != "你好" {
		t.Fatalf("unexpected text: %q", job.Text)
	}
	if job.SourceMessageID != "om_123" {
		t.Fatalf("unexpected source message id: %s", job.SourceMessageID)
	}
	if job.ReplyParentMessageID != "om_parent" {
		t.Fatalf("unexpected parent message id: %s", job.ReplyParentMessageID)
	}
	if job.EventID != "evt_1" {
		t.Fatalf("unexpected event id: %s", job.EventID)
	}
	if job.SessionKey != "chat_id:oc_chat|message:om_123" {
		t.Fatalf("unexpected session key: %s", job.SessionKey)
	}
}

func TestBuildJob_SessionKeyPrefersThreadID(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_1"),
				ThreadId:    strPtr("omt_thread_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"hi"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.SessionKey != "chat_id:oc_chat|thread:omt_thread_1" {
		t.Fatalf("unexpected session key: %s", job.SessionKey)
	}
}

func TestBuildJob_SessionKeyFallsBackToRootID(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_2"),
				RootId:      strPtr("om_root_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"hi"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.SessionKey != "chat_id:oc_chat|thread:om_root_1" {
		t.Fatalf("unexpected session key: %s", job.SessionKey)
	}
}

func TestBuildJob_IgnoreNonText(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageType: strPtr("interactive"),
				Content:     strPtr(`{"type":"template","data":{}}`),
			},
		},
	}

	_, err := BuildJob(event)
	if !errors.Is(err, ErrIgnoreMessage) {
		t.Fatalf("expected ErrIgnoreMessage, got: %v", err)
	}
}

func TestBuildJob_ImageMessage(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_img"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_img"),
				MessageType: strPtr("image"),
				Content:     strPtr(`{"image_key":"img_123"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.MessageType != "image" {
		t.Fatalf("unexpected message type: %s", job.MessageType)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].ImageKey != "img_123" {
		t.Fatalf("unexpected image key: %s", job.Attachments[0].ImageKey)
	}
	if job.Attachments[0].SourceMessageID != "om_img" {
		t.Fatalf("unexpected image source message id: %s", job.Attachments[0].SourceMessageID)
	}
}

func TestBuildJob_StickerMessage(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_stk"),
				MessageType: strPtr("sticker"),
				Content:     strPtr(`{"file_key":"file_sticker_123"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.MessageType != "sticker" {
		t.Fatalf("unexpected message type: %s", job.MessageType)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].FileKey != "file_sticker_123" {
		t.Fatalf("unexpected sticker file key: %s", job.Attachments[0].FileKey)
	}
	if job.Attachments[0].SourceMessageID != "om_stk" {
		t.Fatalf("unexpected sticker source message id: %s", job.Attachments[0].SourceMessageID)
	}
}

func TestBuildJob_AudioMessage(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_audio"),
				MessageType: strPtr("audio"),
				Content:     strPtr(`{"file_key":"file_audio_123"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.MessageType != "audio" {
		t.Fatalf("unexpected message type: %s", job.MessageType)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].FileKey != "file_audio_123" {
		t.Fatalf("unexpected audio file key: %s", job.Attachments[0].FileKey)
	}
	if job.Attachments[0].SourceMessageID != "om_audio" {
		t.Fatalf("unexpected audio source message id: %s", job.Attachments[0].SourceMessageID)
	}
}

func TestBuildJob_FileMessage(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_file"),
				MessageType: strPtr("file"),
				Content:     strPtr(`{"file_key":"file_123","file_name":"report.pdf"}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.MessageType != "file" {
		t.Fatalf("unexpected message type: %s", job.MessageType)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].FileKey != "file_123" {
		t.Fatalf("unexpected file key: %s", job.Attachments[0].FileKey)
	}
	if job.Attachments[0].FileName != "report.pdf" {
		t.Fatalf("unexpected file name: %s", job.Attachments[0].FileName)
	}
	if job.Attachments[0].SourceMessageID != "om_file" {
		t.Fatalf("unexpected file source message id: %s", job.Attachments[0].SourceMessageID)
	}
}

func TestShouldProcessIncomingMessage_GroupRequiresMention(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType: strPtr("group"),
				Content:  strPtr(`{"text":"大家好"}`),
			},
		},
	}

	if shouldProcessIncomingMessage(event, "", "") {
		t.Fatal("group message without mention should be ignored")
	}
}

func TestShouldProcessIncomingMessage_GroupMentionWithBotOpenID(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType: strPtr("group"),
				Content:  strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 你好"}`),
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

	if !shouldProcessIncomingMessage(event, "ou_bot", "") {
		t.Fatal("group message that mentions bot open_id should be processed")
	}
}

func TestShouldProcessIncomingMessage_GroupMentionWithoutBotIDConfigIgnored(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType: strPtr("group"),
				Content:  strPtr(`{"text":"<at user_id=\"ou_other\">Tom</at> 你好"}`),
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

	if shouldProcessIncomingMessage(event, "", "") {
		t.Fatal("group message should be ignored when bot IDs are not configured")
	}
}

func TestShouldProcessIncomingMessage_PrivateChatNoMention(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType: strPtr("p2p"),
				Content:  strPtr(`{"text":"你好"}`),
			},
		},
	}

	if !shouldProcessIncomingMessage(event, "", "") {
		t.Fatal("p2p message should be processed without mention")
	}
}

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
	if !strings.Contains(job.Text, "用户@了你，请结合其最近发送的多媒体继续处理。") {
		t.Fatalf("expected synthetic mention hint in text, got: %q", job.Text)
	}
}

func TestProcessor_ReplyMessageFlow_OnFailureSendsAckThenFallback(t *testing.T) {
	fakeCodex := codexStub{err: errors.New("boom")}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyTextCalls != 2 {
		t.Fatalf("expected 2 reply text calls, got %d", sender.replyTextCalls)
	}
	if len(sender.replyTexts) != 2 {
		t.Fatalf("unexpected reply text history: %#v", sender.replyTexts)
	}
	if sender.replyTexts[0] != "收到！" {
		t.Fatalf("first reply should be ack, got %q", sender.replyTexts[0])
	}
	if sender.replyTexts[1] != "Codex 暂时不可用，请稍后重试。" {
		t.Fatalf("second reply should be failure message, got %q", sender.replyTexts[1])
	}
}

func TestProcessor_SendsAgentMessagesAsRichTextMarkdown(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:          "最终答复",
		agentMessages: []string{"阶段提示", "最终答复"},
	}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyTextCalls != 1 {
		t.Fatalf("expected only ack text reply, got %d", sender.replyTextCalls)
	}
	if len(sender.replyTexts) != 1 || sender.replyTexts[0] != "收到！" {
		t.Fatalf("unexpected ack reply text history: %#v", sender.replyTexts)
	}
	if sender.replyRichMarkdownCalls != 2 {
		t.Fatalf("expected 2 markdown rich replies, got %d", sender.replyRichMarkdownCalls)
	}
	expectedMarkdown := []string{"阶段提示", "最终答复"}
	if len(sender.replyMarkdownTexts) != len(expectedMarkdown) {
		t.Fatalf("unexpected markdown rich reply history: %#v", sender.replyMarkdownTexts)
	}
	for i := range expectedMarkdown {
		if sender.replyMarkdownTexts[i] != expectedMarkdown[i] {
			t.Fatalf("unexpected markdown rich reply at %d: want %q got %q", i, expectedMarkdown[i], sender.replyMarkdownTexts[i])
		}
	}
}

func TestProcessor_FileChangeEventUsesRichTextReply(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:          "最终答复",
		agentMessages: []string{"[file_change] internal/connector/processor.go已更改，+23-34"},
	}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyRichCalls != 1 {
		t.Fatalf("expected 1 rich text reply for file change, got %d", sender.replyRichCalls)
	}
	if len(sender.replyRichLines) != 1 || len(sender.replyRichLines[0]) != 1 {
		t.Fatalf("unexpected rich text payload: %#v", sender.replyRichLines)
	}
	if sender.replyRichLines[0][0] != "internal/connector/processor.go已更改，+23-34" {
		t.Fatalf("unexpected rich text line: %#v", sender.replyRichLines[0])
	}
	if sender.replyTextCalls != 2 {
		t.Fatalf("expected ack + final reply text, got %d", sender.replyTextCalls)
	}
}

func TestProcessor_DeduplicatesFinalReplyWhenAlreadySentViaAgentMessage(t *testing.T) {
	fakeCodex := codexStub{resp: "final answer"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyTextCalls != 2 {
		t.Fatalf("expected ack + final reply, got %d", sender.replyTextCalls)
	}
	if len(sender.replyTexts) != 2 {
		t.Fatalf("unexpected reply text history: %#v", sender.replyTexts)
	}
	if sender.replyTexts[0] != "收到！" || sender.replyTexts[1] != "final answer" {
		t.Fatalf("unexpected reply text history: %#v", sender.replyTexts)
	}
}

func TestProcessor_SkipsDuplicateAgentMessages(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:          "最终答复",
		agentMessages: []string{"阶段提示", "阶段提示", "最终答复"},
	}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyTextCalls != 1 {
		t.Fatalf("expected only ack text reply, got %d", sender.replyTextCalls)
	}
	if len(sender.replyTexts) != 1 || sender.replyTexts[0] != "收到！" {
		t.Fatalf("unexpected reply text history: %#v", sender.replyTexts)
	}

	expected := []string{"阶段提示", "最终答复"}
	if len(sender.replyMarkdownTexts) != len(expected) {
		t.Fatalf("unexpected markdown rich reply history: %#v", sender.replyMarkdownTexts)
	}
	for i := range expected {
		if sender.replyMarkdownTexts[i] != expected[i] {
			t.Fatalf("unexpected markdown rich reply at %d: want %q got %q", i, expected[i], sender.replyMarkdownTexts[i])
		}
	}
}

func TestProcessor_NoSourceMessageUsesSendText(t *testing.T) {
	fakeCodex := codexStub{resp: "final answer"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
	})

	if sender.sendCalls != 1 {
		t.Fatalf("expected 1 send text call, got %d", sender.sendCalls)
	}
	if sender.lastSendText != "final answer" {
		t.Fatalf("unexpected send text content: %s", sender.lastSendText)
	}
}

func TestProcessor_ResolvesAttachmentsAndPassesLocalPathToCodex(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{
		downloadPathByKey: map[string]string{
			"img_123": "/tmp/alice/image.png",
		},
	}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		MessageType:     "image",
		Text:            "用户发送了一张图片。",
		Attachments: []Attachment{
			{
				SourceMessageID: "om_media",
				Kind:            "image",
				ImageKey:        "img_123",
			},
		},
	})

	if sender.downloadCalls != 1 {
		t.Fatalf("expected 1 attachment download, got %d", sender.downloadCalls)
	}
	if len(sender.downloadSourceMessageIDs) != 1 || sender.downloadSourceMessageIDs[0] != "om_media" {
		t.Fatalf("expected attachment download to use attachment source message id, got %#v", sender.downloadSourceMessageIDs)
	}
	if !strings.Contains(fakeCodex.lastInput, "本地路径：/tmp/alice/image.png") {
		t.Fatalf("codex input should include downloaded local path, got: %s", fakeCodex.lastInput)
	}
	if sender.replyTextCalls != 2 {
		t.Fatalf("expected ack + final reply, got %d", sender.replyTextCalls)
	}
}

func TestProcessor_CanceledReplyMarksInterruptedInsteadOfFailure(t *testing.T) {
	fakeCodex := codexStub{err: context.Canceled}
	sender := &senderStub{}
	memory := &memoryStub{prompt: "记忆上下文 + 用户消息"}

	processor := NewProcessorWithMemory(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		memory,
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyTextCalls != 2 {
		t.Fatalf("expected ack + interrupted message, got %d", sender.replyTextCalls)
	}
	if len(sender.replyTexts) != 2 {
		t.Fatalf("unexpected reply text history: %#v", sender.replyTexts)
	}
	if sender.replyTexts[0] != "收到！" {
		t.Fatalf("first reply should be ack, got %q", sender.replyTexts[0])
	}
	if !strings.Contains(sender.replyTexts[1], "已中断") {
		t.Fatalf("second reply should be interrupted message, got %q", sender.replyTexts[1])
	}
	if strings.Contains(sender.replyTexts[1], "Codex 暂时不可用，请稍后重试") {
		t.Fatalf("interrupted reply should not include failure message: %q", sender.replyTexts[1])
	}
	if memory.saveCalls != 0 {
		t.Fatalf("canceled job should not be saved to memory, got %d", memory.saveCalls)
	}
}

func TestProcessor_CanceledNonReplySkipsSendingAndMemory(t *testing.T) {
	fakeCodex := codexStub{err: context.Canceled}
	sender := &senderStub{}
	memory := &memoryStub{prompt: "记忆上下文 + 用户消息"}

	processor := NewProcessorWithMemory(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		memory,
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
	})

	if sender.sendCalls != 0 {
		t.Fatalf("expected no send text calls, got %d", sender.sendCalls)
	}
	if memory.saveCalls != 0 {
		t.Fatalf("canceled job should not be saved to memory, got %d", memory.saveCalls)
	}
}

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

func TestApp_InterruptedJobKeepsPendingForRestart(t *testing.T) {
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
		return ok
	}, "interrupted job should remain pending")

	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	if got := len(restored.queue); got != 1 {
		t.Fatalf("expected interrupted job to be restored, got queue len %d", got)
	}
	recovered := <-restored.queue
	if recovered.EventID != "evt_interrupt" {
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

func TestApp_SelfUpdateInterruptedJobDoesNotResumeAfterRestart(t *testing.T) {
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
	}, "self-update command should be removed from pending after shutdown cancellation")

	if err := app.FlushRuntimeState(); err != nil {
		t.Fatalf("flush runtime state failed: %v", err)
	}

	restored := NewApp(cfg, nil)
	if err := restored.LoadRuntimeState(statePath); err != nil {
		t.Fatalf("load persisted runtime state failed: %v", err)
	}
	if got := len(restored.queue); got != 0 {
		t.Fatalf("expected no restored self-update job, got queue len %d", got)
	}
}

func TestProcessor_UsesMemoryPromptAndSavesInteraction(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{}
	memory := &memoryStub{prompt: "记忆上下文 + 用户消息"}

	processor := NewProcessorWithMemory(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		memory,
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
	})

	if memory.buildCalls != 1 || memory.lastBuildInput != "hello" {
		t.Fatalf("unexpected memory build call: %+v", memory)
	}
	if fakeCodex.lastInput != "记忆上下文 + 用户消息" {
		t.Fatalf("codex should receive memory prompt, got: %s", fakeCodex.lastInput)
	}
	if memory.saveCalls != 1 {
		t.Fatalf("expected 1 memory save, got %d", memory.saveCalls)
	}
	if memory.lastSaveUser != "hello" {
		t.Fatalf("unexpected saved user text: %s", memory.lastSaveUser)
	}
	if memory.lastSaveReply != "final answer" {
		t.Fatalf("unexpected saved reply: %s", memory.lastSaveReply)
	}
	if memory.lastSaveFailed {
		t.Fatalf("save flag should be success")
	}
}

func TestProcessor_AttachesReplyParentMessageContext(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{
		messageTextByID: map[string]string{
			"om_parent": "上一条消息",
		},
	}
	processor := NewProcessor(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src",
		ReplyParentMessageID: "om_parent",
		Text:                 "继续",
	})

	if sender.getMessageTextCalls != 1 {
		t.Fatalf("expected 1 get message text call, got %d", sender.getMessageTextCalls)
	}
	if !strings.Contains(fakeCodex.lastInput, "被回复消息：\n上一条消息") {
		t.Fatalf("expected parent context in codex input, got: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "用户当前回复：\n继续") {
		t.Fatalf("expected current reply in codex input, got: %s", fakeCodex.lastInput)
	}
}

func TestProcessor_ReplyParentContextFetchFailureFallsBackToUserText(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{
		getMessageTextErr: errors.New("boom"),
	}
	processor := NewProcessor(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src",
		ReplyParentMessageID: "om_parent",
		Text:                 "继续",
	})

	if fakeCodex.lastInput != "继续" {
		t.Fatalf("expected fallback to user text, got: %s", fakeCodex.lastInput)
	}
}

func TestProcessor_ResumesCodexThreadWithinSameSession(t *testing.T) {
	fakeCodex := &codexResumableCaptureStub{
		respByCall:   []string{"B", "D"},
		threadByCall: []string{"thread_1", "thread_1"},
	}
	sender := &senderStub{
		messageTextByID: map[string]string{
			"om_parent": "上一条消息",
		},
	}
	processor := NewProcessor(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "A",
		SessionKey:    "chat_id:oc_chat",
	})

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src",
		ReplyParentMessageID: "om_parent",
		Text:                 "C",
		SessionKey:           "chat_id:oc_chat",
	})

	if len(fakeCodex.receivedThreadIDs) != 2 {
		t.Fatalf("expected 2 codex calls, got %d", len(fakeCodex.receivedThreadIDs))
	}
	if fakeCodex.receivedThreadIDs[0] != "" {
		t.Fatalf("first call should start new thread, got %q", fakeCodex.receivedThreadIDs[0])
	}
	if fakeCodex.receivedThreadIDs[1] != "thread_1" {
		t.Fatalf("second call should resume thread_1, got %q", fakeCodex.receivedThreadIDs[1])
	}
	if len(fakeCodex.receivedInputs) != 2 {
		t.Fatalf("expected 2 codex inputs, got %d", len(fakeCodex.receivedInputs))
	}
	if fakeCodex.receivedInputs[1] != "C" {
		t.Fatalf("second input should be direct follow-up text C, got %q", fakeCodex.receivedInputs[1])
	}
	if sender.getMessageTextCalls != 0 {
		t.Fatalf("resume mode should not fetch parent text, got %d", sender.getMessageTextCalls)
	}
}

func TestProcessor_ResumeSkipsMemoryBuildPrompt(t *testing.T) {
	fakeCodex := &codexResumableCaptureStub{
		respByCall:   []string{"B", "D"},
		threadByCall: []string{"thread_1", "thread_1"},
	}
	sender := &senderStub{}
	memory := &memoryStub{prompt: "记忆上下文 + 用户消息"}
	processor := NewProcessorWithMemory(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
		memory,
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "A",
		SessionKey:    "chat_id:oc_chat",
	})
	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "C",
		SessionKey:    "chat_id:oc_chat",
	})

	if memory.buildCalls != 1 {
		t.Fatalf("expected memory build only once before resume, got %d", memory.buildCalls)
	}
	if len(fakeCodex.receivedInputs) != 2 {
		t.Fatalf("expected 2 codex calls, got %d", len(fakeCodex.receivedInputs))
	}
	if fakeCodex.receivedInputs[0] != "记忆上下文 + 用户消息" {
		t.Fatalf("first call should use memory prompt, got %q", fakeCodex.receivedInputs[0])
	}
	if fakeCodex.receivedInputs[1] != "C" {
		t.Fatalf("resume call should use raw user text, got %q", fakeCodex.receivedInputs[1])
	}
}
