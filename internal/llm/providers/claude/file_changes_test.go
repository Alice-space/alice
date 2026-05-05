package claude

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Alice-space/alice/internal/llm/internal/shared"
)

func TestRunWithThreadAndProgressEmitsSyntheticFileChanges(t *testing.T) {
	repo := initClaudeGitRepo(t)
	command := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
printf '%s\n' 'hello' > edited.txt
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"DONE"}]}}'
`
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude command: %v", err)
	}

	var progress []string
	reply, _, _, _, _, err := Runner{RunnerBase: shared.RunnerBase{Command: command, WorkspaceDir: repo}}.RunWithThreadAndProgress(
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

func initClaudeGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	cmd := exec.Command("git", "-C", repo, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return repo
}
