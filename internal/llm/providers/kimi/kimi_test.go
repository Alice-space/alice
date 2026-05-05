package kimi

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

func TestRunWithThreadAndProgressEmitsToolUseRawEvent(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "kimi")
	script := `#!/bin/sh
printf '%s\n' '{"role":"assistant","content":[{"type":"text","text":"checking"}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"pwd","arguments":"{}"}}]}'
printf '%s\n' '{"role":"assistant","content":"DONE"}'
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake kimi command: %v", err)
	}

	var raw []string
	reply, _, err := Runner{RunnerBase: shared.RunnerBase{Command: command}}.RunWithThreadAndProgress(
		context.Background(),
		"",
		"prompt",
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

	if reply != "DONE" {
		t.Fatalf("reply = %q, want DONE", reply)
	}
	if len(raw) != 3 {
		t.Fatalf("raw events = %#v, want 3 events", raw)
	}
	if raw[0] != "stdout_line" || raw[2] != "stdout_line" {
		t.Fatalf("unexpected stdout events: %#v", raw)
	}
	if want := "tool_use:function id=`call_1` name=`pwd` arguments=`{}`"; !reflect.DeepEqual(raw[1], want) {
		t.Fatalf("tool raw event = %#v, want %#v", raw[1], want)
	}
}

func TestRunWithThreadAndProgressEmitsSyntheticFileChanges(t *testing.T) {
	repo := initKimiGitRepo(t)
	command := filepath.Join(t.TempDir(), "kimi")
	script := `#!/bin/sh
printf '%s\n' 'hello' > edited.txt
printf '%s\n' '{"role":"assistant","content":"DONE"}'
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake kimi command: %v", err)
	}

	var progress []string
	reply, _, err := Runner{RunnerBase: shared.RunnerBase{Command: command, WorkspaceDir: repo}}.RunWithThreadAndProgress(
		context.Background(),
		"",
		"prompt",
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

func initKimiGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	cmd := exec.Command("git", "-C", repo, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return repo
}
