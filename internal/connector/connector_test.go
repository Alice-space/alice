package connector

import (
	"context"
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

func TestApp_OnMessageReceive_GroupWithoutMentionNotQueued(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_no_mention"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_no_mention"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"群里随便说说"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
			},
		},
	}

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(app.queue); got != 0 {
		t.Fatalf("expected queue len 0, got %d", got)
	}
}

func TestApp_OnMessageReceive_GroupMentionQueued(t *testing.T) {
	cfg := configForTest()
	cfg.FeishuBotOpenID = "ou_bot"
	app := NewApp(cfg, nil)

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "evt_with_mention"}},
		Event: &larkim.P2MessageReceiveV1Data{
			Message: &larkim.EventMessage{
				MessageId:   strPtr("om_with_mention"),
				MessageType: strPtr("text"),
				Content:     strPtr(`{"text":"<at user_id=\"ou_bot\">Alice</at> 你好"}`),
				ChatId:      strPtr("oc_chat"),
				ChatType:    strPtr("group"),
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

	if err := app.onMessageReceive(context.Background(), event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(app.queue); got != 1 {
		t.Fatalf("expected queue len 1, got %d", got)
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

	if sender.patchCardCalls != 1 {
		t.Fatalf("expected 1 patch call, got %d", sender.patchCardCalls)
	}
	if !strings.Contains(sender.lastPatchedCard, "已中断") {
		t.Fatalf("interrupted card should include interrupted status: %s", sender.lastPatchedCard)
	}
	if strings.Contains(sender.lastPatchedCard, "Codex 暂时不可用，请稍后重试") {
		t.Fatalf("interrupted card should not include failure message: %s", sender.lastPatchedCard)
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

func TestApp_EnqueueJobAssignsVersionAndCancelsActive(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	cancelCalls := 0
	app.latest["chat_id:oc_chat"] = 1
	app.active["chat_id:oc_chat"] = activeSession{
		version: 1,
		cancel: func() {
			cancelCalls++
		},
		eventID: "evt_old",
	}

	job := &Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		EventID:       "evt_new",
	}

	queued, cancelActive, canceledEventID := app.enqueueJob(job)
	if !queued {
		t.Fatal("expected job to be queued")
	}
	if job.SessionKey != "chat_id:oc_chat" {
		t.Fatalf("unexpected session key: %s", job.SessionKey)
	}
	if job.SessionVersion != 2 {
		t.Fatalf("unexpected session version: %d", job.SessionVersion)
	}
	if app.latest[job.SessionKey] != 2 {
		t.Fatalf("latest version should be 2, got %d", app.latest[job.SessionKey])
	}
	if canceledEventID != "evt_old" {
		t.Fatalf("unexpected canceled event id: %s", canceledEventID)
	}
	if cancelActive == nil {
		t.Fatal("expected active cancel func")
	}
	cancelActive()
	if cancelCalls != 1 {
		t.Fatalf("expected cancel to be called once, got %d", cancelCalls)
	}
}

func TestApp_ShouldProcessJobSkipsStaleVersion(t *testing.T) {
	cfg := configForTest()
	app := NewApp(cfg, nil)

	sessionKey := "chat_id:oc_chat"
	app.latest[sessionKey] = 3

	if app.shouldProcessJob(Job{SessionKey: sessionKey, SessionVersion: 2}) {
		t.Fatal("stale job should not be processed")
	}
	if !app.shouldProcessJob(Job{SessionKey: sessionKey, SessionVersion: 3}) {
		t.Fatal("latest job should be processed")
	}
}

func TestProcessor_UsesMemoryPromptAndSavesInteraction(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
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

	if memory.buildCalls != 1 || memory.lastBuildInput != "hello" {
		t.Fatalf("unexpected memory build call: %+v", memory)
	}
	if fakeCodex.lastInput != "记忆上下文 + 用户消息" {
		t.Fatalf("codex should receive memory prompt, got: %s", fakeCodex.lastInput)
	}
	if memory.saveCalls != 1 {
		t.Fatalf("expected 1 memory save, got %d", memory.saveCalls)
	}
	if memory.lastSaveUser != "hello" {
		t.Fatalf("unexpected saved user text: %s", memory.lastSaveUser)
	}
	if memory.lastSaveReply != "final answer" {
		t.Fatalf("unexpected saved reply: %s", memory.lastSaveReply)
	}
	if memory.lastSaveFailed {
		t.Fatalf("save flag should be success")
	}
}

func TestProcessor_AttachesReplyParentMessageContext(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{
		messageTextByID: map[string]string{
			"om_parent": "上一条消息",
		},
	}
	processor := NewProcessor(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src",
		ReplyParentMessageID: "om_parent",
		Text:                 "继续",
	})

	if sender.getMessageTextCalls != 1 {
		t.Fatalf("expected 1 get message text call, got %d", sender.getMessageTextCalls)
	}
	if !strings.Contains(fakeCodex.lastInput, "被回复消息：\n上一条消息") {
		t.Fatalf("expected parent context in codex input, got: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "用户当前回复：\n继续") {
		t.Fatalf("expected current reply in codex input, got: %s", fakeCodex.lastInput)
	}
}

func TestProcessor_ReplyParentContextFetchFailureFallsBackToUserText(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{
		getMessageTextErr: errors.New("boom"),
	}
	processor := NewProcessor(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src",
		ReplyParentMessageID: "om_parent",
		Text:                 "继续",
	})

	if fakeCodex.lastInput != "继续" {
		t.Fatalf("expected fallback to user text, got: %s", fakeCodex.lastInput)
	}
}

func TestProcessor_ResumesCodexThreadWithinSameSession(t *testing.T) {
	fakeCodex := &codexResumableCaptureStub{
		respByCall:   []string{"B", "D"},
		threadByCall: []string{"thread_1", "thread_1"},
	}
	sender := &senderStub{
		messageTextByID: map[string]string{
			"om_parent": "上一条消息",
		},
	}
	processor := NewProcessor(
		fakeCodex,
		sender,
		"Codex 暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "A",
		SessionKey:    "chat_id:oc_chat",
	})

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src",
		ReplyParentMessageID: "om_parent",
		Text:                 "C",
		SessionKey:           "chat_id:oc_chat",
	})

	if len(fakeCodex.receivedThreadIDs) != 2 {
		t.Fatalf("expected 2 codex calls, got %d", len(fakeCodex.receivedThreadIDs))
	}
	if fakeCodex.receivedThreadIDs[0] != "" {
		t.Fatalf("first call should start new thread, got %q", fakeCodex.receivedThreadIDs[0])
	}
	if fakeCodex.receivedThreadIDs[1] != "thread_1" {
		t.Fatalf("second call should resume thread_1, got %q", fakeCodex.receivedThreadIDs[1])
	}
	if len(fakeCodex.receivedInputs) != 2 {
		t.Fatalf("expected 2 codex inputs, got %d", len(fakeCodex.receivedInputs))
	}
	if fakeCodex.receivedInputs[1] != "C" {
		t.Fatalf("second input should be direct follow-up text C, got %q", fakeCodex.receivedInputs[1])
	}
	if sender.getMessageTextCalls != 0 {
		t.Fatalf("resume mode should not fetch parent text, got %d", sender.getMessageTextCalls)
	}
}

func TestProcessor_ResumeSkipsMemoryBuildPrompt(t *testing.T) {
	fakeCodex := &codexResumableCaptureStub{
		respByCall:   []string{"B", "D"},
		threadByCall: []string{"thread_1", "thread_1"},
	}
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
		Text:          "A",
		SessionKey:    "chat_id:oc_chat",
	})
	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		Text:          "C",
		SessionKey:    "chat_id:oc_chat",
	})

	if memory.buildCalls != 1 {
		t.Fatalf("expected memory build only once before resume, got %d", memory.buildCalls)
	}
	if len(fakeCodex.receivedInputs) != 2 {
		t.Fatalf("expected 2 codex calls, got %d", len(fakeCodex.receivedInputs))
	}
	if fakeCodex.receivedInputs[0] != "记忆上下文 + 用户消息" {
		t.Fatalf("first call should use memory prompt, got %q", fakeCodex.receivedInputs[0])
	}
	if fakeCodex.receivedInputs[1] != "C" {
		t.Fatalf("resume call should use raw user text, got %q", fakeCodex.receivedInputs[1])
	}
}
