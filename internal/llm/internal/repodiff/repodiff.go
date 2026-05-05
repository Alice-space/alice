package repodiff

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

const CallbackPrefix = "[file_change] "

type FileChangeStatus int

const (
	FileChangeStatusUnknown FileChangeStatus = iota
	FileChangeStatusModified
	FileChangeStatusAdded
	FileChangeStatusDeleted
)

type FileDiffStat struct {
	Additions int
	Deletions int
}

type Snapshot map[string]FileDiffStat

type Emitter struct {
	ctx       context.Context
	repos     []string
	lease     *Lease
	snapshots map[string]Snapshot
	progress  func(string)
}

func NewEmitter(ctx context.Context, workspaceDir string, progress func(string)) *Emitter {
	if progress == nil {
		return nil
	}
	repos := DiscoverWatchRepos(workspaceDir)
	lease := SyntheticGuard.Acquire(repos)
	return &Emitter{
		ctx:       ctx,
		repos:     repos,
		lease:     lease,
		snapshots: CaptureSnapshots(ctx, repos),
		progress:  progress,
	}
}

func (e *Emitter) Emit() {
	if e == nil || e.progress == nil {
		return
	}
	if !SyntheticGuard.CanEmit(e.lease) {
		e.snapshots = CaptureSnapshots(e.ctx, e.repos)
		return
	}
	messages, nextSnapshots := CollectMessages(e.ctx, e.repos, e.snapshots)
	e.snapshots = nextSnapshots
	for _, message := range messages {
		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}
		e.progress(CallbackPrefix + message)
	}
}

func (e *Emitter) Close() {
	if e == nil {
		return
	}
	SyntheticGuard.Release(e.lease)
}

func DiscoverWatchRepos(workspaceDir string) []string {
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
		if abs, err := filepath.Abs(dir); err == nil {
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

func CaptureSnapshots(ctx context.Context, repos []string) map[string]Snapshot {
	snapshots := make(map[string]Snapshot, len(repos))
	for _, repo := range repos {
		snapshot, err := readRepoDiffSnapshot(ctx, repo)
		if err != nil {
			continue
		}
		snapshots[repo] = snapshot
	}
	return snapshots
}

func CollectMessages(ctx context.Context, repos []string, previous map[string]Snapshot) ([]string, map[string]Snapshot) {
	if previous == nil {
		previous = make(map[string]Snapshot, len(repos))
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
		for _, path := range diffSnapshotPaths(prior, current) {
			stat, ok := current[path]
			if !ok {
				continue
			}
			status := resolveFileChangeStatusByGit(ctx, repo, path)
			if status == FileChangeStatusUnknown {
				status = FileChangeStatusModified
			}
			messages = append(messages, FormatMessage(path, status, stat))
		}
		previous[repo] = current
	}
	return messages, previous
}

func FormatMessage(path string, status FileChangeStatus, stat FileDiffStat) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	label := statusLabel(status)
	if stat.Additions == 0 && stat.Deletions == 0 {
		return fmt.Sprintf("- `%s` %s", path, label)
	}
	return fmt.Sprintf("- `%s` %s (+%d/-%d)", path, label, stat.Additions, stat.Deletions)
}

func statusLabel(status FileChangeStatus) string {
	switch status {
	case FileChangeStatusAdded:
		return "已新增"
	case FileChangeStatusDeleted:
		return "已删除"
	default:
		return "已更改"
	}
}

func diffSnapshotPaths(previous, current Snapshot) []string {
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

func readRepoDiffSnapshot(ctx context.Context, repo string) (Snapshot, error) {
	snapshot := make(Snapshot)

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
		snapshot[path] = FileDiffStat{
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
			snapshot[path] = FileDiffStat{}
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
