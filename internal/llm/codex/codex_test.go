package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Alice-space/alice/internal/prompting"
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
	if fileChange != "- `internal/connector/processor.go` 已更改 (+23/-34)" {
		t.Fatalf("unexpected file change message: %q", fileChange)
	}
	if threadID != "" {
		t.Fatalf("unexpected thread id: %q", threadID)
	}
}

func TestParseEventLine_FileChangeWithChangesArray(t *testing.T) {
	_, _, fileChange, _ := parseEventLine(`{"type":"item.completed","item":{"id":"item_28","type":"file_change","changes":[{"path":"/home/codexbot/alice/internal/llm/codex/codex.go","kind":"update"}],"status":"completed"}}`)
	if fileChange != "- `internal/llm/codex/codex.go` 已更改" {
		t.Fatalf("unexpected file change message from changes array: %q", fileChange)
	}
}

func TestParseEventLine_FileChangeDetectsAddedAndDeleted(t *testing.T) {
	_, _, added, _ := parseEventLine(`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"new.txt","kind":"create"}]}}`)
	if added != "- `new.txt` 已新增" {
		t.Fatalf("unexpected added file change message: %q", added)
	}

	_, _, deleted, _ := parseEventLine(`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"old.txt","kind":"delete"}]}}`)
	if deleted != "- `old.txt` 已删除" {
		t.Fatalf("unexpected deleted file change message: %q", deleted)
	}
}

func TestIsSuccessfulCommandExecutionCompleted(t *testing.T) {
	success := `{"type":"item.completed","item":{"type":"command_execution","command":"echo ok","exit_code":0,"status":"completed"}}`
	if !isSuccessfulCommandExecutionCompleted(success) {
		t.Fatal("expected successful command_execution completion")
	}

	failed := `{"type":"item.completed","item":{"type":"command_execution","command":"false","exit_code":1,"status":"failed"}}`
	if isSuccessfulCommandExecutionCompleted(failed) {
		t.Fatal("failed command_execution should not be treated as successful completion")
	}
}

func TestBuildExecArgs_ResumeThread(t *testing.T) {
	args := buildExecArgs("thread_123", "hello", "", "", "", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	approvalFlagIndex := slices.Index(args, "-a")
	execIndex := slices.Index(args, "exec")
	if !slices.Contains(args, "resume") {
		t.Fatalf("expected resume args, got: %#v", args)
	}
	if !slices.Contains(args, "thread_123") {
		t.Fatalf("expected thread id in args, got: %#v", args)
	}
	if !slices.Contains(args, "--sandbox") || !slices.Contains(args, "workspace-write") {
		t.Fatalf("resume args should include workspace sandbox flag, got: %#v", args)
	}
	if approvalFlagIndex < 0 || !slices.Contains(args, "never") {
		t.Fatalf("resume args should include approval mode, got: %#v", args)
	}
	if execIndex < 0 || approvalFlagIndex > execIndex {
		t.Fatalf("approval mode must be passed before exec to satisfy current codex CLI parsing, got: %#v", args)
	}
	if slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("resume args should not include dangerous bypass flag, got: %#v", args)
	}
	if !slices.Contains(args, "--") {
		t.Fatalf("resume args should include option terminator, got: %#v", args)
	}
}

func TestBuildExecArgs_NewThreadUsesWorkspaceSandbox(t *testing.T) {
	args := buildExecArgs("", "hello", "", "", "", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	approvalFlagIndex := slices.Index(args, "-a")
	execIndex := slices.Index(args, "exec")
	if !slices.Contains(args, "--sandbox") || !slices.Contains(args, "workspace-write") {
		t.Fatalf("new thread args should include workspace sandbox flag, got: %#v", args)
	}
	if approvalFlagIndex < 0 || !slices.Contains(args, "never") {
		t.Fatalf("new thread args should include approval mode, got: %#v", args)
	}
	if execIndex < 0 || approvalFlagIndex > execIndex {
		t.Fatalf("approval mode must be passed before exec to satisfy current codex CLI parsing, got: %#v", args)
	}
	if slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("new thread args should not include dangerous bypass flag, got: %#v", args)
	}
	if !slices.Contains(args, "--") {
		t.Fatalf("new thread args should include option terminator, got: %#v", args)
	}
}

func TestBuildExecArgs_WithModelAndProfile(t *testing.T) {
	args := buildExecArgs("thread_123", "hello", "gpt-4.1-mini", "worker-cheap", "", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	if !slices.Contains(args, "-m") || !slices.Contains(args, "gpt-4.1-mini") {
		t.Fatalf("expected model selector in args, got: %#v", args)
	}
	if !slices.Contains(args, "-p") || !slices.Contains(args, "worker-cheap") {
		t.Fatalf("expected profile selector in args, got: %#v", args)
	}
}

func TestBuildExecArgs_WithReasoningEffort(t *testing.T) {
	args := buildExecArgs("thread_123", "hello", "gpt-5.4", "", "high", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	if !slices.Contains(args, "-c") || !slices.Contains(args, `model_reasoning_effort="high"`) {
		t.Fatalf("expected reasoning effort override in args, got: %#v", args)
	}
}

func TestBuildExecArgs_WithPersonality(t *testing.T) {
	args := buildExecArgs("thread_123", "hello", "gpt-5.4", "", "", "pragmatic", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	if !slices.Contains(args, "-c") || !slices.Contains(args, `personality="pragmatic"`) {
		t.Fatalf("expected personality override in args, got: %#v", args)
	}
}

func TestBuildExecArgs_WithAddDirs(t *testing.T) {
	args := buildExecArgs("", "hello", "", "", "", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
		AddDirs:        []string{"/tmp/resources", "/tmp/assets"},
	})
	if !slices.Contains(args, "--add-dir") {
		t.Fatalf("expected add-dir flags, got: %#v", args)
	}
	if !slices.Contains(args, "/tmp/resources") || !slices.Contains(args, "/tmp/assets") {
		t.Fatalf("expected add-dir paths, got: %#v", args)
	}
}

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

func TestBuildPrompt_NewThreadWithEmptyPrefix(t *testing.T) {
	runner := Runner{
		Prompts: prompting.NewLoader(filepath.Join("..", "..", "..", "prompts")),
	}
	prompt, err := runner.renderPrompt("", "你好", "", "")
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
	prompt, err := runner.renderPrompt("", "你好", "friendly", "[[NO_REPLY]]")
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
	prompt, err := runner.renderPrompt("thread_123", "你好", "", "")
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

func TestRunnerCancelStopsChildProcessGroup(t *testing.T) {
	tempDir := t.TempDir()
	fakeCodexPath := filepath.Join(tempDir, "fake-codex.sh")
	script := `#!/bin/sh
echo '{"type":"thread.started","thread_id":"thread_cancel"}'
sleep 5 &
wait $!
`
	if err := os.WriteFile(fakeCodexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex script failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := Runner{
		Command: fakeCodexPath,
		Timeout: 10 * time.Second,
	}

	done := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		_, _, err := runner.RunWithThreadAndProgress(ctx, "", "assistant", "hello", "", "", "", "", "", "", nil, nil)
		done <- err
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runner did not stop promptly after cancel, elapsed=%s", time.Since(startedAt))
	}
}

func TestRunnerRunWithProgress_SynthesizesFileChangeFromRepoDiff(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir failed: %v", err)
	}
	runInRepo := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v output=%s", args, err, string(out))
		}
	}
	runInRepo("git", "init")
	runInRepo("git", "config", "user.email", "bot@example.com")
	runInRepo("git", "config", "user.name", "Bot")
	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write initial file failed: %v", err)
	}
	runInRepo("git", "add", "a.txt")
	runInRepo("git", "commit", "-m", "init")

	fakeCodexPath := filepath.Join(tempDir, "fake-codex.sh")
	script := `#!/bin/sh
cat <<'EOF'
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"edit","aggregated_output":"","exit_code":null,"status":"in_progress"}}
EOF
printf 'new\n' > a.txt
cat <<'EOF'
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"edit","aggregated_output":"","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"完成"}}
EOF
`
	if err := os.WriteFile(fakeCodexPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex script failed: %v", err)
	}

	runner := Runner{
		Command:      fakeCodexPath,
		Timeout:      3 * time.Second,
		WorkspaceDir: repoDir,
	}
	updates := make([]string, 0, 4)
	reply, err := runner.RunWithProgress(context.Background(), "请修改 a.txt", func(step string) {
		updates = append(updates, strings.TrimSpace(step))
	})
	if err != nil {
		t.Fatalf("run with progress failed: %v", err)
	}
	if reply != "完成" {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if !slices.Contains(updates, "[file_change] - `a.txt` 已更改 (+1/-1)") {
		t.Fatalf("expected synthetic file_change update, got: %#v", updates)
	}
}

func TestRunnerRunWithProgress_DoesNotLeakSyntheticFileChangeAcrossConcurrentRuns(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir failed: %v", err)
	}
	runInRepo := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v output=%s", args, err, string(out))
		}
	}
	runInRepo("git", "init")
	runInRepo("git", "config", "user.email", "bot@example.com")
	runInRepo("git", "config", "user.name", "Bot")
	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write initial file failed: %v", err)
	}
	runInRepo("git", "add", "a.txt")
	runInRepo("git", "commit", "-m", "init")

	markerPath := filepath.Join(tempDir, "run2-started.flag")
	fakeCodexPath1 := filepath.Join(tempDir, "fake-codex-run1.sh")
	script1 := fmt.Sprintf(`#!/bin/sh
cat <<'EOF'
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"edit","aggregated_output":"","exit_code":null,"status":"in_progress"}}
EOF
while [ ! -f %q ]; do sleep 0.02; done
printf 'new\n' > a.txt
cat <<'EOF'
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"edit","aggregated_output":"","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"run1 完成"}}
EOF
`, markerPath)
	if err := os.WriteFile(fakeCodexPath1, []byte(script1), 0o755); err != nil {
		t.Fatalf("write fake codex script 1 failed: %v", err)
	}

	fakeCodexPath2 := filepath.Join(tempDir, "fake-codex-run2.sh")
	script2 := fmt.Sprintf(`#!/bin/sh
cat <<'EOF'
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"check","aggregated_output":"","exit_code":null,"status":"in_progress"}}
EOF
touch %q
sleep 0.2
cat <<'EOF'
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"check","aggregated_output":"","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"run2 完成"}}
EOF
`, markerPath)
	if err := os.WriteFile(fakeCodexPath2, []byte(script2), 0o755); err != nil {
		t.Fatalf("write fake codex script 2 failed: %v", err)
	}

	runner1 := Runner{
		Command:      fakeCodexPath1,
		Timeout:      5 * time.Second,
		WorkspaceDir: repoDir,
	}
	runner2 := Runner{
		Command:      fakeCodexPath2,
		Timeout:      5 * time.Second,
		WorkspaceDir: repoDir,
	}

	type runResult struct {
		reply   string
		updates []string
		err     error
	}
	resultCh := make(chan runResult, 2)
	runWithUpdates := func(runner Runner, prompt string) {
		updates := make([]string, 0, 4)
		reply, err := runner.RunWithProgress(context.Background(), prompt, func(step string) {
			updates = append(updates, strings.TrimSpace(step))
		})
		resultCh <- runResult{
			reply:   strings.TrimSpace(reply),
			updates: updates,
			err:     err,
		}
	}

	go runWithUpdates(runner1, "run1")
	time.Sleep(60 * time.Millisecond)
	go runWithUpdates(runner2, "run2")

	first := <-resultCh
	second := <-resultCh
	results := []runResult{first, second}
	for _, result := range results {
		if result.err != nil {
			t.Fatalf("concurrent run failed: %v", result.err)
		}
	}

	run2Updates := first.updates
	if first.reply == "run1 完成" {
		run2Updates = second.updates
	}
	if second.reply == "run2 完成" {
		run2Updates = second.updates
	}
	if slices.ContainsFunc(run2Updates, func(step string) bool {
		return strings.HasPrefix(strings.TrimSpace(step), fileChangeCallbackPrefix)
	}) {
		t.Fatalf("run2 should not receive synthetic file_change from concurrent run: %#v", run2Updates)
	}
}

func TestCollectRepoDiffMessages_DetectsChangedFile(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir failed: %v", err)
	}
	runInRepo := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v output=%s", args, err, string(out))
		}
	}
	runInRepo("git", "init")
	runInRepo("git", "config", "user.email", "bot@example.com")
	runInRepo("git", "config", "user.name", "Bot")
	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write initial file failed: %v", err)
	}
	runInRepo("git", "add", "a.txt")
	runInRepo("git", "commit", "-m", "init")

	repos := discoverWatchRepos(repoDir)
	if !slices.Contains(repos, repoDir) {
		t.Fatalf("expected discoverWatchRepos to include %s, got %#v", repoDir, repos)
	}

	previous := captureRepoSnapshots(context.Background(), repos)
	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write changed file failed: %v", err)
	}

	messages, _ := collectRepoDiffMessages(context.Background(), repos, previous)
	if !slices.Contains(messages, "- `a.txt` 已更改 (+1/-1)") {
		t.Fatalf("expected repo diff message, got %#v", messages)
	}
}

func TestEnrichFileChangeMessageStats_UsesGitDiffForZeroStats(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir failed: %v", err)
	}
	runInRepo := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v output=%s", args, err, string(out))
		}
	}
	runInRepo("git", "init")
	runInRepo("git", "config", "user.email", "bot@example.com")
	runInRepo("git", "config", "user.name", "Bot")
	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write initial file failed: %v", err)
	}
	runInRepo("git", "add", "a.txt")
	runInRepo("git", "commit", "-m", "init")

	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("new\nline2\n"), 0o644); err != nil {
		t.Fatalf("write changed file failed: %v", err)
	}

	got := enrichFileChangeMessageStats(context.Background(), "- `a.txt` 已更改 (+0/-0)", []string{repoDir})
	if got != "- `a.txt` 已更改 (+2/-1)" {
		t.Fatalf("unexpected enriched message: %q", got)
	}
}

func TestEnrichFileChangeMessageStats_StripsZeroStatsWhenNoDiffFound(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir failed: %v", err)
	}
	runInRepo := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v output=%s", args, err, string(out))
		}
	}
	runInRepo("git", "init")
	runInRepo("git", "config", "user.email", "bot@example.com")
	runInRepo("git", "config", "user.name", "Bot")
	if err := os.WriteFile(filepath.Join(repoDir, "a.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write initial file failed: %v", err)
	}
	runInRepo("git", "add", "a.txt")
	runInRepo("git", "commit", "-m", "init")

	got := enrichFileChangeMessageStats(context.Background(), "- `a.txt` 已更改 (+0/-0)", []string{repoDir})
	if got != "- `a.txt` 已更改" {
		t.Fatalf("unexpected fallback message: %q", got)
	}
}

func TestEnrichFileChangeMessageStats_UsesNoIndexDiffForUntrackedFile(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir failed: %v", err)
	}
	runInRepo := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v output=%s", args, err, string(out))
		}
	}
	runInRepo("git", "init")
	runInRepo("git", "config", "user.email", "bot@example.com")
	runInRepo("git", "config", "user.name", "Bot")

	if err := os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatalf("write untracked file failed: %v", err)
	}

	got := enrichFileChangeMessageStats(context.Background(), "- `new.txt` 已更改 (+0/-0)", []string{repoDir})
	if got != "- `new.txt` 已新增 (+3/-0)" {
		t.Fatalf("unexpected untracked message: %q", got)
	}
}

func TestEnrichFileChangeMessageStats_DetectsDeletedFile(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir failed: %v", err)
	}
	runInRepo := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v failed: %v output=%s", args, err, string(out))
		}
	}
	runInRepo("git", "init")
	runInRepo("git", "config", "user.email", "bot@example.com")
	runInRepo("git", "config", "user.name", "Bot")

	if err := os.WriteFile(filepath.Join(repoDir, "old.txt"), []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatalf("write tracked file failed: %v", err)
	}
	runInRepo("git", "add", "old.txt")
	runInRepo("git", "commit", "-m", "init")

	if err := os.Remove(filepath.Join(repoDir, "old.txt")); err != nil {
		t.Fatalf("delete tracked file failed: %v", err)
	}

	got := enrichFileChangeMessageStats(context.Background(), "- `old.txt` 已更改 (+0/-0)", []string{repoDir})
	if got != "- `old.txt` 已删除 (+0/-2)" {
		t.Fatalf("unexpected deleted file message: %q", got)
	}
}
