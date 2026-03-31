package campaignrepo

import "strings"

const (
	campaignRepoCommitterName  = "Alice CodeArmy"
	campaignRepoCommitterEmail = "alice-codearmy@local"
)

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
	if _, err := runGit(
		root,
		"-c", "user.name="+campaignRepoCommitterName,
		"-c", "user.email="+campaignRepoCommitterEmail,
		"commit", "-m", message,
	); err != nil {
		return "", false, err
	}
	head, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		return "", true, err
	}
	return strings.TrimSpace(head), true, nil
}
