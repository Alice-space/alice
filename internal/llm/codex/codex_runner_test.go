package codex

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/prompting"
)

func TestRunnerRunWithThreadAndProgress_UsesDefaultModelAndReasoningEffort(t *testing.T) {
	tempDir := t.TempDir()
	fakeCodexPath := filepath.Join(tempDir, "fake-codex.sh")
	argsFile := filepath.Join(tempDir, "args.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" > "` + argsFile + `"
cat <<'EOF'
{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}
EOF
`
	if err := os.WriteFile(fakeCodexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex script failed: %v", err)
	}

	runner := Runner{
		Command:                fakeCodexPath,
		Timeout:                3 * time.Second,
		DefaultModel:           "gpt-5.4",
		DefaultReasoningEffort: "medium",
	}
	reply, _, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"hello",
		ExecPolicyConfig{},
		"",
		"",
		"",
		"",
		"",
		"",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	if !slices.Contains(args, "--sandbox") || !slices.Contains(args, "workspace-write") {
		t.Fatalf("expected workspace sandbox in args, got: %#v", args)
	}
	if !slices.Contains(args, "-m") || !slices.Contains(args, "gpt-5.4") {
		t.Fatalf("expected default model in args, got: %#v", args)
	}
	if !slices.Contains(args, "-c") || !slices.Contains(args, `model_reasoning_effort="medium"`) {
		t.Fatalf("expected default reasoning effort in args, got: %#v", args)
	}
}

func TestRunnerRunWithThreadAndProgress_NewWorkSceneUsesDangerousBypass(t *testing.T) {
	tempDir := t.TempDir()
	fakeCodexPath := filepath.Join(tempDir, "fake-codex.sh")
	argsFile := filepath.Join(tempDir, "args.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" > "` + argsFile + `"
cat <<'EOF'
{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}
EOF
`
	if err := os.WriteFile(fakeCodexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex script failed: %v", err)
	}

	runner := Runner{
		Command: fakeCodexPath,
		Timeout: 3 * time.Second,
	}
	reply, _, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"hello",
		ExecPolicyConfig{Sandbox: "danger-full-access", AskForApproval: "never"},
		"",
		"",
		"",
		"",
		"",
		"",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("unexpected reply: %q", reply)
	}

	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	if !slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("expected dangerous bypass in work-scene args, got: %#v", args)
	}
	if slices.Contains(args, "--sandbox") {
		t.Fatalf("work-scene new thread should not pass sandbox when bypassing, got: %#v", args)
	}
	if slices.Contains(args, "-a") {
		t.Fatalf("work-scene new thread should not pass approval when bypassing, got: %#v", args)
	}
}

func TestBuildPrompt_NewThreadIncludesPrefix(t *testing.T) {
	runner := Runner{
		Prompts:      prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
		PromptPrefix: "你是助手Alice。",
	}
	prompt, err := runner.renderPrompt("", "你好", "", "", runner.PromptPrefix)
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你是助手Alice。\n\n你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildPrompt_NewThreadWithEmptyPrefix(t *testing.T) {
	runner := Runner{
		Prompts: prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
	}
	prompt, err := runner.renderPrompt("", "你好", "", "", "")
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildPrompt_NewThreadDoesNotInjectPersonalityText(t *testing.T) {
	runner := Runner{
		Prompts:      prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
		PromptPrefix: "你是助手Alice。",
	}
	prompt, err := runner.renderPrompt("", "你好", "friendly", "[[NO_REPLY]]", runner.PromptPrefix)
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你是助手Alice。\n\nPreferred response style/personality: friendly.\n\nIf no reply is appropriate, return exactly this token and nothing else: [[NO_REPLY]]\n\n你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildPrompt_ResumeThreadSkipsPrefix(t *testing.T) {
	runner := Runner{
		Prompts:      prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
		PromptPrefix: "你是助手Alice。",
	}
	prompt, err := runner.renderPrompt("thread_123", "你好", "", "", runner.PromptPrefix)
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你好" {
		t.Fatalf("unexpected resume prompt: %q", prompt)
	}
}

func TestRunnerRunWithThreadAndProgress_PassesPerRunEnv(t *testing.T) {
	tempDir := t.TempDir()
	fakeCodexPath := filepath.Join(tempDir, "fake-codex.sh")
	script := `#!/bin/sh
cat <<EOF
{"type":"item.completed","item":{"type":"agent_message","text":"$ALICE_TEST_ENV"}}
EOF
`
	if err := os.WriteFile(fakeCodexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex script failed: %v", err)
	}

	runner := Runner{
		Command: fakeCodexPath,
		Timeout: 3 * time.Second,
	}
	reply, _, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"hello",
		ExecPolicyConfig{},
		"",
		"",
		"",
		"",
		"",
		"",
		map[string]string{"ALICE_TEST_ENV": "env_ok"},
		nil,
	)
	if err != nil {
		t.Fatalf("run with env failed: %v", err)
	}
	if reply != "env_ok" {
		t.Fatalf("unexpected reply from env: %q", reply)
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
	if !slices.Contains(updates, "[file_change] - `internal/connector/processor.go` 已更改 (+2/-1)") {
		t.Fatalf("file change should be synced to updates, got: %#v", updates)
	}
	if !slices.Contains(updates, "最终答复") {
		t.Fatalf("final agent message should be synced, got: %#v", updates)
	}
}
