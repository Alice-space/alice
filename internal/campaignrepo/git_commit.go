package campaignrepo

import "strings"

func gitWorktreeDirty(path string) (bool, error) {
	output, err := runGit(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// CommitRepoChanges stages and commits all current worktree changes when the
// campaign repo is dirty. It is a no-op for non-git paths or already-clean
// worktrees.
func CommitRepoChanges(root, message string) (string, bool, error) {
	root = strings.TrimSpace(root)
	if root == "" || !gitWorktreeExists(root) {
		return "", false, nil
	}
	dirty, err := gitWorktreeDirty(root)
	if err != nil || !dirty {
		return "", false, err
	}
	identityArgs, err := gitIdentityConfigArgs(root)
	if err != nil {
		return "", false, err
	}
	if _, err := runGit(root, "add", "-A"); err != nil {
		return "", false, err
	}
	dirty, err = gitWorktreeDirty(root)
	if err != nil || !dirty {
		return "", false, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "chore(campaign): update repo state"
	}
	commitArgs := append(identityArgs, "commit", "-m", message)
	if _, err := runGit(root, commitArgs...); err != nil {
		return "", false, err
	}
	head, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		return "", true, err
	}
	return strings.TrimSpace(head), true, nil
}
