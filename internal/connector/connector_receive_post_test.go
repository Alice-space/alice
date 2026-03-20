package connector

import (
	"context"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApp_OnMessageReceive_GroupChatSceneQueuesPostWithoutMention(t *testing.T) {
	cfg := configForGroupScenesTest()
	app := NewApp(cfg, nil)

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_post_media"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					OpenId: strPtr("ou_user_1"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_post_media"),
				MessageType: strPtr("post"),
				Content:     strPtr(`{"title":"","content":[[{"tag":"img","image_key":"img_post_123"}],[{"tag":"text","text":"刚发了一张图"}]]}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}
	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected post event error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
	}
	job := <-app.queue
	if job.Scene != jobSceneChat {
		t.Fatalf("unexpected scene: %q", job.Scene)
	}
	if job.ResponseMode != jobResponseModeSend {
		t.Fatalf("unexpected response mode: %q", job.ResponseMode)
	}
	if len(job.Attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(job.Attachments))
	}
	if job.Attachments[0].SourceMessageID != "om_post_media" {
		t.Fatalf("unexpected attachment source message id: %s", job.Attachments[0].SourceMessageID)
	}
	if job.SessionKey != "chat_id:oc_chat|scene:chat" {
		t.Fatalf("unexpected session key: %q", job.SessionKey)
	}
	if job.MemoryScopeKey != "chat_id:oc_chat|scene:chat" {
		t.Fatalf("unexpected memory scope key: %q", job.MemoryScopeKey)
	}
	if job.LLMModel != "gpt-5.4-mini" {
		t.Fatalf("unexpected llm model: %q", job.LLMModel)
	}
	if job.LLMReasoningEffort != "low" {
		t.Fatalf("unexpected llm reasoning effort: %q", job.LLMReasoningEffort)
	}
	if job.LLMPersonality != "friendly" {
		t.Fatalf("unexpected llm personality: %q", job.LLMPersonality)
	}
}
