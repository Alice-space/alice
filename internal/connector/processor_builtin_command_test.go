package connector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/automation"
	"github.com/Alice-space/alice/internal/campaign"
	"github.com/Alice-space/alice/internal/llm"
)

type llmCallCountingStub struct {
	calls int
}

func (s *llmCallCountingStub) Run(_ context.Context, _ llm.RunRequest) (llm.RunResult, error) {
	s.calls++
	return llm.RunResult{Reply: "unexpected"}, nil
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
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected reply to contain %q, got %q", want, reply)
		}
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

func TestProcessor_StatusCommand_ListsActiveAutomationTasksAndCampaigns(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	processor.SetStatusIdentity("alice", "Alice")

	automationStore := automation.NewStore(filepath.Join(t.TempDir(), "automation.db"))
	campaignStore := campaign.NewStore(filepath.Join(t.TempDir(), "campaigns.db"))
	processor.SetStatusStores(automationStore, campaignStore)

	if _, err := automationStore.CreateTask(automation.Task{
		ID:       "task_active",
		Title:    "heartbeat",
		Scope:    automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		Route:    automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:  automation.Actor{OpenID: "ou_actor"},
		Schedule: automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 600},
		Action: automation.Action{
			Type:     automation.ActionTypeRunWorkflow,
			Workflow: "code_army",
			Prompt:   "/alice reconcile campaign camp_active",
			StateKey: "camp_active",
		},
		Status: automation.TaskStatusActive,
	}); err != nil {
		t.Fatalf("create active task failed: %v", err)
	}
	if _, err := automationStore.CreateTask(automation.Task{
		ID:       "task_paused",
		Title:    "daily report",
		Scope:    automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		Route:    automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:  automation.Actor{OpenID: "ou_actor"},
		Schedule: automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 3600},
		Action: automation.Action{
			Type:   automation.ActionTypeRunLLM,
			Prompt: "summarize current progress",
		},
		Status: automation.TaskStatusPaused,
	}); err != nil {
		t.Fatalf("create paused task failed: %v", err)
	}

	if _, err := campaignStore.CreateCampaign(campaign.Campaign{
		ID:                "camp_active",
		Title:             "Optimize Model-X",
		Objective:         "improve latency",
		Repo:              "lizhihao/fastecalsim",
		Session:           campaign.SessionRoute{ScopeKey: "chat_id:oc_chat|thread:omt_1", ReceiveIDType: "chat_id", ReceiveID: "oc_chat", ChatType: "group"},
		Creator:           campaign.Actor{OpenID: "ou_actor"},
		Status:            campaign.StatusRunning,
		MaxParallelTrials: 3,
	}); err != nil {
		t.Fatalf("create active campaign failed: %v", err)
	}
	if _, err := campaignStore.CreateCampaign(campaign.Campaign{
		ID:                "camp_done",
		Title:             "Closed Experiment",
		Objective:         "done",
		Session:           campaign.SessionRoute{ScopeKey: "chat_id:oc_chat|thread:omt_1", ReceiveIDType: "chat_id", ReceiveID: "oc_chat", ChatType: "group"},
		Creator:           campaign.Actor{OpenID: "ou_actor"},
		Status:            campaign.StatusMerged,
		MaxParallelTrials: 1,
	}); err != nil {
		t.Fatalf("create merged campaign failed: %v", err)
	}

	processor.recordSessionUsage(buildChatSceneSessionKey("chat_id", "oc_chat"), llm.Usage{
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
		"活跃 Code Army：`1`",
		"`task_active`",
		"`run_workflow/code_army`",
		"state_key `camp_active`",
		"`camp_active`",
		"status `running`",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected status reply to contain %q, got %q", want, reply)
		}
	}
	for _, unwanted := range []string{
		"task_paused",
		"camp_done",
	} {
		if strings.Contains(reply, unwanted) {
			t.Fatalf("expected status reply not to contain %q, got %q", unwanted, reply)
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

func TestIsCodeArmyStatusCommand(t *testing.T) {
	for _, text := range []string{
		"/codearmy status",
		"  /codearmy   status  ",
		"/codearmy status camp_x",
	} {
		if !isCodeArmyStatusCommand(text) {
			t.Fatalf("expected %q to be recognized as codearmy status command", text)
		}
	}
	for _, text := range []string{
		"/codearmy",
		"/codearmystat us",
		"/codearmy stats",
	} {
		if isCodeArmyStatusCommand(text) {
			t.Fatalf("expected %q to be rejected as codearmy status command", text)
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
