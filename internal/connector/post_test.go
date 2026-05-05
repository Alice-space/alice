package connector

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestBuildJob_PostMessageWithImageAndMention(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_post_1"),
				MessageType: strPtr("post"),
				Content:     strPtr(`{"title":"","content":[[{"tag":"img","image_key":"img_post_123"}],[{"tag":"at","user_id":"@_user_1","user_name":"Alice"},{"tag":"text","text":" 这个图片是什么意思"}]]}`),
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
	if job.MessageType != "post" {
		t.Fatalf("unexpected message type: %s", job.MessageType)
	}
	if job.Text != "这个图片是什么意思" {
		t.Fatalf("unexpected text: %q", job.Text)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].ImageKey != "img_post_123" {
		t.Fatalf("unexpected image key: %s", job.Attachments[0].ImageKey)
	}
	if job.Attachments[0].SourceMessageID != "om_post_1" {
		t.Fatalf("unexpected source message id: %s", job.Attachments[0].SourceMessageID)
	}
}

func TestBuildJob_PostMessagePrefersEventMentionOverContentPlaceholder(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_post_placeholder"),
				MessageType: strPtr("post"),
				Content:     strPtr(`{"title":"","content":[[{"tag":"at","user_id":"@_user_1","user_name":"Alice"},{"tag":"text","text":" 帮我看一下"}]]}`),
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
	if len(job.MentionedUsers) != 1 {
		t.Fatalf("expected 1 mentioned user, got %d", len(job.MentionedUsers))
	}
	if job.MentionedUsers[0].OpenID != "ou_bot" {
		t.Fatalf("expected event mention identity to win, got %+v", job.MentionedUsers[0])
	}
	if job.MentionedUsers[0].UserID != "" {
		t.Fatalf("unexpected placeholder user id retained: %+v", job.MentionedUsers[0])
	}
}

func TestBuildJob_PostMessageContentFallbackKeepsUserName(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_post_fallback"),
				MessageType: strPtr("post"),
				Content:     strPtr(`{"title":"","content":[[{"tag":"at","user_id":"ou_alice","user_name":"Alice"},{"tag":"text","text":" 帮我看一下"}]]}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(job.MentionedUsers) != 1 {
		t.Fatalf("expected 1 mentioned user, got %d", len(job.MentionedUsers))
	}
	if job.MentionedUsers[0].OpenID != "ou_alice" || job.MentionedUsers[0].Name != "Alice" {
		t.Fatalf("unexpected post fallback mention: %+v", job.MentionedUsers[0])
	}
}

func TestBuildJob_PostImageOnlyUsesFallbackText(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_post_2"),
				MessageType: strPtr("post"),
				Content:     strPtr(`{"title":"","content":[[{"tag":"img","image_key":"img_post_only"}]]}`),
				ChatId:      strPtr("oc_chat"),
			},
		},
	}

	job, err := BuildJob(event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Text != "用户发送了一条富文本消息。" {
		t.Fatalf("unexpected text: %q", job.Text)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].ImageKey != "img_post_only" {
		t.Fatalf("unexpected image key: %s", job.Attachments[0].ImageKey)
	}
}

func TestShouldProcessIncomingMessage_GroupPostMentionUsesContentFallback(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				ChatType:    strPtr("group"),
				MessageType: strPtr("post"),
				Content:     strPtr(`{"title":"","content":[[{"tag":"at","user_id":"ou_bot","user_name":"Alice"},{"tag":"text","text":" 帮我看图"}]]}`),
			},
		},
	}

	if !shouldProcessIncomingMessage(event, "at", "", "ou_bot", "") {
		t.Fatal("post message with bot mention in content should be processed")
	}
}
