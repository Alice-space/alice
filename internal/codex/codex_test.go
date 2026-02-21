package codex

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseFinalMessage(t *testing.T) {
	output := `not-json
{"type":"item.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"first"}}
{"type":"item.completed","item":{"type":"agent_message","text":"final answer"}}`

	msg, err := ParseFinalMessage(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "final answer" {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestParseFinalMessage_NoAgentMessage(t *testing.T) {
	_, err := ParseFinalMessage(`{"type":"item.completed","item":{"type":"tool_call"}}`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMergeEnv_OverridesAndAppends(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HTTPS_PROXY=http://old:7890"}
	overrides := map[string]string{
		"HTTPS_PROXY": "http://127.0.0.1:7890",
		"ALL_PROXY":   "socks5://127.0.0.1:7891",
	}

	merged := mergeEnv(base, overrides)

	if !slices.Contains(merged, "HTTPS_PROXY=http://127.0.0.1:7890") {
		t.Fatalf("expected HTTPS_PROXY override, got %#v", merged)
	}
	if !slices.Contains(merged, "ALL_PROXY=socks5://127.0.0.1:7891") {
		t.Fatalf("expected ALL_PROXY append, got %#v", merged)
	}
}

func TestParseEventLine_ThreadStarted(t *testing.T) {
	reasoning, message, fileChange, threadID := parseEventLine(`{"type":"thread.started","thread_id":"thread_123"}`)
	if reasoning != "" {
		t.Fatalf("unexpected reasoning: %q", reasoning)
	}
	if message != "" {
		t.Fatalf("unexpected message: %q", message)
	}
	if fileChange != "" {
		t.Fatalf("unexpected file change: %q", fileChange)
	}
	if threadID != "thread_123" {
		t.Fatalf("unexpected thread id: %q", threadID)
	}
}

func TestParseEventLine_FileChange(t *testing.T) {
	reasoning, message, fileChange, threadID := parseEventLine(`{"type":"item.completed","item":{"type":"file_change","path":"internal/connector/processor.go","added_lines":23,"removed_lines":34}}`)
	if reasoning != "" {
		t.Fatalf("unexpected reasoning: %q", reasoning)
	}
	if message != "" {
		t.Fatalf("unexpected message: %q", message)
	}
	if fileChange != "internal/connector/processor.go已更改，+23-34" {
		t.Fatalf("unexpected file change message: %q", fileChange)
	}
	if threadID != "" {
		t.Fatalf("unexpected thread id: %q", threadID)
	}
}

func TestBuildExecArgs_ResumeThread(t *testing.T) {
	args := buildExecArgs("thread_123", "hello")
	if !slices.Contains(args, "resume") {
		t.Fatalf("expected resume args, got: %#v", args)
	}
	if !slices.Contains(args, "thread_123") {
		t.Fatalf("expected thread id in args, got: %#v", args)
	}
	if !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("resume args should include dangerous bypass flag, got: %#v", args)
	}
	if slices.Contains(args, "--sandbox") {
		t.Fatalf("resume args should not include --sandbox, got: %#v", args)
	}
	if !slices.Contains(args, "--") {
		t.Fatalf("resume args should include option terminator, got: %#v", args)
	}
}

func TestBuildExecArgs_NewThreadUsesDangerousBypass(t *testing.T) {
	args := buildExecArgs("", "hello")
	if !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("new thread args should include dangerous bypass flag, got: %#v", args)
	}
	if slices.Contains(args, "--sandbox") {
		t.Fatalf("new thread args should not include --sandbox, got: %#v", args)
	}
	if !slices.Contains(args, "--") {
		t.Fatalf("new thread args should include option terminator, got: %#v", args)
	}
}

func TestBuildPrompt_NewThreadIncludesPrefix(t *testing.T) {
	prompt := buildPrompt("", "你是助手Alice。", "你好")
	if prompt != "你是助手Alice。\n\n你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildPrompt_NewThreadWithEmptyPrefix(t *testing.T) {
	prompt := buildPrompt("", "", "你好")
	if prompt != "你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildPrompt_ResumeThreadSkipsPrefix(t *testing.T) {
	prompt := buildPrompt("thread_123", "你是助手Alice。", "你好")
	if prompt != "你好" {
		t.Fatalf("unexpected resume prompt: %q", prompt)
	}
}

func TestRunnerRunWithProgress_OnlyIncludesAgentMessageUpdates(t *testing.T) {
	tempDir := t.TempDir()
	fakeCodexPath := filepath.Join(tempDir, "fake-codex.sh")
	script := `#!/bin/sh
cat <<'EOF'
{"type":"item.completed","item":{"type":"agent_message","text":"阶段提示"}}
{"type":"item.completed","item":{"type":"reasoning","text":"分析步骤"}}
{"type":"item.completed","item":{"type":"file_change","path":"internal/connector/processor.go","added_lines":2,"removed_lines":1}}
{"type":"item.completed","item":{"type":"agent_message","text":"最终答复"}}
EOF
`
	if err := os.WriteFile(fakeCodexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex script failed: %v", err)
	}

	runner := Runner{
		Command:      fakeCodexPath,
		Timeout:      3 * time.Second,
		PromptPrefix: "你是助手Alice。",
	}
	updates := make([]string, 0, 4)
	reply, err := runner.RunWithProgress(context.Background(), "你好", func(step string) {
		updates = append(updates, strings.TrimSpace(step))
	})
	if err != nil {
		t.Fatalf("run with progress failed: %v", err)
	}
	if reply != "最终答复" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if !slices.Contains(updates, "阶段提示") {
		t.Fatalf("agent message should be synced as progress update, got: %#v", updates)
	}
	if slices.Contains(updates, "分析步骤") {
		t.Fatalf("reasoning should not be synced to user updates, got: %#v", updates)
	}
	if !slices.Contains(updates, "[filechange] internal/connector/processor.go已更改，+2-1") {
		t.Fatalf("file change should be synced to updates, got: %#v", updates)
	}
	if !slices.Contains(updates, "最终答复") {
		t.Fatalf("final agent message should be synced, got: %#v", updates)
	}
}
