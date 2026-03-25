package claude

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

func TestParseFinalMessage(t *testing.T) {
	output := `not-json
{"type":"assistant","message":{"content":[{"type":"text","text":"first"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"final answer"}]}}
{"type":"result","is_error":false,"result":"final answer"}`

	msg, err := ParseFinalMessage(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "final answer" {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestParseFinalMessage_NoAssistantMessage(t *testing.T) {
	_, err := ParseFinalMessage(`{"type":"system","subtype":"init","session_id":"abc"}`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParseEventLine_AssistantAndResult(t *testing.T) {
	assistantEvent := parseEventLine(`{"type":"assistant","session_id":"sid_1","message":{"content":[{"type":"text","text":"阶段提示"}]}}`)
	if assistantEvent.SessionID != "sid_1" {
		t.Fatalf("unexpected session id: %q", assistantEvent.SessionID)
	}
	if assistantEvent.AssistantText != "阶段提示" {
		t.Fatalf("unexpected assistant text: %q", assistantEvent.AssistantText)
	}

	resultEvent := parseEventLine(`{"type":"result","is_error":true,"result":"","errors":["first","second"]}`)
	if !resultEvent.HasResultEvent {
		t.Fatal("expected result event")
	}
	if !resultEvent.ResultIsError {
		t.Fatal("expected result error")
	}
	if resultEvent.ResultText != "first\nsecond" {
		t.Fatalf("unexpected result text: %q", resultEvent.ResultText)
	}
	if len(resultEvent.ResultErrors) != 2 {
		t.Fatalf("unexpected result errors: %#v", resultEvent.ResultErrors)
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

func TestBuildExecArgs_ResumeThreadWithModel(t *testing.T) {
	args := buildExecArgs("session_123", "hello", "claude-sonnet-4-5")
	if !slices.Contains(args, "-p") {
		t.Fatalf("expected -p in args, got: %#v", args)
	}
	if !slices.Contains(args, "--output-format") || !slices.Contains(args, "stream-json") {
		t.Fatalf("expected stream-json output args, got: %#v", args)
	}
	if !slices.Contains(args, "--verbose") {
		t.Fatalf("expected --verbose in args, got: %#v", args)
	}
	if !slices.Contains(args, "--permission-mode") || !slices.Contains(args, "bypassPermissions") {
		t.Fatalf("expected bypass permissions args, got: %#v", args)
	}
	if !slices.Contains(args, "--resume") || !slices.Contains(args, "session_123") {
		t.Fatalf("expected resume args, got: %#v", args)
	}
	if !slices.Contains(args, "--model") || !slices.Contains(args, "claude-sonnet-4-5") {
		t.Fatalf("expected model args, got: %#v", args)
	}
	if !slices.Contains(args, "--") {
		t.Fatalf("expected option terminator, got: %#v", args)
	}
}

func TestBuildPrompt_NewThreadIncludesPrefix(t *testing.T) {
	runner := Runner{
		Prompts:      prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
		PromptPrefix: "你是助手Alice。",
	}
	prompt, err := runner.renderPrompt("", "你好", "", "")
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你是助手Alice。\n\n你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildPrompt_ResumeThreadSkipsPrefix(t *testing.T) {
	runner := Runner{
		Prompts:      prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
		PromptPrefix: "你是助手Alice。",
	}
	prompt, err := runner.renderPrompt("session_123", "你好", "", "")
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestBuildPrompt_NewThreadIgnoresPersonalityText(t *testing.T) {
	runner := Runner{
		Prompts: prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
	}
	prompt, err := runner.renderPrompt("", "你好", "friendly", "[[NO_REPLY]]")
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "Preferred response style/personality: friendly.\n\nIf no reply is appropriate, return exactly this token and nothing else: [[NO_REPLY]]\n\n你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestRunnerRunWithThreadAndProgress_PassesPerRunEnv(t *testing.T) {
	tempDir := t.TempDir()
	fakeClaudePath := filepath.Join(tempDir, "fake-claude.sh")
	script := `#!/bin/sh
cat <<EOF
{"type":"assistant","message":{"content":[{"type":"text","text":"$ALICE_TEST_ENV"}]}}
{"type":"result","is_error":false,"result":"$ALICE_TEST_ENV","session_id":"session_env"}
EOF
`
	if err := os.WriteFile(fakeClaudePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script failed: %v", err)
	}

	runner := Runner{
		Command: fakeClaudePath,
		Timeout: 3 * time.Second,
	}
	reply, nextSessionID, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"hello",
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
	if nextSessionID != "session_env" {
		t.Fatalf("unexpected session id: %q", nextSessionID)
	}
}

func TestRunnerRunWithProgress_OnlyIncludesAssistantUpdates(t *testing.T) {
	tempDir := t.TempDir()
	fakeClaudePath := filepath.Join(tempDir, "fake-claude.sh")
	script := `#!/bin/sh
cat <<'EOF'
{"type":"assistant","message":{"content":[{"type":"text","text":"阶段提示"}]}}
{"type":"progress","data":{"text":"this should be ignored"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"最终答复"}]}}
{"type":"result","is_error":false,"result":"最终答复","session_id":"session_progress"}
EOF
`
	if err := os.WriteFile(fakeClaudePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script failed: %v", err)
	}

	runner := Runner{
		Command:      fakeClaudePath,
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
		t.Fatalf("assistant update should be present, got: %#v", updates)
	}
	if !slices.Contains(updates, "最终答复") {
		t.Fatalf("final assistant update should be present, got: %#v", updates)
	}
	if slices.Contains(updates, "this should be ignored") {
		t.Fatalf("non-assistant progress should not be emitted, got: %#v", updates)
	}
}

func TestRunnerRunWithThreadAndProgress_ResultError(t *testing.T) {
	tempDir := t.TempDir()
	fakeClaudePath := filepath.Join(tempDir, "fake-claude.sh")
	script := `#!/bin/sh
cat <<'EOF'
{"type":"system","subtype":"init","session_id":"session_error"}
{"type":"result","is_error":true,"errors":["Not logged in · Please run /login"]}
EOF
`
	if err := os.WriteFile(fakeClaudePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude script failed: %v", err)
	}

	runner := Runner{
		Command: fakeClaudePath,
		Timeout: 3 * time.Second,
	}
	_, nextSessionID, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"hello",
		"",
		"",
		"",
		"",
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Not logged in") {
		t.Fatalf("unexpected error: %v", err)
	}
	if nextSessionID != "session_error" {
		t.Fatalf("unexpected session id: %q", nextSessionID)
	}
}
