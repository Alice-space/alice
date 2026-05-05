package repodiff

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveFileChangeStatusByGit(ctx context.Context, repo, path string) FileChangeStatus {
	relPath, _, ok := resolvePathForRepo(repo, path)
	if !ok {
		return FileChangeStatusUnknown
	}
	return detectGitPathStatus(ctx, repo, relPath)
}

func detectGitPathStatus(ctx context.Context, repo, relPath string) FileChangeStatus {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "status", "--porcelain", "--", relPath)
	out, err := cmd.Output()
	if err != nil {
		return FileChangeStatusUnknown
	}

	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || len(line) < 2 {
			continue
		}
		code := line[:2]
		if code == "??" {
			return FileChangeStatusAdded
		}
		if strings.Contains(code, "D") {
			return FileChangeStatusDeleted
		}
		if strings.Contains(code, "A") {
			return FileChangeStatusAdded
		}
		if strings.ContainsAny(code, "MRCUT") {
			return FileChangeStatusModified
		}
	}
	return FileChangeStatusUnknown
}

func resolvePathForRepo(repo, path string) (relPath string, absPath string, ok bool) {
	repo = strings.TrimSpace(repo)
	path = strings.TrimSpace(path)
	if repo == "" || path == "" {
		return "", "", false
	}

	cleanRepo := filepath.Clean(repo)
	if filepath.IsAbs(path) {
		cleanAbs := filepath.Clean(path)
		rel, err := filepath.Rel(cleanRepo, cleanAbs)
		if err != nil {
			return "", "", false
		}
		rel = filepath.ToSlash(rel)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return "", "", false
		}
		return rel, cleanAbs, true
	}

	rel := filepath.ToSlash(strings.TrimPrefix(path, "./"))
	if rel == "" {
		return "", "", false
	}
	abs := filepath.Join(cleanRepo, filepath.FromSlash(rel))
	return rel, filepath.Clean(abs), true
}

func readNoIndexDiffStat(ctx context.Context, absPath string) (FileDiffStat, bool) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--numstat", "--no-index", "/dev/null", absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, isExitErr := err.(*exec.ExitError); !isExitErr {
			return FileDiffStat{}, false
		}
	}

	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || !strings.Contains(line, "\t") {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		return FileDiffStat{
			Additions: parseNumstatValue(fields[0]),
			Deletions: parseNumstatValue(fields[1]),
		}, true
	}
	return FileDiffStat{}, false
}
