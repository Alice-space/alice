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

	if sender.reactionCalls != 2 {
		t.Fatalf("expected ack and final reactions, got %d", sender.reactionCalls)
	}
	if len(sender.reactionTargets) != 2 ||
		sender.reactionTargets[0] != "om_src" ||
		sender.reactionTargets[1] != "om_reply_card" {
		t.Fatalf("unexpected reaction targets: %#v", sender.reactionTargets)
	}
	if len(sender.reactionTypes) != 2 ||
		sender.reactionTypes[0] != "SMILE" ||
		sender.reactionTypes[1] != finalReplyDoneEmoji {
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

	if sender.reactionCalls != 2 {
		t.Fatalf("expected ack and final reaction attempts, got %d", sender.reactionCalls)
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
	if sender.patchCardCalls != 0 {
		t.Fatalf("progress should be sent as completed agent messages, not patched cards; got %d patches", sender.patchCardCalls)
	}
	if len(sender.reactionTypes) != 1 || sender.reactionTypes[0] != finalReplyDoneEmoji {
		t.Fatalf("expected DONE reaction on final progress message, got %#v", sender.reactionTypes)
	}
	if len(sender.reactionTargets) != 1 || sender.reactionTargets[0] != "om_reply_card" {
		t.Fatalf("unexpected final reaction target: %#v", sender.reactionTargets)
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

func TestProcessor_FileChangeEventDoesNotSendStandaloneReply(t *testing.T) {
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
	if sender.replyCardCalls != 2 {
		t.Fatalf("expected ack + final card replies only, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 2 {
		t.Fatalf("unexpected card reply history: %#v", sender.replyCards)
	}
	if sender.sendCardCalls != 0 {
		t.Fatalf("expected no send card for filechange event, got %d", sender.sendCardCalls)
	}
	for _, card := range sender.replyCards {
		if strings.Contains(card, "internal/connector/processor.go已更改") {
			t.Fatalf("filechange should not be sent as standalone card, got %q", card)
		}
	}
	if !strings.Contains(sender.replyCards[1], "最终答复") {
		t.Fatalf("final reply should be card markdown, got %q", sender.replyCards[1])
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
