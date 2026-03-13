package connector

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Alice-space/alice/internal/mcpbridge"
)

func TestProcessor_UsesMemoryPromptAndSavesInteraction(t *testing.T) {
	fakeCodex := &codexCaptureStub{resp: "final answer"}
	sender := &senderStub{resourceRoot: "/tmp/resources"}
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
		SourceMessageID: "om_source",
		Text:            "hello",
	})

	if memory.buildCalls != 1 {
		t.Fatalf("unexpected memory build call count: %+v", memory)
	}
	if memory.lastBuildScope != "chat_id:oc_chat" {
		t.Fatalf("unexpected memory build scope: %q", memory.lastBuildScope)
	}
	if !strings.Contains(memory.lastBuildInput, "alice-message skill") ||
		!strings.Contains(memory.lastBuildInput, "hello") {
		t.Fatalf("first memory build input should include concise tool hint and user text, got: %q", memory.lastBuildInput)
	}
	if fakeCodex.lastInput != "记忆上下文 + 用户消息" {
		t.Fatalf("codex should receive memory prompt, got: %s", fakeCodex.lastInput)
	}
	if memory.saveCalls != 1 {
		t.Fatalf("expected 1 memory save, got %d", memory.saveCalls)
	}
	if memory.lastSaveScope != "chat_id:oc_chat" {
		t.Fatalf("unexpected saved scope: %q", memory.lastSaveScope)
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
	if fakeCodex.lastEnv[mcpbridge.EnvReceiveIDType] != "chat_id" {
		t.Fatalf("missing mcp receive id type env: %#v", fakeCodex.lastEnv)
	}
	if fakeCodex.lastEnv[mcpbridge.EnvReceiveID] != "oc_chat" {
		t.Fatalf("missing mcp receive id env: %#v", fakeCodex.lastEnv)
	}
	if fakeCodex.lastEnv[mcpbridge.EnvSourceMessageID] != "om_source" {
		t.Fatalf("missing mcp source message env: %#v", fakeCodex.lastEnv)
	}
	wantResourceRoot := filepath.Join("/tmp/resources", "scopes", "chat_id", "oc_chat")
	if fakeCodex.lastEnv[mcpbridge.EnvResourceRoot] != wantResourceRoot {
		t.Fatalf("unexpected scoped mcp resource root env: %#v", fakeCodex.lastEnv)
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
	if !strings.Contains(fakeCodex.lastInput, "@提及规则：若需要在回复中艾特某人，请直接写 @姓名 或 @用户id（如 @ou_xxx），系统会自动转换为飞书 mention。") {
		t.Fatalf("missing mention usage hint in input: %s", fakeCodex.lastInput)
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
	if !strings.Contains(fakeCodex.lastInput, "@提及规则：若需要在回复中艾特某人，请直接写 @姓名 或 @用户id（如 @ou_xxx），系统会自动转换为飞书 mention。") {
		t.Fatalf("missing mention usage hint in input: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "Bob说：@Carlo 这是xxx") {
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
		"Codex 暂时不可用，请稍后重试。",
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

	if !strings.Contains(fakeCodex.lastInput, "用户Bob的id是ou_bob") {
		t.Fatalf("missing sender id mapping in input: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "用户Carlo的id是ou_carlo") {
		t.Fatalf("missing non-bot mentioned user id mapping in input: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "@提及规则：若需要在回复中艾特某人，请直接写 @姓名 或 @用户id（如 @ou_xxx），系统会自动转换为飞书 mention。") {
		t.Fatalf("missing mention usage hint in input: %s", fakeCodex.lastInput)
	}
	if strings.Contains(fakeCodex.lastInput, "用户Alice的id是ou_alice") {
		t.Fatalf("bot id mapping should be filtered from input: %s", fakeCodex.lastInput)
	}
	if !strings.Contains(fakeCodex.lastInput, "Bob说：@Carlo 这是xxx") {
		t.Fatalf("non-bot mention should remain in speech context: %s", fakeCodex.lastInput)
	}
	if strings.Contains(fakeCodex.lastInput, "@Alice") {
		t.Fatalf("bot mention should be filtered from speech context: %s", fakeCodex.lastInput)
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

	if !strings.Contains(fakeCodex.lastInput, "alice-message skill") ||
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
		"Codex 暂时不可用，请稍后重试。",
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
	if !strings.Contains(fakeCodex.receivedInputs[0], "alice-message skill") ||
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
