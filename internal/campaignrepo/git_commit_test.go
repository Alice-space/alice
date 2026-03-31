package campaignrepo

import (
	"path/filepath"
	"testing"
)

func TestCommitRepoChanges_CommitsDirtyWorktree(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)

	mustWriteTestFile(t, filepath.Join(root, "notes.md"), "hello\n")
	head, committed, err := CommitRepoChanges(root, "chore(campaign): test commit")
	if err != nil {
		t.Fatalf("commit repo changes failed: %v", err)
	}
	if !committed {
		t.Fatal("expected dirty worktree to be committed")
	}
	if head == "" {
		t.Fatal("expected non-empty commit hash")
	}
	if dirty, err := gitWorktreeDirty(root); err != nil {
		t.Fatalf("check dirty worktree failed: %v", err)
	} else if dirty {
		t.Fatal("expected clean worktree after commit")
	}
}

func TestCommitRepoChanges_SkipsCleanWorktree(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)

	head, committed, err := CommitRepoChanges(root, "chore(campaign): should skip")
	if err != nil {
		t.Fatalf("commit repo changes failed: %v", err)
	}
	if committed {
		t.Fatalf("expected no commit for clean worktree, got head=%q", head)
	}
	if head != "" {
		t.Fatalf("expected empty head for skipped commit, got %q", head)
	}
}
