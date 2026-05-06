package opencode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

func TestRunWithThreadAndProgressEmitsIndependentTextEvents(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
printf '%s\n' '{"type":"step_start","sessionID":"ses_test"}'
printf '%s\n' '{"type":"text","part":{"text":"FIRST"}}'
printf '%s\n' '{"type":"step_finish","part":{"tokens":{"input":10,"output":5,"cache":{"read":2}}}}'
printf '%s\n' '{"type":"step_start","sessionID":"ses_test"}'
printf '%s\n' '{"type":"text","part":{"text":"SECOND"}}'
printf '%s\n' '{"type":"step_finish","part":{"tokens":{"input":12,"output":6,"cache":{"read":4}}}}'
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake opencode command: %v", err)
	}

	var progress []string
	reply, threadID, inputTokens, outputTokens, cacheTokens, err := Runner{RunnerBase: shared.RunnerBase{Command: command}}.RunWithThreadAndProgress(
		context.Background(),
		"",
		"prompt",
		"",
		"",
		nil,
		func(step string) {
			progress = append(progress, step)
		},
		nil,
	)
	if err != nil {
		t.Fatalf("RunWithThreadAndProgress returned error: %v", err)
	}

	if want := []string{"FIRST", "SECOND"}; !reflect.DeepEqual(progress, want) {
		t.Fatalf("progress = %#v, want %#v", progress, want)
	}
	if reply != "SECOND" {
		t.Fatalf("reply = %q, want %q", reply, "SECOND")
	}
	if threadID != "ses_test" {
		t.Fatalf("threadID = %q, want %q", threadID, "ses_test")
	}
	if inputTokens != 12 || outputTokens != 6 || cacheTokens != 4 {
		t.Fatalf("tokens = input:%d output:%d cache:%d, want input:12 output:6 cache:4", inputTokens, outputTokens, cacheTokens)
	}
}

func TestRunWithThreadAndProgressEmitsSyntheticFileChanges(t *testing.T) {
	repo := initOpenCodeGitRepo(t)
	command := filepath.Join(t.TempDir(), "opencode")
	script := `#!/bin/sh
printf '%s\n' 'hello' > edited.txt
printf '%s\n' '{"type":"text","part":{"text":"DONE"}}'
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake opencode command: %v", err)
	}

	var progress []string
	reply, _, _, _, _, err := Runner{RunnerBase: shared.RunnerBase{Command: command, WorkspaceDir: repo}}.RunWithThreadAndProgress(
		context.Background(),
		"",
		"prompt",
		"",
		"",
		nil,
		func(step string) {
			progress = append(progress, step)
		},
		nil,
	)
	if err != nil {
		t.Fatalf("RunWithThreadAndProgress returned error: %v", err)
	}
	if reply != "DONE" {
		t.Fatalf("reply = %q, want DONE", reply)
	}
	if len(progress) < 2 || !strings.HasPrefix(progress[0], "[file_change] ") || !strings.Contains(progress[0], "edited.txt") {
		t.Fatalf("expected first progress event to be synthetic file change, got %#v", progress)
	}
	if progress[len(progress)-1] != "DONE" {
		t.Fatalf("expected final progress to be DONE, got %#v", progress)
	}
}

func initOpenCodeGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	cmd := exec.Command("git", "-C", repo, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return repo
}

func TestRunWithThreadAndProgressEmitsRawEvents(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
printf '%s\n' '{"type":"step_start","sessionID":"ses_test"}'
printf '%s\n' '{"type":"reasoning","part":{"text":"thinking"}}'
printf '%s\n' '{"type":"tool_use","part":{"tool":"bash","callID":"call_1","state":{"status":"completed","input":{"command":"pwd"}}}}'
printf '%s\n' '{"type":"text","part":{"text":"OK"}}'
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake opencode command: %v", err)
	}

	var raw []string
	_, _, _, _, _, err := Runner{RunnerBase: shared.RunnerBase{Command: command}}.RunWithThreadAndProgress(
		context.Background(),
		"",
		"prompt",
		"",
		"",
		nil,
		nil,
		func(kind, _, detail string) {
			if detail != "" {
				raw = append(raw, kind+":"+detail)
				return
			}
			raw = append(raw, kind)
		},
	)
	if err != nil {
		t.Fatalf("RunWithThreadAndProgress returned error: %v", err)
	}

	if want := []string{
		"stdout_line",
		"stdout_line",
		"reasoning:thinking",
		"stdout_line",
		"tool_use:tool_use tool=`bash` call_id=`call_1` status=`completed` command=`pwd`",
		"stdout_line",
	}; !reflect.DeepEqual(raw, want) {
		t.Fatalf("raw events = %#v, want %#v", raw, want)
	}
}

func TestParseOpenCodeLineTextEventsAreIndependentProgressMessages(t *testing.T) {
	var (
		inputTokens  int64
		outputTokens int64
		cacheTokens  int64
		threadID     string
		finalText    string
	)

	first := parseOpenCodeLine(
		[]byte(`{"type":"text","part":{"text":"FIRST"}}`),
		&inputTokens,
		&outputTokens,
		&cacheTokens,
		&threadID,
		&finalText,
	)
	second := parseOpenCodeLine(
		[]byte(`{"type":"text","part":{"text":"SECOND"}}`),
		&inputTokens,
		&outputTokens,
		&cacheTokens,
		&threadID,
		&finalText,
	)

	if first != "FIRST" {
		t.Fatalf("first progress text = %q, want %q", first, "FIRST")
	}
	if second != "SECOND" {
		t.Fatalf("second progress text = %q, want %q", second, "SECOND")
	}
	if strings.Contains(second, first) {
		t.Fatalf("second progress text should not include first text: %q", second)
	}
	if finalText != "SECOND" {
		t.Fatalf("final text = %q, want last text %q", finalText, "SECOND")
	}
}

func TestParseOpenCodeLineCapturesThreadAndTokens(t *testing.T) {
	var (
		inputTokens  int64
		outputTokens int64
		cacheTokens  int64
		threadID     string
		finalText    string
	)

	parseOpenCodeLine(
		[]byte(`{"type":"step_start","sessionID":"ses_123"}`),
		&inputTokens,
		&outputTokens,
		&cacheTokens,
		&threadID,
		&finalText,
	)
	parseOpenCodeLine(
		[]byte(`{"type":"step_finish","part":{"tokens":{"input":10,"output":7,"cache":{"read":3}}}}`),
		&inputTokens,
		&outputTokens,
		&cacheTokens,
		&threadID,
		&finalText,
	)

	if threadID != "ses_123" {
		t.Fatalf("threadID = %q, want %q", threadID, "ses_123")
	}
	if inputTokens != 10 || outputTokens != 7 || cacheTokens != 3 {
		t.Fatalf("tokens = input:%d output:%d cache:%d, want input:10 output:7 cache:3", inputTokens, outputTokens, cacheTokens)
	}
}
