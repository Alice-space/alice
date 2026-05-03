package connector

import (
	"context"
	"errors"
	"strings"
	"testing"
)

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
		"暂时不可用，请稍后重试。",
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

	if !strings.Contains(fakeCodex.lastInput, "`Bob`说：@Carlo 这是xxx") {
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
		"暂时不可用，请稍后重试。",
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

	if !strings.Contains(fakeCodex.lastInput, "`Bob`说：@Carlo 这是xxx") {
		t.Fatalf("missing expected speech context in input: %s", fakeCodex.lastInput)
	}
	if sender.resolveChatMemberNameCalls == 0 {
		t.Fatalf("expected chat member fallback to be called")
	}
}

func TestProcessor_BuildsIdentityAwareUserContext_SkipsBotIdentity(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{
		userNameByIdentity: map[string]string{
			"open_id:ou_bob":   "Bob",
			"open_id:ou_carlo": "Carlo",
			"open_id:ou_alice": "Alice",
		},
	}
	processor := NewProcessor(
		fakeCodex,
		sender,
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		BotOpenID:     "ou_alice",
		SenderOpenID:  "ou_bob",
		MentionedUsers: []MentionedUser{
			{OpenID: "ou_alice"},
			{OpenID: "ou_carlo"},
		},
		Text: "这是xxx",
	})

	if !strings.Contains(fakeCodex.lastInput, "`Bob`说：@Carlo 这是xxx") {
		t.Fatalf("non-bot mention should remain in speech context: %s", fakeCodex.lastInput)
	}
	if strings.Contains(fakeCodex.lastInput, "@Alice") {
		t.Fatalf("bot mention should be filtered from speech context: %s", fakeCodex.lastInput)
	}
}

func TestProcessor_ResumeThreadSkipsRepeatedSenderMappingHint(t *testing.T) {
	fakeCodex := &codexResumableCaptureStub{
		respByCall:   []string{"ok-1", "ok-2"},
		threadByCall: []string{"thread_1", "thread_1"},
	}
	sender := &senderStub{
		userNameByIdentity: map[string]string{
			"open_id:ou_bob": "Bob",
		},
	}
	processor := NewProcessor(
		fakeCodex,
		sender,
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		SenderOpenID:  "ou_bob",
		Text:          "第一次",
		SessionKey:    "chat_id:oc_chat",
	})
	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		SenderOpenID:  "ou_bob",
		Text:          "第二次",
		SessionKey:    "chat_id:oc_chat",
	})

	if len(fakeCodex.receivedInputs) != 2 {
		t.Fatalf("expected 2 codex inputs, got %d", len(fakeCodex.receivedInputs))
	}
	if fakeCodex.receivedInputs[0] != "`Bob`说：第一次" {
		t.Fatalf("first input should be plain speaker context, got %q", fakeCodex.receivedInputs[0])
	}
	if strings.Contains(fakeCodex.receivedInputs[1], "@提及规则：若需要在回复中艾特某人") {
		t.Fatalf("resume input should not repeat mention rule, got %q", fakeCodex.receivedInputs[1])
	}
	if fakeCodex.receivedInputs[1] != "`Bob`说：第二次" {
		t.Fatalf("unexpected resume input: %q", fakeCodex.receivedInputs[1])
	}
}

func TestProcessor_PreservesMentionOrderWhenTextAlreadyContainsMention(t *testing.T) {
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
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:     "oc_chat",
		ReceiveIDType: "chat_id",
		SenderOpenID:  "ou_bob",
		MentionedUsers: []MentionedUser{
			{OpenID: "ou_carlo"},
		},
		Text: "我是谁？@Carlo 又是谁？",
	})

	if !strings.Contains(fakeCodex.lastInput, "`Bob`说：我是谁？@Carlo 又是谁？") {
		t.Fatalf("missing expected preserved mention order in input: %s", fakeCodex.lastInput)
	}
	if strings.Contains(fakeCodex.lastInput, "`Bob`说：@Carlo 我是谁？") {
		t.Fatalf("mention should not be duplicated or re-prefixed in input: %s", fakeCodex.lastInput)
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
		"暂时不可用，请稍后重试。",
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
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:            "oc_chat",
		ReceiveIDType:        "chat_id",
		SourceMessageID:      "om_src",
		ReplyParentMessageID: "om_parent",
		Text:                 "继续",
	})

	if !strings.Contains(fakeCodex.lastInput, "@提及规则：若需要在回复中艾特某人") ||
		!strings.Contains(fakeCodex.lastInput, "继续") {
		t.Fatalf("expected fallback input to include concise tool hint and user text, got: %s", fakeCodex.lastInput)
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
		"暂时不可用，请稍后重试。",
		"正在思考中...",
	)

	processor.ProcessJob(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		SourceMessageID: "om_first",
		Text:            "A",
		SessionKey:      "chat_id:oc_chat",
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
	if !strings.Contains(fakeCodex.receivedInputs[0], "@提及规则：若需要在回复中艾特某人") ||
		!strings.Contains(fakeCodex.receivedInputs[0], "A") {
		t.Fatalf("first input should include concise mcp auto-route hint and first text, got %q", fakeCodex.receivedInputs[0])
	}
	if fakeCodex.receivedInputs[1] != "C" {
		t.Fatalf("resume input should not include repeated tool hint, got %q", fakeCodex.receivedInputs[1])
	}
	if sender.getMessageTextCalls != 0 {
		t.Fatalf("resume mode should not fetch parent text, got %d", sender.getMessageTextCalls)
	}
}
