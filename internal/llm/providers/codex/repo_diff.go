package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type fileChangeStatus int

const (
	fileChangeStatusUnknown fileChangeStatus = iota
	fileChangeStatusModified
	fileChangeStatusAdded
	fileChangeStatusDeleted
)

type parsedFileChangeLine struct {
	Path     string
	Status   fileChangeStatus
	Stat     fileDiffStat
	HasStats bool
}

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
			status := resolveFileChangeStatusByGit(ctx, repo, path)
			if status == fileChangeStatusUnknown {
				status = fileChangeStatusModified
			}
			messages = append(messages, formatFileChangeMessageWithStatus(path, status, stat))
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
			absPath := filepath.Join(repo, filepath.FromSlash(path))
			if stat, found := readNoIndexDiffStat(ctx, absPath); found {
				snapshot[path] = stat
				continue
			}
			snapshot[path] = fileDiffStat{}
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
