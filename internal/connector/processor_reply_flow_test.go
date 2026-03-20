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

	if sender.replyTextCalls != 0 {
		t.Fatalf("expected zero text replies, got %d", sender.replyTextCalls)
	}
	if sender.replyCardCalls != 3 {
		t.Fatalf("expected ack + deduplicated stage/final card replies, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 3 {
		t.Fatalf("unexpected card reply history: %#v", sender.replyCards)
	}
	if !strings.Contains(sender.replyCards[1], "阶段提示") || !strings.Contains(sender.replyCards[2], "最终答复") {
		t.Fatalf("unexpected card progress content: %#v", sender.replyCards)
	}
}

func TestProcessor_NoSourceMessageUsesSendCard(t *testing.T) {
	fakeCodex := codexStub{resp: "final answer"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
	})

	if sender.sendCardCalls != 1 {
		t.Fatalf("expected 1 send card call, got %d", sender.sendCardCalls)
	}
	if len(sender.sendCards) != 1 || !strings.Contains(sender.sendCards[0], "final answer") {
		t.Fatalf("unexpected send card content: %#v", sender.sendCards)
	}
	if sender.sendCalls != 0 {
		t.Fatalf("expected 0 send text call, got %d", sender.sendCalls)
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
	if len(sender.downloadScopeKeys) != 1 || sender.downloadScopeKeys[0] != "chat_id:oc_chat" {
		t.Fatalf("expected attachment download to use scoped resource root key, got %#v", sender.downloadScopeKeys)
	}
	if len(sender.downloadSourceMessageIDs) != 1 || sender.downloadSourceMessageIDs[0] != "om_media" {
		t.Fatalf("expected attachment download to use attachment source message id, got %#v", sender.downloadSourceMessageIDs)
	}
	if !strings.Contains(fakeCodex.lastInput, "本地路径：/tmp/alice/image.png") {
		t.Fatalf("codex input should include downloaded local path, got: %s", fakeCodex.lastInput)
	}
	if sender.replyTextCalls != 0 {
		t.Fatalf("expected zero text replies, got %d", sender.replyTextCalls)
	}
	if sender.replyCardCalls != 2 {
		t.Fatalf("expected ack + final card replies, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 2 || !strings.Contains(sender.replyCards[1], "final answer") {
		t.Fatalf("final reply should be card markdown, got %#v", sender.replyCards)
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

	if sender.replyTextCalls != 0 {
		t.Fatalf("expected zero text replies, got %d", sender.replyTextCalls)
	}
	if sender.replyCardCalls != 2 {
		t.Fatalf("expected ack + interrupted card replies, got %d", sender.replyCardCalls)
	}
	if len(sender.replyCards) != 2 {
		t.Fatalf("unexpected card reply history: %#v", sender.replyCards)
	}
	if !strings.Contains(sender.replyCards[1], "已中断") {
		t.Fatalf("second card should be interrupted message, got %q", sender.replyCards[1])
	}
	if strings.Contains(sender.replyCards[1], "Codex 暂时不可用，请稍后重试") {
		t.Fatalf("interrupted reply should not include failure message: %q", sender.replyCards[1])
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

func TestProcessor_ReplyMentionUsesTextReply(t *testing.T) {
	fakeCodex := codexStub{resp: "@李志昊 请看下这个结果"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")
	processor.SetImmediateFeedback("reaction", "smile")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		SenderName:      "李志昊",
		SenderOpenID:    "ou_776ddbea0c07fd88caaf8fce1b413a41",
		Text:            "hello",
	})

	if sender.replyCardCalls != 0 {
		t.Fatalf("expected no ack card reply in reaction mode, got %d", sender.replyCardCalls)
	}
	if sender.reactionCalls != 1 {
		t.Fatalf("expected one reaction ack, got %d", sender.reactionCalls)
	}
	if sender.replyTextCalls != 1 {
		t.Fatalf("expected one final text reply for mention, got %d", sender.replyTextCalls)
	}
	want := `<at user_id="ou_776ddbea0c07fd88caaf8fce1b413a41">李志昊</at> 请看下这个结果`
	if len(sender.replyTexts) != 1 || sender.replyTexts[0] != want {
		t.Fatalf("unexpected mention reply text: %#v", sender.replyTexts)
	}
}

func TestProcessor_NoSourceMentionUsesSendText(t *testing.T) {
	fakeCodex := codexStub{resp: "@Xiang Shi 收到"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		MentionedUsers: []MentionedUser{
			{
				Name:   "Xiang Shi",
				OpenID: "ou_809a189717a7a855905957ea612ca9f8",
			},
		},
		Text: "hello",
	})

	if sender.sendCardCalls != 0 {
		t.Fatalf("expected no card send for mention output, got %d", sender.sendCardCalls)
	}
	if sender.sendCalls != 1 {
		t.Fatalf("expected one text send for mention output, got %d", sender.sendCalls)
	}
	want := `<at user_id="ou_809a189717a7a855905957ea612ca9f8">Xiang Shi</at> 收到`
	if sender.lastSendText != want {
		t.Fatalf("unexpected mention send text: %q", sender.lastSendText)
	}
}

func TestProcessor_SendModeSuppressesNoReplyToken(t *testing.T) {
	fakeCodex := codexStub{resp: "[[NO_REPLY]]"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		ResponseMode:  jobResponseModeSend,
		NoReplyToken:  "[[NO_REPLY]]",
		Text:          "hello",
	})

	if sender.sendCardCalls != 0 {
		t.Fatalf("expected no card sends, got %d", sender.sendCardCalls)
	}
	if sender.sendCalls != 0 {
		t.Fatalf("expected no text sends, got %d", sender.sendCalls)
	}
}

func TestProcessor_PassesJobLLMRunOptionsToBackend(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "[[NO_REPLY]]"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:          "oc_chat",
		ReceiveIDType:      "chat_id",
		ResponseMode:       jobResponseModeSend,
		NoReplyToken:       "[[NO_REPLY]]",
		LLMModel:           "gpt-5.4-mini",
		LLMProfile:         "balanced",
		LLMReasoningEffort: "low",
		LLMPersonality:     "friendly",
		Text:               "hello",
	})

	if fakeCodex.lastReq.Model != "gpt-5.4-mini" {
		t.Fatalf("unexpected model passed to llm: %q", fakeCodex.lastReq.Model)
	}
	if fakeCodex.lastReq.Profile != "balanced" {
		t.Fatalf("unexpected profile passed to llm: %q", fakeCodex.lastReq.Profile)
	}
	if fakeCodex.lastReq.ReasoningEffort != "low" {
		t.Fatalf("unexpected reasoning effort passed to llm: %q", fakeCodex.lastReq.ReasoningEffort)
	}
	if fakeCodex.lastReq.Personality != "friendly" {
		t.Fatalf("unexpected personality passed to llm: %q", fakeCodex.lastReq.Personality)
	}
	if fakeCodex.lastReq.NoReplyToken != "[[NO_REPLY]]" {
		t.Fatalf("unexpected no-reply token passed to llm: %q", fakeCodex.lastReq.NoReplyToken)
	}
}
