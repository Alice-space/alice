package campaignrepo

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitTaskWorktree_RejectsSharedOccupiedBranch(t *testing.T) {
	root := t.TempDir()
	sourceRoot := filepath.Join(root, "source")
	initGitRepo(t, sourceRoot)
	runGitOrFail(t, sourceRoot, "checkout", "-b", "rCM")
	baseCommit := gitHeadCommit(t, sourceRoot)

	worktreePath := filepath.Join(root, ".worktrees", "repo-a", "t001")
	err := ensureGitTaskWorktree(sourceRoot, worktreePath, "rCM", baseCommit)
	if err == nil {
		t.Fatal("expected shared occupied branch to be rejected")
	}
	if !strings.Contains(err.Error(), "task-private branch") {
		t.Fatalf("expected task-private branch guidance, got %v", err)
	}
	if !strings.Contains(err.Error(), sourceRoot) {
		t.Fatalf("expected error to mention occupied worktree path, got %v", err)
	}
}
