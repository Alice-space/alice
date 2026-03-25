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
)

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
		_, _, err := runner.RunWithThreadAndProgress(ctx, "", "assistant", "hello", ExecPolicyConfig{}, "", "", "", "", "", "", nil, nil)
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
