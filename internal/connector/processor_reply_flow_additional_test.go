package connector

import (
	"context"
	"strings"
	"testing"
)

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
	if len(sender.reactionTypes) != 1 || sender.reactionTypes[0] != finalReplyDoneEmoji {
		t.Fatalf("expected DONE reaction on sent final message, got %#v", sender.reactionTypes)
	}
	if len(sender.reactionTargets) != 1 || sender.reactionTargets[0] != "om_send_card" {
		t.Fatalf("unexpected final reaction target: %#v", sender.reactionTargets)
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

	processor := NewProcessor(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
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
}

func TestProcessor_CanceledNonReplySkipsSending(t *testing.T) {
	fakeCodex := codexStub{err: context.Canceled}
	sender := &senderStub{}

	processor := NewProcessor(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "hello",
	})

	if sender.sendCalls != 0 {
		t.Fatalf("expected no send text calls, got %d", sender.sendCalls)
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
	if sender.reactionCalls != 2 {
		t.Fatalf("expected ack and final reactions, got %d", sender.reactionCalls)
	}
	if len(sender.reactionTypes) != 2 ||
		sender.reactionTypes[0] != "SMILE" ||
		sender.reactionTypes[1] != finalReplyDoneEmoji {
		t.Fatalf("unexpected reaction types: %#v", sender.reactionTypes)
	}
	if len(sender.reactionTargets) != 2 ||
		sender.reactionTargets[0] != "om_src" ||
		sender.reactionTargets[1] != "om_reply_text" {
		t.Fatalf("unexpected reaction targets: %#v", sender.reactionTargets)
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

func TestProcessor_MentionReplyStripsMarkdownWhenForcedToText(t *testing.T) {
	fakeCodex := codexStub{resp: "@李志昊 **请看下** 这个`结果`"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_src",
		SenderName:      "李志昊",
		SenderOpenID:    "ou_776ddbea0c07fd88caaf8fce1b413a41",
		Text:            "hello",
	})

	if sender.replyCardCalls != 1 {
		t.Fatalf("expected only ack card reply before final mention text, got %d", sender.replyCardCalls)
	}
	if sender.replyTextCalls != 1 {
		t.Fatalf("expected one text reply for mention output, got %d", sender.replyTextCalls)
	}
	want := `<at user_id="ou_776ddbea0c07fd88caaf8fce1b413a41">李志昊</at> 请看下 这个结果`
	if len(sender.replyTexts) != 1 || sender.replyTexts[0] != want {
		t.Fatalf("unexpected sanitized mention reply text: %#v", sender.replyTexts)
	}
}

func TestProcessor_NoSourceMentionStripsMarkdownWhenForcedToText(t *testing.T) {
	fakeCodex := codexStub{resp: "@Xiang Shi **收到** [详情](https://example.com)"}
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
		t.Fatalf("expected mention send not to use cards, got %d", sender.sendCardCalls)
	}
	if sender.sendCalls != 1 {
		t.Fatalf("expected one text send for mention output, got %d", sender.sendCalls)
	}
	want := `<at user_id="ou_809a189717a7a855905957ea612ca9f8">Xiang Shi</at> 收到 详情`
	if sender.lastSendText != want {
		t.Fatalf("unexpected sanitized mention send text: %q", sender.lastSendText)
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
		SoulDoc: soulDocument{
			OutputContract: outputContract{
				ReplyWillTag:   "reply_will",
				ReplyWillField: "reply_will",
			},
		},
		Text: "hello",
	})

	if sender.sendCardCalls != 0 {
		t.Fatalf("expected no card sends, got %d", sender.sendCardCalls)
	}
	if sender.sendCalls != 0 {
		t.Fatalf("expected no text sends, got %d", sender.sendCalls)
	}
}

func TestProcessor_SendModeSuppressesNoReplyToken_WithReplyWillBlock(t *testing.T) {
	fakeCodex := codexStub{resp: "<reply_will>32%</reply_will>\n[[NO_REPLY]]"}
	sender := &senderStub{}
	processor := NewProcessor(fakeCodex, sender, "Codex 暂时不可用，请稍后重试。", "正在思考中...")

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		ResponseMode:  jobResponseModeSend,
		NoReplyToken:  "[[NO_REPLY]]",
		SoulDoc: soulDocument{
			OutputContract: outputContract{
				ReplyWillTag:   "reply_will",
				ReplyWillField: "reply_will",
			},
		},
		Text: "hello",
	})

	if sender.sendCardCalls != 0 {
		t.Fatalf("expected no card sends, got %d", sender.sendCardCalls)
	}
	if sender.sendCalls != 0 {
		t.Fatalf("expected no text sends, got %d", sender.sendCalls)
	}
}

func TestProcessor_ChatSceneStripsReplyWillBlockBeforeSending(t *testing.T) {
	fakeCodex := codexStub{resp: "<reply_will>88%</reply_will>\n<motion>轻轻晃了晃尾巴</motion>\n咱在这儿看着你喵。"}
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
		SoulDoc: soulDocument{
			OutputContract: outputContract{
				ReplyWillTag:   "reply_will",
				ReplyWillField: "reply_will",
				MotionTag:      "motion",
			},
		},
		Text: "hello",
	})

	if sender.replyRichMarkdownCalls != 1 {
		t.Fatalf("expected one markdown reply, got %d", sender.replyRichMarkdownCalls)
	}
	if len(sender.replyMarkdownTexts) != 1 {
		t.Fatalf("unexpected markdown reply history: %#v", sender.replyMarkdownTexts)
	}
	if strings.Contains(sender.replyMarkdownTexts[0], "<reply_will>") {
		t.Fatalf("reply_will block should be stripped before sending, got %q", sender.replyMarkdownTexts[0])
	}
	if strings.Contains(sender.replyMarkdownTexts[0], "<motion>") {
		t.Fatalf("motion block should be stripped before sending, got %q", sender.replyMarkdownTexts[0])
	}
	want := "咱在这儿看着你喵。"
	if sender.replyMarkdownTexts[0] != want {
		t.Fatalf("unexpected markdown reply:\nwant: %q\ngot : %q", want, sender.replyMarkdownTexts[0])
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
		LLMProvider:        "claude",
		LLMModel:           "gpt-5.4-mini",
		LLMProfile:         "balanced",
		LLMReasoningEffort: "low",
		LLMPersonality:     "friendly",
		Text:               "hello",
	})

	if fakeCodex.lastReq.Model != "gpt-5.4-mini" {
		t.Fatalf("unexpected model passed to llm: %q", fakeCodex.lastReq.Model)
	}
	if fakeCodex.lastReq.Provider != "claude" {
		t.Fatalf("unexpected provider passed to llm: %q", fakeCodex.lastReq.Provider)
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
	// NoReplyToken is now assembled into UserText rather than passed as a separate field.
	if !strings.Contains(fakeCodex.lastReq.UserText, "[[NO_REPLY]]") {
		t.Fatalf("expected no-reply token in UserText, got: %q", fakeCodex.lastReq.UserText)
	}
}
