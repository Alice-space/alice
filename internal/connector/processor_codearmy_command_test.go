package connector

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitee.com/alicespace/alice/internal/automation"
	"gitee.com/alicespace/alice/internal/codearmy"
	"gitee.com/alicespace/alice/internal/llm"
)

type codexCallCountingStub struct {
	calls int
}

func (c *codexCallCountingStub) Run(_ context.Context, _ llm.RunRequest) (llm.RunResult, error) {
	c.calls++
	return llm.RunResult{Reply: "unexpected llm call"}, nil
}

func TestProcessor_CodeArmyStatusCommand_ListsActiveTasksAndStates(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "code_army")
	sessionDir := filepath.Join(stateDir, "chat_id_oc_chat")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir failed: %v", err)
	}

	raw, err := json.Marshal(map[string]any{
		"version":       1,
		"workflow":      "code_army",
		"key":           "rust-cli-calculator",
		"session_key":   "chat_id:oc_chat",
		"task_id":       "task_existing",
		"phase":         "manager",
		"iteration":     2,
		"last_decision": "pass",
		"objective":     "推进 code army",
		"updated_at":    time.Date(2026, 3, 3, 8, 15, 47, 0, time.UTC),
		"history": []map[string]any{
			{
				"at":       time.Date(2026, 3, 3, 7, 59, 21, 0, time.UTC),
				"phase":    "worker",
				"summary":  "完成 rust 终端计算器初版",
				"decision": "",
			},
			{
				"at":       time.Date(2026, 3, 3, 8, 14, 26, 0, time.UTC),
				"phase":    "reviewer",
				"summary":  "reviewer 通过，建议补两个测试",
				"decision": "pass",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal state failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "rust-cli-calculator.json"), raw, 0o644); err != nil {
		t.Fatalf("write state file failed: %v", err)
	}

	store := automation.NewStore(filepath.Join(t.TempDir(), "automation_state.json"))
	_, err = store.CreateTask(automation.Task{
		Scope:    automation.Scope{Kind: automation.ScopeKindChat, ID: "oc_chat"},
		Route:    automation.Route{ReceiveIDType: "chat_id", ReceiveID: "oc_chat"},
		Creator:  automation.Actor{OpenID: "ou_actor"},
		Schedule: automation.Schedule{Type: automation.ScheduleTypeInterval, EverySeconds: 60},
		Action: automation.Action{
			Type:       automation.ActionTypeRunWorkflow,
			Workflow:   automation.WorkflowCodeArmy,
			StateKey:   "rust-cli-calculator",
			SessionKey: "chat_id:oc_chat",
			Prompt:     "继续推进 rust-cli-calculator",
		},
		Status: automation.TaskStatusActive,
	})
	if err != nil {
		t.Fatalf("create task failed: %v", err)
	}

	llmStub := &codexCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	processor.SetCodeArmyCommandDependencies(codearmy.NewInspector(stateDir), store)

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		ChatType:        "group",
		SenderOpenID:    "ou_actor",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|message:om_root",
		Text:            "/codearmy status",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected builtin command to bypass llm, got %d llm calls", llmStub.calls)
	}
	if sender.replyCardCalls != 1 {
		t.Fatalf("expected one direct reply card, got %d", sender.replyCardCalls)
	}
	if sender.replyTargets[0] != "om_src" {
		t.Fatalf("expected reply to source message, got %#v", sender.replyTargets)
	}
	reply := sender.replyCards[0]
	for _, want := range []string{
		"## Code Army 状态",
		"### 运行中的任务",
		"### 工作流快照",
		"`rust-cli-calculator`",
		"`phase`: `manager · 规划`",
		"`iteration`: `2`",
		"`last_decision`: `pass · 通过`",
		"Asia/Shanghai",
		"最近记录",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected reply to contain %q, got %q", want, reply)
		}
	}
}

func TestParseCodeArmyCommand_RequiresSlashCommand(t *testing.T) {
	for _, text := range []string{
		"/codearmy status",
		"/codearmy status rust-cli-calculator",
	} {
		cmd, ok := parseCodeArmyCommand(text)
		if !ok {
			t.Fatalf("expected command %q to parse", text)
		}
		if cmd.action != "status" {
			t.Fatalf("expected status action for %q, got %q", text, cmd.action)
		}
	}
	for _, text := range []string{
		"codearmy status",
		"codearmy status rust-cli-calculator",
	} {
		if _, ok := parseCodeArmyCommand(text); ok {
			t.Fatalf("expected command %q to be rejected without slash prefix", text)
		}
	}
}

func TestProcessor_CodeArmyStatusCommand_NoStateOrTask(t *testing.T) {
	llmStub := &codexCallCountingStub{}
	sender := &senderStub{}
	processor := NewProcessor(llmStub, sender, "failed", "thinking")
	processor.SetCodeArmyCommandDependencies(codearmy.NewInspector(filepath.Join(t.TempDir(), "code_army")), nil)

	state := processor.ProcessJobState(context.Background(), Job{
		ReceiveID:       "oc_chat",
		ReceiveIDType:   "chat_id",
		ChatType:        "group",
		SenderOpenID:    "ou_actor",
		SourceMessageID: "om_src",
		SessionKey:      "chat_id:oc_chat|message:om_root",
		Text:            "/codearmy status",
	})
	if state != JobProcessCompleted {
		t.Fatalf("expected completed state, got %s", state)
	}
	if llmStub.calls != 0 {
		t.Fatalf("expected builtin command to bypass llm, got %d llm calls", llmStub.calls)
	}
	if sender.replyCardCalls != 1 {
		t.Fatalf("expected one direct reply card, got %d", sender.replyCardCalls)
	}
	if !strings.Contains(sender.replyCards[0], "当前会话暂无 `code_army` 任务或状态") {
		t.Fatalf("unexpected empty status reply: %q", sender.replyCards[0])
	}
}
