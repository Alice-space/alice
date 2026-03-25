package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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

	if sender.replyTextCalls != 0 {
		t.Fatalf("expected zero text replies, got %d", sender.replyTextCalls)
	}
	if sender.replyCardCalls != 2 {
		t.Fatalf("expected ack + final card replies, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 2 {
		t.Fatalf("unexpected card reply history: %#v", sender.replyCards)
	}
	if !strings.Contains(sender.replyCards[0], "收到！") {
		t.Fatalf("first card should be ack, got %q", sender.replyCards[0])
	}
	if !strings.Contains(sender.replyCards[1], "Codex 暂时不可用，请稍后重试。") {
		t.Fatalf("second card should be failure message, got %q", sender.replyCards[1])
	}
}

func TestProcessor_ReplyMessageFlow_ReactionImmediateFeedbackSkipsAckReply(t *testing.T) {
	fakeCodex := codexStub{resp: "final answer"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")
	processor.SetImmediateFeedback("reaction", "smile")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.reactionCalls != 1 {
		t.Fatalf("expected one reaction feedback, got %d", sender.reactionCalls)
	}
	if len(sender.reactionTargets) != 1 || sender.reactionTargets[0] != "om_src" {
		t.Fatalf("unexpected reaction targets: %#v", sender.reactionTargets)
	}
	if len(sender.reactionTypes) != 1 || sender.reactionTypes[0] != "SMILE" {
		t.Fatalf("unexpected reaction types: %#v", sender.reactionTypes)
	}
	if sender.replyCardCalls != 1 {
		t.Fatalf("expected final reply only after reaction ack, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 1 || !strings.Contains(sender.replyCards[0], "final answer") {
		t.Fatalf("unexpected reply history: %#v", sender.replyCards)
	}
}

func TestProcessor_ReplyMessageFlow_ReactionFallbacksToAckReply(t *testing.T) {
	fakeCodex := codexStub{resp: "final answer"}
	sender := &senderStub{reactionErr: errors.New("reaction unavailable")}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")
	processor.SetImmediateFeedback("reaction", "smile")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.reactionCalls != 1 {
		t.Fatalf("expected one reaction attempt, got %d", sender.reactionCalls)
	}
	if sender.replyCardCalls != 2 {
		t.Fatalf("expected ack reply fallback plus final reply, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 2 || !strings.Contains(sender.replyCards[0], "收到！") {
		t.Fatalf("unexpected reply history: %#v", sender.replyCards)
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

	if sender.replyTextCalls != 0 {
		t.Fatalf("expected zero text replies, got %d", sender.replyTextCalls)
	}
	if sender.replyRichMarkdownCalls != 0 {
		t.Fatalf("expected zero markdown post replies, got %d", sender.replyRichMarkdownCalls)
	}
	if sender.replyCardCalls != 3 {
		t.Fatalf("expected ack + 2 progress card replies, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 3 {
		t.Fatalf("unexpected card reply history: %#v", sender.replyCards)
	}
	if !strings.Contains(sender.replyCards[1], "阶段提示") {
		t.Fatalf("expected stage card content, got %q", sender.replyCards[1])
	}
	if !strings.Contains(sender.replyCards[2], "最终答复") {
		t.Fatalf("expected final progress card content, got %q", sender.replyCards[2])
	}
}

func TestProcessor_ChatSceneRepliesWithRichTextInsteadOfCards(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:          "最终答复",
		agentMessages: []string{"阶段提示", "最终答复"},
	}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:          "oc_chat",
		ReceiveIDType:      "chat_id",
		SourceMessageID:    "om_src",
		Scene:              jobSceneChat,
		ResponseMode:       jobResponseModeReply,
		CreateFeishuThread: false,
		DisableAck:         true,
		Text:               "hello",
	})

	if sender.replyCardCalls != 0 {
		t.Fatalf("chat scene should not use reply cards, got %d", sender.replyCardCalls)
	}
	if sender.replyRichMarkdownCalls != 2 {
		t.Fatalf("chat scene should use rich markdown replies for progress/final, got %d", sender.replyRichMarkdownCalls)
	}
	if sender.replyRichMarkdownDirectCalls != 2 {
		t.Fatalf("chat scene should reply directly without thread, got %d", sender.replyRichMarkdownDirectCalls)
	}
	if len(sender.replyMarkdownTexts) != 2 {
		t.Fatalf("unexpected markdown reply history: %#v", sender.replyMarkdownTexts)
	}
	if sender.replyMarkdownTexts[0] != "阶段提示" {
		t.Fatalf("unexpected progress markdown reply: %q", sender.replyMarkdownTexts[0])
	}
	if sender.replyMarkdownTexts[1] != "最终答复" {
		t.Fatalf("unexpected final markdown reply: %q", sender.replyMarkdownTexts[1])
	}
}

func TestProcessor_FileChangeEventRepliesInThread(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:          "最终答复",
		agentMessages: []string{"[file_change] internal/connector/processor.go已更改，+23-34"},
	}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src",
		ReplyParentMessageID: "om_parent",
		Text:                 "hello",
	})

	if sender.replyRichCalls != 0 {
		t.Fatalf("expected zero rich text replies, got %d", sender.replyRichCalls)
	}
	if sender.replyTextCalls != 0 {
		t.Fatalf("expected zero text replies, got %d", sender.replyTextCalls)
	}
	if sender.replyCardCalls != 3 {
		t.Fatalf("expected ack + filechange + final card replies, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 3 {
		t.Fatalf("unexpected card reply history: %#v", sender.replyCards)
	}
	if sender.sendCardCalls != 0 {
		t.Fatalf("expected no send card for filechange thread reply, got %d", sender.sendCardCalls)
	}
	if len(sender.replyTargets) != 3 {
		t.Fatalf("unexpected reply targets: %#v", sender.replyTargets)
	}
	if sender.replyTargets[1] != "om_src" {
		t.Fatalf("filechange should reply to current source message id, got %q", sender.replyTargets[1])
	}
	if !strings.Contains(sender.replyCards[1], "internal/connector/processor.go已更改，+23-34") {
		t.Fatalf("filechange should be sent as thread reply card, got %q", sender.replyCards[1])
	}
	if !strings.Contains(sender.replyCards[2], "最终答复") {
		t.Fatalf("final reply should be card markdown, got %q", sender.replyCards[2])
	}
}

func TestProcessor_FileChangeEventUsesPerSessionSourceWhenParentShared(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:          "最终答复",
		agentMessages: []string{"[file_change] internal/connector/processor.go已更改，+23-34"},
	}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src_a",
		ReplyParentMessageID: "om_shared_parent",
		Text:                 "hello A",
	})
	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src_b",
		ReplyParentMessageID: "om_shared_parent",
		Text:                 "hello B",
	})

	if len(sender.replyTargets) != 6 {
		t.Fatalf("unexpected reply targets: %#v", sender.replyTargets)
	}
	if sender.replyTargets[1] != "om_src_a" {
		t.Fatalf("first filechange should stay in source A thread, got %q", sender.replyTargets[1])
	}
	if sender.replyTargets[4] != "om_src_b" {
		t.Fatalf("second filechange should stay in source B thread, got %q", sender.replyTargets[4])
	}
}

func TestProcessor_FileChangeEventFallsBackToThreadWhenSourceMissing(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:          "最终答复",
		agentMessages: []string{"[file_change] internal/connector/processor.go已更改，+23-34"},
	}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.processReplyMessage(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		ThreadID:      "om_thread",
		Text:          "hello",
	})

	if sender.sendCardCalls != 0 {
		t.Fatalf("expected filechange to stay in thread reply, got sendCardCalls=%d", sender.sendCardCalls)
	}
	if len(sender.replyTargets) < 2 {
		t.Fatalf("unexpected reply targets: %#v", sender.replyTargets)
	}
	if sender.replyTargets[1] != "om_thread" {
		t.Fatalf("filechange should fallback to thread id, got %q", sender.replyTargets[1])
	}
}

func TestProcessor_FileChangeEventFallsBackToSendCardWithoutReplyTargets(t *testing.T) {
	fakeCodex := codexStreamingStub{
		resp:          "最终答复",
		agentMessages: []string{"[file_change] internal/connector/processor.go已更改，+23-34"},
	}
	sender := &senderStub{replyCardErr: errors.New("reply unavailable")}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.processReplyMessage(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
	})

	if sender.sendCardCalls != 1 {
		t.Fatalf("expected filechange to fallback to send card, got %d", sender.sendCardCalls)
	}
	if len(sender.sendCards) != 1 || !strings.Contains(sender.sendCards[0], "internal/connector/processor.go已更改，+23-34") {
		t.Fatalf("unexpected send card history: %#v", sender.sendCards)
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

	if sender.replyTextCalls != 0 {
		t.Fatalf("expected zero text replies, got %d", sender.replyTextCalls)
	}
	if sender.replyCardCalls != 2 {
		t.Fatalf("expected ack + final card replies, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 2 {
		t.Fatalf("unexpected card reply history: %#v", sender.replyCards)
	}
	if !strings.Contains(sender.replyCards[1], "final answer") {
		t.Fatalf("final reply should be card markdown, got %q", sender.replyCards[1])
	}
}

func TestProcessor_FallsBackToTextWhenFinalCardAndMarkdownReplyFail(t *testing.T) {
	fakeCodex := codexStub{resp: "final answer"}
	sender := &senderStub{
		replyCardErr:         errors.New("card unavailable"),
		replyRichMarkdownErr: errors.New("rich markdown unavailable"),
	}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		Text:            "hello",
	})

	if sender.replyCardCalls != 2 {
		t.Fatalf("expected card attempts for ack + final reply, got %d", sender.replyCardCalls)
	}
	if sender.replyRichMarkdownCalls != 2 {
		t.Fatalf("expected markdown fallback attempts for ack + final reply, got %d", sender.replyRichMarkdownCalls)
	}
	if sender.replyTextCalls != 2 {
		t.Fatalf("expected ack + fallback text reply, got %d", sender.replyTextCalls)
	}
	if len(sender.replyTexts) != 2 || sender.replyTexts[0] != "收到！" || sender.replyTexts[1] != "final answer" {
		t.Fatalf("unexpected fallback reply text history: %#v", sender.replyTexts)
	}
}
