package gemini

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

func TestBuildExecArgs_ResumeThreadWithModel(t *testing.T) {
	args := buildExecArgs("session_123", "hello", "gemini-2.5-flash")
	if !slices.Contains(args, "-p") || !slices.Contains(args, "hello") {
		t.Fatalf("expected prompt args, got %#v", args)
	}
	if !slices.Contains(args, "--output-format") || !slices.Contains(args, "json") {
		t.Fatalf("expected json output args, got %#v", args)
	}
	if !slices.Contains(args, "--model") || !slices.Contains(args, "gemini-2.5-flash") {
		t.Fatalf("expected model args, got %#v", args)
	}
	if !slices.Contains(args, "--resume") || !slices.Contains(args, "session_123") {
		t.Fatalf("expected resume args, got %#v", args)
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

func TestBuildPrompt_ResumeThreadSkipsPrefix(t *testing.T) {
	runner := Runner{
		Prompts:      prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
		PromptPrefix: "你是助手Alice。",
	}
	prompt, err := runner.renderPrompt("session_123", "你好", "", "", runner.PromptPrefix)
	if err != nil {
		t.Fatalf("render prompt failed: %v", err)
	}
	if prompt != "你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestParseJSONResponse(t *testing.T) {
	response, err := parseJSONResponse(`{
  "session_id": "session_123",
  "response": "最终答复"
}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response.SessionID != "session_123" {
		t.Fatalf("unexpected session id: %q", response.SessionID)
	}
	if response.Response != "最终答复" {
		t.Fatalf("unexpected response: %q", response.Response)
	}
}

func TestParseJSONResponse_NoResponse(t *testing.T) {
	_, err := parseJSONResponse(`{"session_id":"session_123"}`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRunnerRunWithThreadAndProgress_ParsesJSONAndProgress(t *testing.T) {
	tempDir := t.TempDir()
	fakeGeminiPath := filepath.Join(tempDir, "fake-gemini.sh")
	argsFile := filepath.Join(tempDir, "args.txt")
	script := `#!/bin/sh
printf '%s\000' "$@" > "` + argsFile + `"
cat <<'EOF'
{
  "session_id": "session_gemini",
  "response": "最终答复"
}
EOF
`
	if err := os.WriteFile(fakeGeminiPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gemini script failed: %v", err)
	}

	updates := make([]string, 0, 2)
	runner := Runner{
		Command:      fakeGeminiPath,
		Timeout:      3 * time.Second,
		PromptPrefix: "你是助手Alice。",
	}
	reply, nextThreadID, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"",
		"assistant",
		"你好",
		"gemini-2.5-flash",
		"",
		"",
		"",
		map[string]string{"ALICE_TEST_ENV": "env_ok"},
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
	if nextThreadID != "session_gemini" {
		t.Fatalf("unexpected thread id: %q", nextThreadID)
	}
	if len(updates) != 1 || updates[0] != "最终答复" {
		t.Fatalf("unexpected progress updates: %#v", updates)
	}

	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	args := splitNULSeparatedArgs(rawArgs)
	if !slices.Contains(args, "--model") || !slices.Contains(args, "gemini-2.5-flash") {
		t.Fatalf("expected model args, got %#v", args)
	}
	if prompt := valueAfterFlag(args, "-p"); prompt != "你是助手Alice。\n\n你好" {
		t.Fatalf("unexpected prompt: %q", prompt)
	}
}

func TestRunnerRunWithThreadAndProgress_PreservesResumeThreadID(t *testing.T) {
	tempDir := t.TempDir()
	fakeGeminiPath := filepath.Join(tempDir, "fake-gemini.sh")
	argsFile := filepath.Join(tempDir, "args.txt")
	script := `#!/bin/sh
printf '%s\000' "$@" > "` + argsFile + `"
cat <<'EOF'
{
  "session_id": "",
  "response": "继续答复"
}
EOF
`
	if err := os.WriteFile(fakeGeminiPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gemini script failed: %v", err)
	}

	runner := Runner{
		Command:      fakeGeminiPath,
		Timeout:      3 * time.Second,
		PromptPrefix: "你是助手Alice。",
	}
	reply, nextThreadID, err := runner.RunWithThreadAndProgress(
		context.Background(),
		"session_existing",
		"assistant",
		"你好",
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
	if reply != "继续答复" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if nextThreadID != "session_existing" {
		t.Fatalf("unexpected thread id: %q", nextThreadID)
	}

	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args failed: %v", err)
	}
	args := splitNULSeparatedArgs(rawArgs)
	if !slices.Contains(args, "--resume") || !slices.Contains(args, "session_existing") {
		t.Fatalf("expected resume args, got %#v", args)
	}
	if prompt := valueAfterFlag(args, "-p"); prompt != "你好" {
		t.Fatalf("unexpected resume prompt: %q", prompt)
	}
}

func TestDecorateNodeVersionError(t *testing.T) {
	err := decorateNodeVersionError(
		context.DeadlineExceeded,
		"SyntaxError: Invalid regular expression flags\nNode.js v18.20.8",
	)
	if !strings.Contains(err.Error(), "Node >= 20") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func splitNULSeparatedArgs(raw []byte) []string {
	text := strings.TrimSuffix(string(raw), "\x00")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\x00")
}

func valueAfterFlag(args []string, flag string) string {
	for idx := range args {
		if args[idx] != flag || idx+1 >= len(args) {
			continue
		}
		return args[idx+1]
	}
	return ""
}
