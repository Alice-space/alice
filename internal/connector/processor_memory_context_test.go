package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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

func TestProcessor_BuildsIdentityAwareUserContext(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{
		userNameByIdentity: map[string]string{
			"open_id:ou_bob":   "Bob",
			"open_id:ou_carlo": "Carlo",
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
		SenderOpenID:  "ou_bob",
		MentionedUsers: []MentionedUser{
			{OpenID: "ou_carlo"},
		},
		Text: "这是xxx",
	})

	if !strings.Contains(fakeCodex.lastInput, "用户Bob的id是ou_bob") {
		t.Fatalf("missing sender id mapping in input: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "用户Carlo的id是ou_carlo") {
		t.Fatalf("missing mentioned user id mapping in input: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "Bob说：@Carlo 这是xxx") {
		t.Fatalf("missing expected speech context in input: %s", fakeCodex.lastInput)
	}
}

func TestProcessor_BuildsIdentityAwareUserContext_WithChatMembersFallback(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{
		resolveUserNameErr: errors.New("contact lookup failed"),
		chatMemberNameByIdentity: map[string]string{
			"chat_id:oc_chat|open_id:ou_bob":   "Bob",
			"chat_id:oc_chat|open_id:ou_carlo": "Carlo",
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
		SenderOpenID:  "ou_bob",
		MentionedUsers: []MentionedUser{
			{OpenID: "ou_carlo"},
		},
		Text: "这是xxx",
	})

	if !strings.Contains(fakeCodex.lastInput, "用户Bob的id是ou_bob") {
		t.Fatalf("missing sender id mapping in input: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "用户Carlo的id是ou_carlo") {
		t.Fatalf("missing mentioned user id mapping in input: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "Bob说：@Carlo 这是xxx") {
		t.Fatalf("missing expected speech context in input: %s", fakeCodex.lastInput)
	}
	if sender.resolveChatMemberNameCalls == 0 {
		t.Fatalf("expected chat member fallback to be called")
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
