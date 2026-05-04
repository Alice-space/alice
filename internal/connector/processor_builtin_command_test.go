package connector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentbridge "github.com/Alice-space/agentbridge"
	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/prompting"
)

type llmCallCountingStub struct {
	calls int
}

func (s *llmCallCountingStub) Run(_ context.Context, _ agentbridge.RunRequest) (agentbridge.RunResult, error) {
	s.calls++
	return agentbridge.RunResult{Reply: "unexpected"}, nil
}

func TestProcessor_HelpCommand_ListsBuiltinCommands(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		ChatType:        "group",
		SenderOpenID:    "ou_actor",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|message:om_root",
		Text:            "/help",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected builtin command to bypass llm, got %d llm calls", llmStub.calls)
	}
	if sender.replyCardCalls != 1 || sender.replyCardDirectCalls != 1 {
		t.Fatalf("expected one direct help card reply, got card=%d direct=%d", sender.replyCardCalls, sender.replyCardDirectCalls)
	}
	if sender.replyRichMarkdownCalls != 0 || sender.replyRichMarkdownDirectCalls != 0 {
		t.Fatalf("expected help command not to use rich markdown reply, got rich=%d direct=%d", sender.replyRichMarkdownCalls, sender.replyRichMarkdownDirectCalls)
	}
	card := parseReplyCard(t, sender.replyCards[0])
	if got := card.Header.Title.Content; got != builtinHelpCardTitle {
		t.Fatalf("unexpected help card title: %q", got)
	}
	reply := card.Body.Elements[0].Content
	for _, want := range []string{
		"## Alice 内建命令",
		"`/help`",
		"`/status`",
		"`/clear`",
		"`/stop`",
		"`普通模式`",
		"`工作模式`",
		"`#work`",
		"backend/session/cwd",
		"空 `@Alice #work`",
		"`@Alice #work /session <backend-session-id>`",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected reply to contain %q, got %q", want, reply)
		}
	}
}

func TestProcessor_HelpCommand_UsesPromptTemplateOverride(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")

	root := t.TempDir()
	templatePath := filepath.Join(root, "connector", "help.md.tmpl")
	if err := os.MkdirAll(filepath.Dir(templatePath), 0o750); err != nil {
		t.Fatalf("create prompt template dir failed: %v", err)
	}
	if err := os.WriteFile(templatePath, []byte("custom help {{ .WorkModeTrigger }}"), 0o600); err != nil {
		t.Fatalf("write prompt template failed: %v", err)
	}
	processor.SetPromptLoader(prompting.NewLoader(root))

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		ChatType:        "group",
		SenderOpenID:    "ou_actor",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|message:om_root",
		Text:            "/help",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected builtin command to bypass llm, got %d llm calls", llmStub.calls)
	}
	card := parseReplyCard(t, sender.replyCards[0])
	reply := card.Body.Elements[0].Content
	if reply != "custom help `@机器人` + `#work`" {
		t.Fatalf("unexpected custom help reply: %q", reply)
	}
}

func TestProcessor_ClearCommand_RotatesGroupChatSceneSession(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	processor.SetBuiltinHelpConfig(configForGroupScenesTest())

	baseSessionKey := buildChatSceneSessionKey("chat_id", "oc_chat")
	processor.setThreadID(baseSessionKey, "thread_old")

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		ChatType:        "group",
		SourceMessageID: "om_clear",
		SessionKey:      "chat_id:oc_chat|message:om_clear",
		Text:            "/clear",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected clear command to bypass llm, got %d llm calls", llmStub.calls)
	}
	if sender.replyRichMarkdownCalls != 1 || sender.replyRichMarkdownDirectCalls != 1 {
		t.Fatalf("expected one direct rich markdown reply, got rich=%d direct=%d", sender.replyRichMarkdownCalls, sender.replyRichMarkdownDirectCalls)
	}
	if !strings.Contains(sender.replyMarkdownTexts[0], "已经清空") {
		t.Fatalf("unexpected clear reply: %q", sender.replyMarkdownTexts[0])
	}
	if !strings.Contains(sender.replyMarkdownTexts[0], "新的 Codex session") {
		t.Fatalf("unexpected clear reply: %q", sender.replyMarkdownTexts[0])
	}

	resolved := processor.resolveCanonicalSessionKey(baseSessionKey)
	if resolved == "" || resolved == baseSessionKey {
		t.Fatalf("expected base session key to rotate, got %q", resolved)
	}
	if threadID := processor.getThreadID(resolved); threadID != "" {
		t.Fatalf("expected rotated session to start without thread id, got %q", threadID)
	}
}

func TestProcessor_StopCommand_BypassesLLMAndReplies(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		ChatType:        "group",
		SourceMessageID: "om_stop",
		SessionKey:      "chat_id:oc_chat|thread:omt_1",
		Text:            "/stop",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected stop command to bypass llm, got %d llm calls", llmStub.calls)
	}
	if sender.replyRichMarkdownCalls != 1 || sender.replyRichMarkdownDirectCalls != 1 {
		t.Fatalf("expected one direct rich markdown reply, got rich=%d direct=%d", sender.replyRichMarkdownCalls, sender.replyRichMarkdownDirectCalls)
	}
	if !strings.Contains(sender.replyMarkdownTexts[0], "Codex session") {
		t.Fatalf("unexpected stop reply: %q", sender.replyMarkdownTexts[0])
	}
	if !strings.Contains(sender.replyMarkdownTexts[0], "会保留") {
		t.Fatalf("unexpected stop reply: %q", sender.replyMarkdownTexts[0])
	}
}

func TestProcessor_WorkThreadBootstrap_EmptyWorkDoesNotCallLLM(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	processor.SetWorkspaceDir("/repo")

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:          "oc_chat",
		ReceiveIDType:      "chat_id",
		ChatType:           "group",
		SourceMessageID:    "om_work_root",
		SessionKey:         "chat_id:oc_chat|scene:work|seed:om_work_root",
		Scene:              jobSceneWork,
		ResponseMode:       jobResponseModeReply,
		CreateFeishuThread: true,
		LLMProvider:        "codex",
		LLMProfile:         "work",
		LLMModel:           "gpt-5.4",
		Text:               "",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected empty work bootstrap to bypass llm, got %d llm calls", llmStub.calls)
	}
	if sender.replyCardCalls != 1 || sender.replyCardDirectCalls != 0 {
		t.Fatalf("expected one threaded bootstrap card reply, got card=%d direct=%d", sender.replyCardCalls, sender.replyCardDirectCalls)
	}
	card := parseReplyCard(t, sender.replyCards[0])
	if got := card.Header.Title.Content; got != builtinWorkThreadCardTitle {
		t.Fatalf("unexpected bootstrap card title: %q", got)
	}
	reply := card.Body.Elements[0].Content
	for _, want := range []string{
		"Work thread 已创建。",
		"backend: `codex`",
		"backend profile: `work`",
		"backend session id: `未开始`",
		"cwd: `/repo`",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected bootstrap reply to contain %q, got %q", want, reply)
		}
	}
	if !processor.hasActiveSession("chat_id:oc_chat|scene:work|seed:om_work_root") {
		t.Fatal("expected empty work bootstrap to create a session state")
	}
}

func TestProcessor_SessionCommand_BindsBackendSessionAndRepliesInWorkThread(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	processor.SetWorkspaceDir("/repo")

	sessionKey := "chat_id:oc_chat|scene:work|seed:om_work_root"
	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:          "oc_chat",
		ReceiveIDType:      "chat_id",
		ChatType:           "group",
		SourceMessageID:    "om_session",
		SessionKey:         sessionKey,
		Scene:              jobSceneWork,
		ResponseMode:       jobResponseModeReply,
		CreateFeishuThread: true,
		LLMProvider:        "codex",
		LLMProfile:         "work",
		LLMModel:           "gpt-5.4",
		Text:               "/session sess_123",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected /session without instruction to bypass llm, got %d llm calls", llmStub.calls)
	}
	if got := processor.getThreadID(sessionKey); got != "sess_123" {
		t.Fatalf("unexpected bound backend session id: %q", got)
	}
	if sender.replyCardCalls != 1 || sender.replyCardDirectCalls != 0 {
		t.Fatalf("expected one threaded session card reply, got card=%d direct=%d", sender.replyCardCalls, sender.replyCardDirectCalls)
	}
	card := parseReplyCard(t, sender.replyCards[0])
	reply := card.Body.Elements[0].Content
	for _, want := range []string{
		"已绑定后端 session。",
		"backend session id: `sess_123`",
		"CLI resume: `codex resume -C /repo sess_123`",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected session reply to contain %q, got %q", want, reply)
		}
	}
}

func TestProcessor_SessionCommandWithInstruction_ResumesBoundBackendSession(t *testing.T) {
	llmStub := &codexCaptureStub{resp: "done"}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")

	sessionKey := "chat_id:oc_chat|scene:work|seed:om_work_root"
	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:          "oc_chat",
		ReceiveIDType:      "chat_id",
		ChatType:           "group",
		SourceMessageID:    "om_session",
		SessionKey:         sessionKey,
		Scene:              jobSceneWork,
		ResponseMode:       jobResponseModeReply,
		CreateFeishuThread: true,
		LLMProvider:        "codex",
		LLMProfile:         "work",
		Text:               "/session sess_123 继续检查刚才的问题",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.lastReq.ThreadID != "sess_123" {
		t.Fatalf("expected llm to resume bound session, got %q", llmStub.lastReq.ThreadID)
	}
	if got := processor.getThreadID(sessionKey); got != "sess_123" {
		t.Fatalf("unexpected stored backend session id: %q", got)
	}
	if !strings.Contains(llmStub.lastInput, "继续检查刚才的问题") {
		t.Fatalf("expected instruction in llm input, got %q", llmStub.lastInput)
	}
	if strings.Contains(llmStub.lastInput, "/session") {
		t.Fatalf("session directive should be stripped from llm input, got %q", llmStub.lastInput)
	}
}

func TestProcessor_StatusCommand_ListsActiveAutomationTasks(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	processor.SetStatusIdentity("alice", "Alice")

	automationStore := automation.NewStore(filepath.Join(t.TempDir(), "automation.db"))
	processor.SetStatusStores(automationStore)

	if _, err := automationStore.CreateTask(automation.Task{
		ID:       "task_active",
		Title:    "heartbeat",
		Scope:    automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		Route:    automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:  automation.Actor{OpenID: "ou_actor"},
		Schedule: automation.Schedule{EverySeconds: 600},
		Prompt:   "/alice reconcile camp_active",
		Status:   automation.TaskStatusActive,
	}); err != nil {
		t.Fatalf("create active task failed: %v", err)
	}
	if _, err := automationStore.CreateTask(automation.Task{
		ID:       "task_paused",
		Title:    "daily report",
		Scope:    automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		Route:    automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:  automation.Actor{OpenID: "ou_actor"},
		Schedule: automation.Schedule{EverySeconds: 3600},
		Prompt:   "summarize current progress",
		Status:   automation.TaskStatusPaused,
	}); err != nil {
		t.Fatalf("create paused task failed: %v", err)
	}

	processor.recordSessionUsage(buildChatSceneSessionKey("chat_id", "oc_chat"), agentbridge.Usage{
		InputTokens:       120,
		CachedInputTokens: 60,
		OutputTokens:      15,
	})
	peerStatePath := filepath.Join(t.TempDir(), "mea_session_state.json")
	peerSnapshot := sessionStateSnapshot{
		BotID:   "mea",
		BotName: "Mea",
		Sessions: map[string]sessionState{
			buildChatSceneSessionKey("chat_id", "oc_chat"): {
				ScopeKey: "chat_id:oc_chat",
				Usage: sessionUsageStats{
					InputTokens:       80,
					CachedInputTokens: 20,
					OutputTokens:      10,
					Turns:             2,
					UpdatedAt:         time.Date(2026, 3, 23, 12, 0, 0, 0, time.FixedZone("CST", 8*3600)),
				},
			},
		},
	}
	rawPeerSnapshot, err := json.Marshal(peerSnapshot)
	if err != nil {
		t.Fatalf("marshal peer snapshot failed: %v", err)
	}
	if err := os.WriteFile(peerStatePath, rawPeerSnapshot, 0o600); err != nil {
		t.Fatalf("write peer snapshot failed: %v", err)
	}
	processor.SetStatusUsageSources([]StatusUsageSource{
		{BotID: "alice", BotName: "Alice", SessionStatePath: filepath.Join(t.TempDir(), "unused-self.json")},
		{BotID: "mea", BotName: "Mea", SessionStatePath: peerStatePath},
	})

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		ChatType:        "group",
		SenderOpenID:    "ou_actor",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|thread:omt_2|message:om_src",
		Text:            "/status",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected status command to bypass llm, got %d llm calls", llmStub.calls)
	}
	if sender.replyCardCalls != 1 || sender.replyCardDirectCalls != 1 {
		t.Fatalf("expected one direct status card reply, got card=%d direct=%d", sender.replyCardCalls, sender.replyCardDirectCalls)
	}
	if sender.replyRichMarkdownCalls != 0 || sender.replyRichMarkdownDirectCalls != 0 {
		t.Fatalf("expected status command not to use rich markdown reply, got rich=%d direct=%d", sender.replyRichMarkdownCalls, sender.replyRichMarkdownDirectCalls)
	}

	card := parseReplyCard(t, sender.replyCards[0])
	if got := card.Header.Title.Content; got != builtinStatusCardTitle {
		t.Fatalf("unexpected status card title: %q", got)
	}
	reply := card.Body.Elements[0].Content
	for _, want := range []string{
		"## Alice 当前状态",
		"总 token：`225`",
		"token 明细：input `200` | cached `80` | output `25` | turns `3`",
		"`Alice` | total `135` | input `120` | cached `60` | output `15` | turns `1`",
		"`Mea` | total `90` | input `80` | cached `20` | output `10` | turns `2`",
		"活跃自动化任务：`1`",
		"`task_active`",
		"`run_llm`",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected status reply to contain %q, got %q", want, reply)
		}
	}
	for _, unwanted := range []string{
		"task_paused",
	} {
		if strings.Contains(reply, unwanted) {
			t.Fatalf("expected status reply not to contain %q, got %q", unwanted, reply)
		}
	}
}

func TestProcessor_StatusCommand_ShowsCurrentWorkSessionDetails(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	processor.SetWorkspaceDir("/repo")
	processor.SetStatusIdentity("alice", "Alice")
	automationStore := automation.NewStore(filepath.Join(t.TempDir(), "automation.db"))
	processor.SetStatusStores(automationStore)

	sessionKey := "chat_id:oc_chat|scene:work|seed:om_work_root"
	processor.setThreadID(sessionKey, "sess_123")
	processor.setWorkThreadID(sessionKey, "omt_work")
	processor.recordSessionMetadata(sessionKey, Job{
		LLMProvider: "codex",
		LLMProfile:  "work",
		LLMModel:    "gpt-5.4",
	})

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:          "oc_chat",
		ReceiveIDType:      "chat_id",
		ChatType:           "group",
		SourceMessageID:    "om_status",
		ThreadID:           "omt_work",
		SessionKey:         sessionKey,
		Scene:              jobSceneWork,
		ResponseMode:       jobResponseModeReply,
		CreateFeishuThread: true,
		LLMProvider:        "codex",
		LLMProfile:         "work",
		LLMModel:           "gpt-5.4",
		Text:               "/status",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected status command to bypass llm, got %d llm calls", llmStub.calls)
	}
	if sender.replyCardCalls != 1 || sender.replyCardDirectCalls != 0 {
		t.Fatalf("expected one threaded status card reply, got card=%d direct=%d", sender.replyCardCalls, sender.replyCardDirectCalls)
	}
	card := parseReplyCard(t, sender.replyCards[0])
	reply := card.Body.Elements[0].Content
	for _, want := range []string{
		"### 当前 Session",
		"scene: `work`",
		"Alice session key: `chat_id:oc_chat|scene:work|seed:om_work_root`",
		"Feishu thread id: `omt_work`",
		"backend: `codex`",
		"backend session id: `sess_123`",
		"cwd: `/repo`",
		"CLI resume: `codex resume -C /repo sess_123`",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected status reply to contain %q, got %q", want, reply)
		}
	}
}

type replyCardPayload struct {
	Header struct {
		Title struct {
			Content string `json:"content"`
		} `json:"title"`
	} `json:"header"`
	Body struct {
		Elements []struct {
			Tag     string `json:"tag"`
			Content string `json:"content"`
		} `json:"elements"`
	} `json:"body"`
}

func parseReplyCard(t *testing.T, raw string) replyCardPayload {
	t.Helper()
	var payload replyCardPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal reply card failed: %v", err)
	}
	if len(payload.Body.Elements) == 0 {
		t.Fatalf("reply card missing body elements: %q", raw)
	}
	return payload
}

func TestIsHelpCommand(t *testing.T) {
	for _, text := range []string{
		"/help",
		"  /help  ",
		"/help codearmy",
	} {
		if !isHelpCommand(text) {
			t.Fatalf("expected %q to be recognized as help command", text)
		}
	}
	for _, text := range []string{
		"help",
		"/ helper",
		"/helpful",
	} {
		if isHelpCommand(text) {
			t.Fatalf("expected %q to be rejected as help command", text)
		}
	}
}

func TestIsStatusCommand(t *testing.T) {
	for _, text := range []string{
		"/status",
		"  /status  ",
		"/status now",
	} {
		if !isStatusCommand(text) {
			t.Fatalf("expected %q to be recognized as status command", text)
		}
	}
	for _, text := range []string{
		"status",
		"/statusful",
		"/ status",
	} {
		if isStatusCommand(text) {
			t.Fatalf("expected %q to be rejected as status command", text)
		}
	}
}

func TestIsClearCommand(t *testing.T) {
	for _, text := range []string{
		"/clear",
		"  /clear  ",
		"/clear now",
	} {
		if !isClearCommand(text) {
			t.Fatalf("expected %q to be recognized as clear command", text)
		}
	}
	for _, text := range []string{
		"clear",
		"/ cleared",
		"/clearer",
	} {
		if isClearCommand(text) {
			t.Fatalf("expected %q to be rejected as clear command", text)
		}
	}
}

func TestIsStopCommand(t *testing.T) {
	for _, text := range []string{
		"/stop",
		"  /stop  ",
		"/stop now",
	} {
		if !isStopCommand(text) {
			t.Fatalf("expected %q to be recognized as stop command", text)
		}
	}
	for _, text := range []string{
		"stop",
		"/ stopping",
		"/stopper",
	} {
		if isStopCommand(text) {
			t.Fatalf("expected %q to be rejected as stop command", text)
		}
	}
}

func TestIsSessionCommand(t *testing.T) {
	for _, text := range []string{
		"/session abc",
		"  /session abc  ",
		"/session abc continue",
	} {
		if !isSessionCommand(text) {
			t.Fatalf("expected %q to be recognized as session command", text)
		}
	}
	for _, text := range []string{
		"session abc",
		"/sessionful abc",
		"/ session abc",
	} {
		if isSessionCommand(text) {
			t.Fatalf("expected %q to be rejected as session command", text)
		}
	}
}
