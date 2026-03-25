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
				ChatType:    strPtr("p2p"),
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
	if job.SessionKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected session key: %s", job.SessionKey)
	}
}

func TestBuildJob_ExtractsSenderAndMentionedUsers(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId:  strPtr("ou_bob"),
					UserId:  strPtr("u_bob"),
					UnionId: strPtr("on_bob"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_identity_1"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"@_user_1 这是xxx"}`),
				ChatId:      strPtr("oc_chat"),
				Mentions: []*larkim.MentionEvent{
					{
						Key:  strPtr("@_user_1"),
						Name: strPtr("Carlo"),
						Id: &larkim.UserId{
							OpenId: strPtr("ou_carlo"),
							UserId: strPtr("u_carlo"),
						},
					},
				},
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.SenderOpenID != "ou_bob" || job.SenderUserID != "u_bob" || job.SenderUnionID != "on_bob" {
		t.Fatalf("unexpected sender ids: open=%q user=%q union=%q", job.SenderOpenID, job.SenderUserID, job.SenderUnionID)
	}
	if len(job.MentionedUsers) != 1 {
		t.Fatalf("expected 1 mentioned user, got %d", len(job.MentionedUsers))
	}
	if job.MentionedUsers[0].Name != "Carlo" {
		t.Fatalf("unexpected mentioned user name: %q", job.MentionedUsers[0].Name)
	}
	if job.MentionedUsers[0].OpenID != "ou_carlo" || job.MentionedUsers[0].UserID != "u_carlo" {
		t.Fatalf("unexpected mentioned user ids: %+v", job.MentionedUsers[0])
	}
	if job.Text != "@Carlo 这是xxx" {
		t.Fatalf("unexpected text with mention display name: %q", job.Text)
	}
}

func TestBuildJob_TextMessageStripsMentionKey(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_plain_mention"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"@_user_1 帮我重启连接器"}`),
				ChatId:      strPtr("oc_chat"),
				Mentions: []*larkim.MentionEvent{
					{
						Key: strPtr("@_user_1"),
						Id: &larkim.UserId{
							OpenId: strPtr("ou_bot"),
						},
					},
				},
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Text != "帮我重启连接器" {
		t.Fatalf("unexpected text: %q", job.Text)
	}
}

func TestBuildJob_TextMessageMentionOnlyWithKeyIgnored(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_plain_mention_only"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"@_user_1"}`),
				ChatId:      strPtr("oc_chat"),
				Mentions: []*larkim.MentionEvent{
					{
						Key: strPtr("@_user_1"),
						Id: &larkim.UserId{
							OpenId: strPtr("ou_bot"),
						},
					},
				},
			},
		},
	}

	_, err := BuildJob(event)
	if !errors.Is(err, ErrIgnoreMessage) {
		t.Fatalf("expected ErrIgnoreMessage, got: %v", err)
	}
}

func TestBuildJob_TextMessagePreservesMentionPosition(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_plain_mention_middle"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"我是谁？@_user_1 又是谁？"}`),
				ChatId:      strPtr("oc_chat"),
				Mentions: []*larkim.MentionEvent{
					{
						Key:  strPtr("@_user_1"),
						Name: strPtr("Carlo"),
						Id: &larkim.UserId{
							OpenId: strPtr("ou_carlo"),
						},
					},
				},
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Text != "我是谁？@Carlo 又是谁？" {
		t.Fatalf("unexpected text: %q", job.Text)
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
				ChatType:    strPtr("group"),
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
				ChatType:    strPtr("group"),
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

func TestBuildJob_ImageMessageWithTextAndMentions(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_img_text"),
				MessageType: strPtr("image"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> @_user_1 看这张图","image_key":"img_456"}`),
				ChatId:      strPtr("oc_chat"),
				Mentions: []*larkim.MentionEvent{
					{
						Key: strPtr("@_user_1"),
						Id: &larkim.UserId{
							OpenId: strPtr("ou_bot"),
						},
					},
				},
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Text != "看这张图" {
		t.Fatalf("unexpected text: %q", job.Text)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].ImageKey != "img_456" {
		t.Fatalf("unexpected image key: %s", job.Attachments[0].ImageKey)
	}
	if job.Attachments[0].SourceMessageID != "om_img_text" {
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
	if job.Text != "用户发送了一个文件：report.pdf" {
		t.Fatalf("unexpected text: %q", job.Text)
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

func TestBuildJob_FileMessageWithTextAndMentions(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_file_text"),
				MessageType: strPtr("file"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> @_user_2 请处理这个文件","file_key":"file_456","file_name":"design.docx"}`),
				ChatId:      strPtr("oc_chat"),
				Mentions: []*larkim.MentionEvent{
					{
						Key: strPtr("@_user_2"),
						Id: &larkim.UserId{
							OpenId: strPtr("ou_bot"),
						},
					},
				},
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Text != "请处理这个文件" {
		t.Fatalf("unexpected text: %q", job.Text)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].FileKey != "file_456" {
		t.Fatalf("unexpected file key: %s", job.Attachments[0].FileKey)
	}
	if job.Attachments[0].FileName != "design.docx" {
		t.Fatalf("unexpected file name: %s", job.Attachments[0].FileName)
	}
	if job.Attachments[0].SourceMessageID != "om_file_text" {
		t.Fatalf("unexpected file source message id: %s", job.Attachments[0].SourceMessageID)
	}
}
