package connector

import (
	"errors"
	"testing"

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
