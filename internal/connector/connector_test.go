package connector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	if job.EventID != "evt_1" {
		t.Fatalf("unexpected event id: %s", job.EventID)
	}
}

func TestBuildJob_IgnoreNonText(t *testing.T) {
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageType: strPtr("image"),
				Content:     strPtr(`{"image_key":"abc"}`),
			},
		},
	}

	_, err := BuildJob(event)
	if !errors.Is(err, ErrIgnoreMessage) {
		t.Fatalf("expected ErrIgnoreMessage, got: %v", err)
	}
}

func TestProcessor_UsesReplyCardAndPatchOnFailure(t *testing.T) {
	fakeCodex := codexStub{err: errors.New("boom")}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyCardCalls != 1 {
		t.Fatalf("expected 1 reply card call, got %d", sender.replyCardCalls)
	}
	if sender.patchCardCalls != 1 {
		t.Fatalf("expected 1 patch call, got %d", sender.patchCardCalls)
	}
	if !strings.Contains(sender.lastPatchedCard, "Codex 暂时不可用，请稍后重试") {
		t.Fatalf("final card missing fallback message: %s", sender.lastPatchedCard)
	}
}

func TestProcessor_SyncsThinkingWhenStreaming(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:      "final answer",
		reasoning: []string{"分析第一步", "分析第二步"},
	}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyCardCalls != 1 {
		t.Fatalf("expected 1 reply card call, got %d", sender.replyCardCalls)
	}
	if sender.patchCardCalls < 2 {
		t.Fatalf("expected at least 2 patch calls, got %d", sender.patchCardCalls)
	}
	if !strings.Contains(sender.lastPatchedCard, "分析第二步") {
		t.Fatalf("final card missing synced reasoning: %s", sender.lastPatchedCard)
	}
	if !strings.Contains(sender.lastPatchedCard, "final answer") {
		t.Fatalf("final card missing final answer: %s", sender.lastPatchedCard)
	}
}

func TestProcessor_FallbackToReplyTextWhenPatchFails(t *testing.T) {
	fakeCodex := codexStub{resp: "final answer"}
	sender := &senderStub{patchCardErr: errors.New("patch failed")}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.patchCardCalls != 1 {
		t.Fatalf("expected 1 patch call, got %d", sender.patchCardCalls)
	}
	if sender.replyTextCalls != 1 {
		t.Fatalf("expected 1 fallback reply text call, got %d", sender.replyTextCalls)
	}
	if sender.lastReplyText != "final answer" {
		t.Fatalf("unexpected fallback text: %s", sender.lastReplyText)
	}
}

func TestProcessor_FinalCardRemovesThinkingMessage(t *testing.T) {
	fakeCodex := codexStub{resp: "final answer"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.patchCardCalls != 1 {
		t.Fatalf("expected 1 patch call, got %d", sender.patchCardCalls)
	}
	if strings.Contains(sender.lastPatchedCard, "正在思考中...") {
		t.Fatalf("final card should not keep thinking placeholder: %s", sender.lastPatchedCard)
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

func TestBuildProgressCardContent_UsesCardSchemaV2BodyElements(t *testing.T) {
	content := buildProgressCardContent("问题", "思考", "答案", false)

	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("unmarshal card content failed: %v", err)
	}

	if payload["schema"] != "2.0" {
		t.Fatalf("expected schema 2.0, got %#v", payload["schema"])
	}
	if _, exists := payload["elements"]; exists {
		t.Fatalf("schema 2.0 card should not use top-level elements")
	}

	body, ok := payload["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body object, got %#v", payload["body"])
	}
	elements, ok := body["elements"].([]any)
	if !ok || len(elements) == 0 {
		t.Fatalf("expected non-empty body.elements, got %#v", body["elements"])
	}
	first, ok := elements[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element object, got %#v", elements[0])
	}
	if first["tag"] != "markdown" {
		t.Fatalf("expected markdown element, got %#v", first["tag"])
	}
	last, ok := elements[len(elements)-1].(map[string]any)
	if !ok {
		t.Fatalf("expected last element object, got %#v", elements[len(elements)-1])
	}
	lastContent, _ := last["content"].(string)
	if !strings.Contains(lastContent, "-") || !strings.Contains(lastContent, ":") {
		t.Fatalf("expected human readable timestamp in last element, got %q", lastContent)
	}
}

type codexStub struct {
	resp string
	err  error
}

func (c codexStub) Run(_ context.Context, _ string) (string, error) {
	return c.resp, c.err
}

type codexStreamingStub struct {
	resp      string
	err       error
	reasoning []string
}

func (c codexStreamingStub) Run(_ context.Context, _ string) (string, error) {
	return c.resp, c.err
}

func (c codexStreamingStub) RunWithProgress(
	_ context.Context,
	_ string,
	onThinking func(step string),
) (string, error) {
	for _, step := range c.reasoning {
		onThinking(step)
	}
	return c.resp, c.err
}

type senderStub struct {
	sendCalls      int
	lastSendText   string
	replyTextCalls int
	lastReplyText  string

	replyCardCalls  int
	lastReplyCard   string
	patchCardCalls  int
	lastPatchedCard string
	patchCardErr    error
}

func (s *senderStub) SendText(_ context.Context, _, _ string, text string) error {
	s.sendCalls++
	s.lastSendText = text
	return nil
}

func (s *senderStub) ReplyText(_ context.Context, _ string, text string) (string, error) {
	s.replyTextCalls++
	s.lastReplyText = text
	return "om_reply_text", nil
}

func (s *senderStub) ReplyCard(_ context.Context, _ string, cardContent string) (string, error) {
	s.replyCardCalls++
	s.lastReplyCard = cardContent
	return "om_reply_card", nil
}

func (s *senderStub) PatchCard(_ context.Context, _ string, cardContent string) error {
	s.patchCardCalls++
	s.lastPatchedCard = cardContent
	return s.patchCardErr
}

func strPtr(s string) *string { return &s }
