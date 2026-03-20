package connector

import (
	"context"
	"strings"
	"testing"

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
	if sender.replyCardCalls != 0 {
		t.Fatalf("expected help command not to use card reply, got %d", sender.replyCardCalls)
	}
	if sender.replyRichMarkdownCalls != 1 || sender.replyRichMarkdownDirectCalls != 1 {
		t.Fatalf("expected one direct rich markdown reply, got rich=%d direct=%d", sender.replyRichMarkdownCalls, sender.replyRichMarkdownDirectCalls)
	}
	reply := sender.replyMarkdownTexts[0]
	for _, want := range []string{
		"## Alice 内建命令",
		"`/help`",
		"`普通模式`",
		"`工作模式`",
		"`#work`",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected reply to contain %q, got %q", want, reply)
		}
	}
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
