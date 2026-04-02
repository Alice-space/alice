package campaignrepo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitRepoChanges_CommitsDirtyWorktree(t *testing.T) {
	setupIsolatedGitConfigHome(t)
	root := t.TempDir()
	initGitRepo(t, root)
	runGitOrFail(t, root, "config", "user.name", "Local Test")
	runGitOrFail(t, root, "config", "user.email", "local@example.com")

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
	author, err := runGit(root, "show", "-s", "--format=%an <%ae>", head)
	if err != nil {
		t.Fatalf("git show author failed: %v", err)
	}
	if got := strings.TrimSpace(author); got != "Local Test <local@example.com>" {
		t.Fatalf("unexpected commit author: %q", got)
	}
}

func TestCommitRepoChanges_SkipsCleanWorktree(t *testing.T) {
	setupIsolatedGitConfigHome(t)
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

func TestCommitRepoChanges_UsesGlobalIdentityWhenLocalMissing(t *testing.T) {
	setupIsolatedGitConfigHome(t)
	root := t.TempDir()
	initGitRepo(t, root)
	clearLocalGitIdentity(t, root)
	runGitOrFail(t, root, "config", "--global", "user.name", "Global Test")
	runGitOrFail(t, root, "config", "--global", "user.email", "global@example.com")

	mustWriteTestFile(t, filepath.Join(root, "notes.md"), "hello\n")
	head, committed, err := CommitRepoChanges(root, "chore(campaign): use global identity")
	if err != nil {
		t.Fatalf("commit repo changes failed: %v", err)
	}
	if !committed {
		t.Fatal("expected dirty worktree to be committed")
	}
	author, err := runGit(root, "show", "-s", "--format=%an <%ae>", head)
	if err != nil {
		t.Fatalf("git show author failed: %v", err)
	}
	if got := strings.TrimSpace(author); got != "Global Test <global@example.com>" {
		t.Fatalf("unexpected commit author: %q", got)
	}
}

func TestCommitRepoChanges_FailsWithoutGitIdentity(t *testing.T) {
	setupIsolatedGitConfigHome(t)
	root := t.TempDir()
	initGitRepo(t, root)
	clearLocalGitIdentity(t, root)

	mustWriteTestFile(t, filepath.Join(root, "notes.md"), "hello\n")
	head, committed, err := CommitRepoChanges(root, "chore(campaign): should fail")
	if err == nil {
		t.Fatal("expected missing git identity error")
	}
	var identityErr *gitIdentityError
	if !errors.As(err, &identityErr) {
		t.Fatalf("expected gitIdentityError, got %T: %v", err, err)
	}
	if identityErr.Kind != gitIdentityErrorMissing {
		t.Fatalf("expected missing identity error, got %+v", identityErr)
	}
	if committed {
		t.Fatalf("expected commit to fail before commit, got head=%q", head)
	}
	if head != "" {
		t.Fatalf("expected empty head on failure, got %q", head)
	}
}

func TestCommitRepoChanges_RejectsIncompleteLocalIdentity(t *testing.T) {
	setupIsolatedGitConfigHome(t)
	root := t.TempDir()
	initGitRepo(t, root)
	clearLocalGitIdentity(t, root)
	runGitOrFail(t, root, "config", "--global", "user.name", "Global Test")
	runGitOrFail(t, root, "config", "--global", "user.email", "global@example.com")
	runGitOrFail(t, root, "config", "user.name", "Partial Local")

	mustWriteTestFile(t, filepath.Join(root, "notes.md"), "hello\n")
	_, _, err := CommitRepoChanges(root, "chore(campaign): should reject partial local")
	if err == nil {
		t.Fatal("expected incomplete local git identity error")
	}
	var identityErr *gitIdentityError
	if !errors.As(err, &identityErr) {
		t.Fatalf("expected gitIdentityError, got %T: %v", err, err)
	}
	if identityErr.Kind != gitIdentityErrorIncomplete {
		t.Fatalf("expected incomplete identity error, got %+v", identityErr)
	}
	if identityErr.Scope != gitIdentityScopeLocal {
		t.Fatalf("expected local scope for incomplete identity, got %+v", identityErr)
	}
}

func setupIsolatedGitConfigHome(t *testing.T) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create isolated git home failed: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func clearLocalGitIdentity(t *testing.T, root string) {
	t.Helper()
	runGitOrFail(t, root, "config", "--unset-all", "user.name")
	runGitOrFail(t, root, "config", "--unset-all", "user.email")
}
