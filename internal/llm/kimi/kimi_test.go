package kimi

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/mcpbridge"
	"github.com/Alice-space/alice/internal/prompting"
)

func TestBuildExecArgs_UsesSessionWithoutContinue(t *testing.T) {
	args := buildExecArgs("session_123", "hello", "moonshot-v1-8k")
	if !slices.Contains(args, "--print") {
		t.Fatalf("expected --print in args, got %#v", args)
	}
	if !slices.Contains(args, "-S") || !slices.Contains(args, "session_123") {
		t.Fatalf("expected explicit session in args, got %#v", args)
	}
	if slices.Contains(args, "-C") {
		t.Fatalf("did not expect --continue when --session is present, got %#v", args)
	}
	if !slices.Contains(args, "-m") || !slices.Contains(args, "moonshot-v1-8k") {
		t.Fatalf("expected model in args, got %#v", args)
	}
}

func TestBuildPrompt_UsesPrefixOnlyForNewThread(t *testing.T) {
	runner := Runner{
		Prompts:      prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
		PromptPrefix: "你是助手Alice。",
	}

	prompt, err := runner.renderPrompt("", "你好", "", "")
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你是助手Alice。\n\n你好" {
		t.Fatalf("unexpected new-thread prompt: %q", prompt)
	}

	resumePrompt, err := runner.renderPrompt("session_123", "你好", "", "")
	if err != nil {
		t.Fatalf("render resume prompt failed: %v", err)
	}
	if resumePrompt != "你好" {
		t.Fatalf("unexpected resume prompt: %q", resumePrompt)
	}
}

func TestBuildPrompt_IgnoresPersonalityText(t *testing.T) {
	runner := Runner{
		Prompts: prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
	}
	prompt, err := runner.renderPrompt("", "你好", "pragmatic", "")
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestRunnerRunWithThreadAndProgress_UsesSessionMetadataAndProgressUpdates(t *testing.T) {
	tempDir := t.TempDir()
	shareDir := filepath.Join(tempDir, "share")
	workspaceDir := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("create workspace dir failed: %v", err)
	}
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("create share dir failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(shareDir, "kimi.json"), []byte(`{
  "work_dirs": [
    {
      "path": "`+workspaceDir+`",
      "kaos": "local",
      "last_session_id": "session-from-metadata"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write kimi metadata failed: %v", err)
	}

	scriptPath := filepath.Join(tempDir, "fake-kimi.sh")
	script := `#!/bin/sh
cat <<'EOF'
{"role":"assistant","content":[{"type":"think","think":"hidden"},{"type":"text","text":"阶段提示"}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"query\":\"kimi\"}"}}]}
{"role":"tool","content":"工具输出"}
{"role":"assistant","content":"最终答复"}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake kimi script failed: %v", err)
	}

	updates := make([]string, 0, 4)
	runner := Runner{
		Command:      scriptPath,
		Timeout:      3 * time.Second,
		WorkspaceDir: workspaceDir,
		Env: map[string]string{
			"KIMI_SHARE_DIR": shareDir,
		},
	}

	reply, nextThreadID, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"hello",
		"",
		"",
		"",
		nil,
		func(step string) {
			updates = append(updates, strings.TrimSpace(step))
		},
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if reply != "最终答复" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if nextThreadID != "session-from-metadata" {
		t.Fatalf("unexpected thread id: %q", nextThreadID)
	}
	if len(updates) != 2 {
		t.Fatalf("unexpected progress updates: %#v", updates)
	}
	if updates[0] != "阶段提示" || updates[1] != "最终答复" {
		t.Fatalf("unexpected progress updates: %#v", updates)
	}
}

func TestRunnerRunWithThreadAndProgress_UsesSessionKeyFallbackWithoutResumePrompt(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "fake-kimi.sh")
	argsFile := filepath.Join(tempDir, "args.txt")
	script := `#!/bin/sh
printf '%s\000' "$@" > "` + argsFile + `"
cat <<'EOF'
{"role":"assistant","content":"最终答复"}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake kimi script failed: %v", err)
	}

	sessionKey := "chat_id:oc_chat|thread:omt_thread_1"
	runner := Runner{
		Command:      scriptPath,
		Timeout:      3 * time.Second,
		PromptPrefix: "你是助手Alice。",
	}
	reply, nextThreadID, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"你好",
		"",
		"",
		"",
		map[string]string{mcpbridge.EnvSessionKey: sessionKey},
		nil,
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if reply != "最终答复" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if nextThreadID != sessionKey {
		t.Fatalf("unexpected thread id: %q", nextThreadID)
	}

	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	args := splitNULSeparatedArgs(rawArgs)
	if !slices.Contains(args, "-S") || !slices.Contains(args, sessionKey) {
		t.Fatalf("expected fallback session key in args, got %#v", args)
	}
	if slices.Contains(args, "-C") {
		t.Fatalf("did not expect --continue with fallback session key, got %#v", args)
	}
	if prompt := valueAfterFlag(args, "-p"); prompt != "你是助手Alice。\n\n你好" {
		t.Fatalf("unexpected prompt with fallback session key: %q", prompt)
	}
}

func TestRunnerRunWithThreadAndProgress_PreservesExistingThreadID(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "fake-kimi.sh")
	script := `#!/bin/sh
cat <<'EOF'
{"role":"assistant","content":"继续回答"}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake kimi script failed: %v", err)
	}

	runner := Runner{
		Command: scriptPath,
		Timeout: 3 * time.Second,
	}

	reply, nextThreadID, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"session-123",
		"assistant",
		"hello",
		"",
		"",
		"",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if reply != "继续回答" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if nextThreadID != "session-123" {
		t.Fatalf("unexpected thread id: %q", nextThreadID)
	}
}

func TestRunnerRunWithThreadAndProgress_ReturnsStderrOnFailure(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "fake-kimi.sh")
	script := `#!/bin/sh
printf '%s\n' "boom on stderr" >&2
exit 37
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake kimi script failed: %v", err)
	}

	runner := Runner{
		Command: scriptPath,
		Timeout: 3 * time.Second,
	}

	_, nextThreadID, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"hello",
		"",
		"",
		"",
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "boom on stderr") &&
		!strings.Contains(err.Error(), "exit status 37") {
		t.Fatalf("unexpected error: %v", err)
	}
	if nextThreadID != "" {
		t.Fatalf("unexpected thread id on failure: %q", nextThreadID)
	}
}

func TestRunnerRunWithThreadAndProgress_TimesOut(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "fake-kimi.sh")
	script := `#!/bin/sh
sleep 5
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake kimi script failed: %v", err)
	}

	runner := Runner{
		Command: scriptPath,
		Timeout: 100 * time.Millisecond,
	}

	_, _, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"hello",
		"",
		"",
		"",
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "kimi timeout") {
		t.Fatalf("unexpected timeout error: %v", err)
	}
}

func valueAfterFlag(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

func splitNULSeparatedArgs(raw []byte) []string {
	parts := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return nil
	}
	return parts
}
