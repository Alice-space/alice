package codex

import (
	"slices"
	"testing"
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

func TestParseUsageLine(t *testing.T) {
	usage := parseUsageLine(`{"type":"turn.completed","usage":{"input_tokens":120,"cached_input_tokens":40,"output_tokens":8}}`)
	if usage.InputTokens != 120 {
		t.Fatalf("unexpected input tokens: %d", usage.InputTokens)
	}
	if usage.CachedInputTokens != 40 {
		t.Fatalf("unexpected cached input tokens: %d", usage.CachedInputTokens)
	}
	if usage.OutputTokens != 8 {
		t.Fatalf("unexpected output tokens: %d", usage.OutputTokens)
	}
	if usage.TotalTokens() != 128 {
		t.Fatalf("unexpected total tokens: %d", usage.TotalTokens())
	}
}

func TestParseUsageLine_IgnoresOtherEvents(t *testing.T) {
	usage := parseUsageLine(`{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}`)
	if usage.HasUsage() {
		t.Fatalf("unexpected usage for non-turn event: %#v", usage)
	}
}

func TestBuildExecArgs_ResumeThread(t *testing.T) {
	args := buildExecArgs("thread_123", "hello", "", "", "", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	approvalFlagIndex := slices.Index(args, "-a")
	sandboxFlagIndex := slices.Index(args, "--sandbox")
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
	if sandboxFlagIndex < 0 || sandboxFlagIndex > execIndex {
		t.Fatalf("sandbox mode must be passed before exec to satisfy current codex CLI parsing, got: %#v", args)
	}
	if slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("resume args should not include dangerous bypass flag, got: %#v", args)
	}
	if !slices.Contains(args, "--") {
		t.Fatalf("resume args should include option terminator, got: %#v", args)
	}
}

func TestBuildExecArgs_ResumeThreadUsesDangerousBypassForFullAccess(t *testing.T) {
	args := buildExecArgs("thread_123", "hello", "", "", "", "", ExecPolicyConfig{
		Sandbox:        "danger-full-access",
		AskForApproval: "never",
	})
	resumeIndex := slices.Index(args, "resume")
	bypassIndex := slices.Index(args, "--dangerously-bypass-approvals-and-sandbox")
	if resumeIndex < 0 {
		t.Fatalf("expected resume args, got: %#v", args)
	}
	if bypassIndex < 0 {
		t.Fatalf("resume args should use dangerous bypass for full-access sessions, got: %#v", args)
	}
	if bypassIndex < resumeIndex {
		t.Fatalf("dangerous bypass flag should be passed as a resume option, got: %#v", args)
	}
	if slices.Contains(args, "--sandbox") {
		t.Fatalf("resume args should not pass sandbox flag alongside dangerous bypass, got: %#v", args)
	}
	if slices.Contains(args, "-a") {
		t.Fatalf("resume args should not pass approval flag alongside dangerous bypass, got: %#v", args)
	}
}

func TestBuildExecArgs_NewThreadUsesWorkspaceSandbox(t *testing.T) {
	args := buildExecArgs("", "hello", "", "", "", "", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
	})
	approvalFlagIndex := slices.Index(args, "-a")
	sandboxFlagIndex := slices.Index(args, "--sandbox")
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
	if sandboxFlagIndex < 0 || sandboxFlagIndex > execIndex {
		t.Fatalf("sandbox mode must be passed before exec to satisfy current codex CLI parsing, got: %#v", args)
	}
	if slices.Contains(args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("new thread args should not include dangerous bypass flag, got: %#v", args)
	}
	if !slices.Contains(args, "--") {
		t.Fatalf("new thread args should include option terminator, got: %#v", args)
	}
}

func TestBuildExecArgs_NewThreadUsesDangerousBypassForFullAccess(t *testing.T) {
	args := buildExecArgs("", "hello", "", "", "", "", ExecPolicyConfig{
		Sandbox:        "danger-full-access",
		AskForApproval: "never",
	})
	execIndex := slices.Index(args, "exec")
	bypassIndex := slices.Index(args, "--dangerously-bypass-approvals-and-sandbox")
	if execIndex < 0 {
		t.Fatalf("expected exec in args, got: %#v", args)
	}
	if bypassIndex < 0 {
		t.Fatalf("new thread args should use dangerous bypass for full-access sessions, got: %#v", args)
	}
	if bypassIndex < execIndex {
		t.Fatalf("dangerous bypass flag should be passed as an exec option, got: %#v", args)
	}
	if slices.Contains(args, "--sandbox") {
		t.Fatalf("new thread args should not pass sandbox flag alongside dangerous bypass, got: %#v", args)
	}
	if slices.Contains(args, "-a") {
		t.Fatalf("new thread args should not pass approval flag alongside dangerous bypass, got: %#v", args)
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
	addDirIndex := slices.Index(args, "--add-dir")
	execIndex := slices.Index(args, "exec")
	if !slices.Contains(args, "--add-dir") {
		t.Fatalf("expected add-dir flags, got: %#v", args)
	}
	if !slices.Contains(args, "/tmp/resources") || !slices.Contains(args, "/tmp/assets") {
		t.Fatalf("expected add-dir paths, got: %#v", args)
	}
	if addDirIndex < 0 || addDirIndex > execIndex {
		t.Fatalf("add-dir flags must be passed before exec to satisfy current codex CLI parsing, got: %#v", args)
	}
}

func TestBuildExecArgs_ResumeThreadPlacesRootFlagsBeforeExec(t *testing.T) {
	args := buildExecArgs("thread_123", "hello", "gpt-5.4", "worker-cheap", "high", "pragmatic", ExecPolicyConfig{
		Sandbox:        "workspace-write",
		AskForApproval: "never",
		AddDirs:        []string{"/tmp/resources"},
	})
	execIndex := slices.Index(args, "exec")
	if execIndex < 0 {
		t.Fatalf("expected exec in args, got: %#v", args)
	}

	rootFlags := []string{"-a", "--sandbox", "--add-dir", "-m", "-p", "-c"}
	for _, flag := range rootFlags {
		flagIndex := slices.Index(args, flag)
		if flagIndex < 0 {
			t.Fatalf("expected %s in args, got: %#v", flag, args)
		}
		if flagIndex > execIndex {
			t.Fatalf("expected %s before exec to satisfy current codex CLI parsing, got: %#v", flag, args)
		}
	}
}
