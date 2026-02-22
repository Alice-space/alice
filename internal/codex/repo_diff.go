package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func discoverWatchRepos(workspaceDir string) []string {
	workspaceDir = strings.TrimSpace(workspaceDir)
	if workspaceDir == "" {
		if wd, err := os.Getwd(); err == nil {
			workspaceDir = strings.TrimSpace(wd)
		}
	}
	if workspaceDir == "" {
		return nil
	}

	repoSet := make(map[string]struct{}, 2)
	tryAdd := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		abs, err := filepath.Abs(dir)
		if err == nil {
			dir = abs
		}
		if !isGitRepo(dir) {
			return
		}
		repoSet[dir] = struct{}{}
	}

	tryAdd(workspaceDir)
	tryAdd(filepath.Join(workspaceDir, "alice"))

	repos := make([]string, 0, len(repoSet))
	for repo := range repoSet {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	return repos
}

func isGitRepo(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func captureRepoSnapshots(ctx context.Context, repos []string) map[string]repoDiffSnapshot {
	snapshots := make(map[string]repoDiffSnapshot, len(repos))
	for _, repo := range repos {
		snapshot, err := readRepoDiffSnapshot(ctx, repo)
		if err != nil {
			continue
		}
		snapshots[repo] = snapshot
	}
	return snapshots
}

func collectRepoDiffMessages(
	ctx context.Context,
	repos []string,
	previous map[string]repoDiffSnapshot,
) ([]string, map[string]repoDiffSnapshot) {
	if previous == nil {
		previous = make(map[string]repoDiffSnapshot, len(repos))
	}
	if len(repos) == 0 {
		return nil, previous
	}

	messages := make([]string, 0, 4)
	for _, repo := range repos {
		current, err := readRepoDiffSnapshot(ctx, repo)
		if err != nil {
			continue
		}
		prior := previous[repo]
		changedPaths := diffSnapshotPaths(prior, current)
		for _, path := range changedPaths {
			stat, ok := current[path]
			if !ok {
				continue
			}
			messages = append(messages, formatFileChangeMessage(path, stat))
		}
		previous[repo] = current
	}
	return messages, previous
}

func diffSnapshotPaths(previous, current repoDiffSnapshot) []string {
	if len(current) == 0 {
		return nil
	}

	paths := make([]string, 0, len(current))
	for path, currentStat := range current {
		previousStat, exists := previous[path]
		if exists && previousStat == currentStat {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func readRepoDiffSnapshot(ctx context.Context, repo string) (repoDiffSnapshot, error) {
	snapshot := make(repoDiffSnapshot)

	diffCmd := exec.CommandContext(ctx, "git", "-C", repo, "diff", "--numstat", "--")
	diffOut, err := diffCmd.Output()
	if err != nil {
		return nil, err
	}
	for _, rawLine := range strings.Split(string(diffOut), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		path := strings.TrimSpace(fields[2])
		if path == "" {
			continue
		}
		snapshot[path] = fileDiffStat{
			Additions: parseNumstatValue(fields[0]),
			Deletions: parseNumstatValue(fields[1]),
		}
	}

	untrackedCmd := exec.CommandContext(ctx, "git", "-C", repo, "ls-files", "--others", "--exclude-standard")
	untrackedOut, err := untrackedCmd.Output()
	if err == nil {
		for _, rawLine := range strings.Split(string(untrackedOut), "\n") {
			path := strings.TrimSpace(rawLine)
			if path == "" {
				continue
			}
			if _, exists := snapshot[path]; exists {
				continue
			}
			snapshot[path] = fileDiffStat{Additions: 0, Deletions: 0}
		}
	}

	return snapshot, nil
}

func parseNumstatValue(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" || value == "-" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func formatFileChangeMessage(path string, stat fileDiffStat) string {
	return fmt.Sprintf("%s已更改，+%d-%d", strings.TrimSpace(path), stat.Additions, stat.Deletions)
}

func enrichFileChangeMessageStats(ctx context.Context, message string, repos []string) string {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	if len(lines) == 0 {
		return ""
	}

	updated := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		path, additions, deletions, ok := parseFormattedFileChangeLine(line)
		if !ok {
			updated = append(updated, line)
			continue
		}
		if additions != 0 || deletions != 0 {
			updated = append(updated, line)
			continue
		}

		stat, found := resolveFileChangeStatByGitDiff(ctx, repos, path)
		if found && (stat.Additions != 0 || stat.Deletions != 0) {
			updated = append(updated, formatFileChangeMessage(path, stat))
			continue
		}

		updated = append(updated, fmt.Sprintf("%s已更改", strings.TrimSpace(path)))
	}

	return strings.Join(updated, "\n")
}

func parseFormattedFileChangeLine(line string) (path string, additions int, deletions int, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", 0, 0, false
	}

	const marker = "已更改，+"
	idx := strings.LastIndex(line, marker)
	if idx < 0 {
		return "", 0, 0, false
	}
	path = strings.TrimSpace(line[:idx])
	if path == "" {
		return "", 0, 0, false
	}

	statsPart := strings.TrimSpace(line[idx+len(marker):])
	parts := strings.SplitN(statsPart, "-", 2)
	if len(parts) != 2 {
		return path, 0, 0, false
	}

	add, addErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	del, delErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if addErr != nil || delErr != nil {
		return path, 0, 0, false
	}
	return path, add, del, true
}

func resolveFileChangeStatByGitDiff(ctx context.Context, repos []string, path string) (fileDiffStat, bool) {
	path = strings.TrimSpace(path)
	if path == "" || len(repos) == 0 {
		return fileDiffStat{}, false
	}

	for _, repo := range repos {
		stat, ok := readRepoPathDiffStat(ctx, repo, path)
		if ok {
			return stat, true
		}
	}
	return fileDiffStat{}, false
}

func readRepoPathDiffStat(ctx context.Context, repo, path string) (fileDiffStat, bool) {
	relPath, absPath, ok := resolvePathForRepo(repo, path)
	if !ok {
		return fileDiffStat{}, false
	}

	if stat, found := readGitNumstatForPath(ctx, repo, relPath, false); found && (stat.Additions != 0 || stat.Deletions != 0) {
		return stat, true
	}
	if stat, found := readGitNumstatForPath(ctx, repo, relPath, true); found && (stat.Additions != 0 || stat.Deletions != 0) {
		return stat, true
	}
	if isUntrackedPath(ctx, repo, relPath) {
		if stat, found := readNoIndexDiffStat(ctx, absPath); found && (stat.Additions != 0 || stat.Deletions != 0) {
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
		stat := fileDiffStat{
			Additions: parseNumstatValue(fields[0]),
			Deletions: parseNumstatValue(fields[1]),
		}
		return stat, true
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
