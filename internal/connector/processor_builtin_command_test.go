package connector

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

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

func TestProcessor_StatusCommand_ListsActiveAutomationTasksAndCampaigns(t *testing.T) {
	llmStub := &llmCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")

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
		ID:                   "camp_active",
		Title:                "Optimize Model-X",
		Objective:            "improve latency",
		Repo:                 "lizhihao/fastecalsim",
		IssueIID:             "218",
		Session:              campaign.SessionRoute{ScopeKey: "chat_id:oc_chat|thread:omt_1", ReceiveIDType: "chat_id", ReceiveID: "oc_chat", ChatType: "group"},
		Creator:              campaign.Actor{OpenID: "ou_actor"},
		Status:               campaign.StatusRunning,
		MaxParallelTrials:    3,
		CurrentWinnerTrialID: "trial-1",
		Trials: []campaign.Trial{
			{ID: "trial-1", Status: campaign.TrialStatusRunning},
			{ID: "trial-2", Status: campaign.TrialStatusHold},
			{ID: "trial-3", Status: campaign.TrialStatusMerged},
		},
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
		"活跃自动化任务：`1`",
		"活跃 Code Army：`1`",
		"`task_active`",
		"`run_workflow/code_army`",
		"state_key `camp_active`",
		"`camp_active`",
		"status `running`",
		"active trials `trial-1, trial-2`",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected status reply to contain %q, got %q", want, reply)
		}
	}
	for _, unwanted := range []string{
		"task_paused",
		"camp_done",
		"trial-3",
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
