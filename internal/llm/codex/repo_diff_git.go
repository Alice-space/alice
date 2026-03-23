package codex

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

func resolveFileChangeStatByGitDiff(ctx context.Context, repos []string, path string) (fileDiffStat, bool) {
	_, stat, found := resolveFileChangeInfoByGitDiff(ctx, repos, path)
	return stat, found
}

func resolveFileChangeInfoByGitDiff(
	ctx context.Context,
	repos []string,
	path string,
) (fileChangeStatus, fileDiffStat, bool) {
	path = strings.TrimSpace(path)
	if path == "" || len(repos) == 0 {
		return fileChangeStatusUnknown, fileDiffStat{}, false
	}

	for _, repo := range repos {
		status, stat, ok := readRepoPathDiffInfo(ctx, repo, path)
		if ok {
			return status, stat, true
		}
	}
	return fileChangeStatusUnknown, fileDiffStat{}, false
}

func resolveFileChangeStatusByGit(ctx context.Context, repo, path string) fileChangeStatus {
	relPath, _, ok := resolvePathForRepo(repo, path)
	if !ok {
		return fileChangeStatusUnknown
	}
	return detectGitPathStatus(ctx, repo, relPath)
}

func detectGitPathStatus(ctx context.Context, repo, relPath string) fileChangeStatus {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "status", "--porcelain", "--", relPath)
	out, err := cmd.Output()
	if err != nil {
		return fileChangeStatusUnknown
	}

	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || len(line) < 2 {
			continue
		}
		code := line[:2]
		if code == "??" {
			return fileChangeStatusAdded
		}
		if strings.Contains(code, "D") {
			return fileChangeStatusDeleted
		}
		if strings.Contains(code, "A") {
			return fileChangeStatusAdded
		}
		if strings.ContainsAny(code, "MRCUT") {
			return fileChangeStatusModified
		}
	}
	return fileChangeStatusUnknown
}

func readRepoPathDiffInfo(ctx context.Context, repo, path string) (fileChangeStatus, fileDiffStat, bool) {
	relPath, absPath, ok := resolvePathForRepo(repo, path)
	if !ok {
		return fileChangeStatusUnknown, fileDiffStat{}, false
	}

	status := detectGitPathStatus(ctx, repo, relPath)
	stat, statFound := readRepoPathDiffStat(ctx, repo, relPath)
	if !statFound && status == fileChangeStatusAdded {
		if untrackedStat, found := readNoIndexDiffStat(ctx, absPath); found {
			stat = untrackedStat
			statFound = true
		}
	}

	if status == fileChangeStatusUnknown {
		if statFound {
			status = fileChangeStatusModified
		} else {
			return fileChangeStatusUnknown, fileDiffStat{}, false
		}
	}
	return status, stat, true
}

func readRepoPathDiffStat(ctx context.Context, repo, path string) (fileDiffStat, bool) {
	relPath, absPath, ok := resolvePathForRepo(repo, path)
	if !ok {
		return fileDiffStat{}, false
	}

	if stat, found := readGitNumstatForPath(ctx, repo, relPath, false); found {
		return stat, true
	}
	if stat, found := readGitNumstatForPath(ctx, repo, relPath, true); found {
		return stat, true
	}
	if isUntrackedPath(ctx, repo, relPath) {
		if stat, found := readNoIndexDiffStat(ctx, absPath); found {
			return stat, true
		}
	}
	return fileDiffStat{}, false
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

func readGitNumstatForPath(ctx context.Context, repo, relPath string, cached bool) (fileDiffStat, bool) {
	args := []string{"-C", repo, "diff"}
	if cached {
		args = append(args, "--cached")
	}
	args = append(args, "--numstat", "--", relPath)

	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return fileDiffStat{}, false
	}

	targetRel := filepath.ToSlash(strings.TrimSpace(relPath))
	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		diffPath := filepath.ToSlash(strings.TrimSpace(fields[2]))
		if diffPath != targetRel {
			continue
		}
		return fileDiffStat{
			Additions: parseNumstatValue(fields[0]),
			Deletions: parseNumstatValue(fields[1]),
		}, true
	}
	return fileDiffStat{}, false
}

func isUntrackedPath(ctx context.Context, repo, relPath string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "ls-files", "--others", "--exclude-standard", "--", relPath)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, rawLine := range strings.Split(string(out), "\n") {
		if filepath.ToSlash(strings.TrimSpace(rawLine)) == filepath.ToSlash(strings.TrimSpace(relPath)) {
			return true
		}
	}
	return false
}

func readNoIndexDiffStat(ctx context.Context, absPath string) (fileDiffStat, bool) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--numstat", "--no-index", "/dev/null", absPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, isExitErr := err.(*exec.ExitError); !isExitErr {
			return fileDiffStat{}, false
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
		return fileDiffStat{
			Additions: parseNumstatValue(fields[0]),
			Deletions: parseNumstatValue(fields[1]),
		}, true
	}
	return fileDiffStat{}, false
}
