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
