package connector

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Alice-space/alice/internal/logging"
)

func TestProcessorBuildPrompt_AppendsBotSoulForNewThread(t *testing.T) {
	soulPath := filepath.Join(t.TempDir(), "SOUL.md")
	soulText := "---\noutput_contract:\n  hidden_tags:\n    - reply_will\n    - motion\n  reply_will_tag: reply_will\n  reply_will_field: reply_will\n  motion_tag: motion\n  suppress_token: \"[[NO_REPLY]]\"\n---\n# Alice Chat\n- 回答前先澄清目标\n"
	if err := os.WriteFile(soulPath, []byte(soulText), 0o600); err != nil {
		t.Fatalf("write SOUL.md failed: %v", err)
	}

	processor := NewProcessor(codexStub{}, &senderStub{}, "", "")
	prompt := processor.buildPrompt(context.Background(), Job{
		EventID:  "evt_1",
		Text:     "帮我总结一下",
		BotName:  "Alice Chat",
		SoulPath: soulPath,
	}, "")

	if !strings.Contains(prompt, "Alice Chat") {
		t.Fatalf("expected prompt to include bot name, got %q", prompt)
	}
	if !strings.Contains(prompt, "回答前先澄清目标") {
		t.Fatalf("expected prompt to include soul content, got %q", prompt)
	}
	if strings.Contains(prompt, "output_contract:") {
		t.Fatalf("expected frontmatter to be stripped from prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "帮我总结一下") {
		t.Fatalf("expected prompt to include user text, got %q", prompt)
	}
}

func TestProcessorBuildPrompt_SkipsBotSoulForWorkScene(t *testing.T) {
	soulPath := filepath.Join(t.TempDir(), "SOUL.md")
	if err := os.WriteFile(soulPath, []byte("这段设定不应该出现在 work prompt"), 0o600); err != nil {
		t.Fatalf("write SOUL.md failed: %v", err)
	}

	processor := NewProcessor(codexStub{}, &senderStub{}, "", "")
	prompt := processor.buildPrompt(context.Background(), Job{
		EventID:  "evt_2",
		Text:     "执行任务",
		Scene:    jobSceneWork,
		BotName:  "Alice Work",
		SoulPath: soulPath,
	}, "")

	if strings.Contains(prompt, "SOUL.md") || strings.Contains(prompt, "这段设定不应该出现在 work prompt") {
		t.Fatalf("work prompt should not include soul content, got %q", prompt)
	}
	if !strings.Contains(prompt, "执行任务") {
		t.Fatalf("expected prompt to include user text, got %q", prompt)
	}
}

func TestProcessorRunLLM_LogsBackendProgress(t *testing.T) {
	origStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe failed: %v", err)
	}
	os.Stderr = writer
	defer func() {
		os.Stderr = origStderr
	}()

	logging.Configure(logging.Options{Level: "debug"})
	defer logging.Configure(logging.Options{Level: "info"})

	processor := NewProcessor(codexStreamingStub{
		resp: "final reply",
		agentMessages: []string{
			"step 1",
			"[file_change] internal/connector/processor_context.go已更改，+10-2",
		},
	}, &senderStub{}, "", "")

	var forwarded []string
	reply, nextThreadID, _, runErr := processor.runLLM(
		context.Background(),
		"thread_1",
		"user prompt",
		llmRunOptions{
			EventID:  "evt_1",
			Scene:    "chat",
			Provider: "codex",
			Model:    "gpt-5",
			Profile:  "executor",
		},
		nil,
		func(message string) {
			forwarded = append(forwarded, message)
		},
		nil,
	)
	if runErr != nil {
		t.Fatalf("runLLM returned error: %v", runErr)
	}
	if reply != "final reply" {
		t.Fatalf("expected final reply, got %q", reply)
	}
	if nextThreadID != "thread_1" {
		t.Fatalf("expected thread id fallback, got %q", nextThreadID)
	}
	if len(forwarded) != 1 || forwarded[0] != "step 1" {
		t.Fatalf("expected forwarded progress messages, got %v", forwarded)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer failed: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("read stderr failed: %v", err)
	}
	logs := buf.String()

	for _, want := range []string{
		"codex run start event_id=evt_1",
		"codex agent_message event_id=evt_1 thread_id=thread_1",
		"codex file_change event_id=evt_1 thread_id=thread_1",
		"codex run completed event_id=evt_1",
		"agent trace",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected logs to contain %q, got %q", want, logs)
		}
	}
}

func TestProcessor_BuildUserTextWithReplyContext_SkipsReplyContextForWorkScene(t *testing.T) {
	stub := &codexStub{resp: "ok"}
	sender := &senderStub{}
	processor := NewProcessor(stub, sender, "failed", "thinking")

	job := Job{
		Text:                 "继续处理",
		Scene:                jobSceneWork,
		ReplyParentMessageID: "om_parent",
		EventID:              "evt_test",
	}

	result := processor.buildUserTextWithReplyContext(context.Background(), job, "")
	if !strings.Contains(result, "继续处理") {
		t.Fatalf("expected user text in result, got %q", result)
	}
	if strings.Contains(result, "你正在回复") {
		t.Fatalf("expected NO reply context wrapper for work scene, got %q", result)
	}
}
