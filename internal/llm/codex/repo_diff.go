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

func formatFileChangeMessage(path string, stat fileDiffStat) string {
	return formatFileChangeMessageWithStatus(path, fileChangeStatusModified, stat)
}

func formatFileChangeMessageWithStatus(path string, status fileChangeStatus, stat fileDiffStat) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	label := fileChangeStatusLabel(status)
	if stat.Additions == 0 && stat.Deletions == 0 {
		return fmt.Sprintf("- `%s` %s", path, label)
	}
	return fmt.Sprintf("- `%s` %s (+%d/-%d)", path, label, stat.Additions, stat.Deletions)
}

func fileChangeStatusLabel(status fileChangeStatus) string {
	switch status {
	case fileChangeStatusAdded:
		return "已新增"
	case fileChangeStatusDeleted:
		return "已删除"
	default:
		return "已更改"
	}
}

func normalizeFileChangeStatus(raw string) fileChangeStatus {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return fileChangeStatusUnknown
	}

	switch normalized {
	case "已更改", "更改", "修改", "updated", "update", "modified", "modify", "changed", "change", "edit", "edited", "rewrite", "rewritten":
		return fileChangeStatusModified
	case "已新增", "新增", "新建", "create", "created", "add", "added", "new", "created_file":
		return fileChangeStatusAdded
	case "已删除", "删除", "delete", "deleted", "remove", "removed", "rm", "unlink":
		return fileChangeStatusDeleted
	default:
		return fileChangeStatusUnknown
	}
}

func detectGitPathStatus(ctx context.Context, repo, relPath string) fileChangeStatus {
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "status", "--porcelain", "--", relPath)
	out, err := cmd.Output()
	if err != nil {
		return fileChangeStatusUnknown
	}

	for _, rawLine := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if len(line) < 2 {
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

		parsedLine, ok := parseFormattedFileChangeLine(line)
		if !ok {
			updated = append(updated, line)
			continue
		}

		status := parsedLine.Status
		if status == fileChangeStatusUnknown {
			status = fileChangeStatusModified
		}
		stat := parsedLine.Stat
		hasNumbers := stat.Additions != 0 || stat.Deletions != 0

		resolvedStatus, resolvedStat, resolved := resolveFileChangeInfoByGitDiff(ctx, repos, parsedLine.Path)
		if resolved {
			if status == fileChangeStatusModified &&
				(resolvedStatus == fileChangeStatusAdded || resolvedStatus == fileChangeStatusDeleted) {
				status = resolvedStatus
			}
			if !hasNumbers && (resolvedStat.Additions != 0 || resolvedStat.Deletions != 0) {
				stat = resolvedStat
				hasNumbers = true
			}
			if status == fileChangeStatusUnknown {
				status = resolvedStatus
			}
		}
		if status == fileChangeStatusUnknown {
			status = fileChangeStatusModified
		}

		if !hasNumbers {
			stat = fileDiffStat{}
		}
		updated = append(updated, formatFileChangeMessageWithStatus(parsedLine.Path, status, stat))
	}

	return strings.Join(updated, "\n")
}

func parseFormattedFileChangeLine(line string) (parsedFileChangeLine, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return parsedFileChangeLine{}, false
	}

	if parsed, ok := parseMarkdownFileChangeLine(line); ok {
		return parsed, true
	}
	if parsed, ok := parseLegacyFileChangeLine(line); ok {
		return parsed, true
	}
	return parsedFileChangeLine{}, false
}

func parseMarkdownFileChangeLine(line string) (parsedFileChangeLine, bool) {
	const prefix = "- `"
	if !strings.HasPrefix(line, prefix) {
		return parsedFileChangeLine{}, false
	}
	rest := strings.TrimPrefix(line, prefix)
	endPath := strings.Index(rest, "`")
	if endPath <= 0 {
		return parsedFileChangeLine{}, false
	}
	path := strings.TrimSpace(rest[:endPath])
	if path == "" {
		return parsedFileChangeLine{}, false
	}
	tail := strings.TrimSpace(rest[endPath+1:])
	if tail == "" {
		return parsedFileChangeLine{}, false
	}

	statusPart := tail
	stat := fileDiffStat{}
	hasStats := false
	if idx := strings.Index(tail, " (+"); idx >= 0 && strings.HasSuffix(tail, ")") {
		statusPart = strings.TrimSpace(tail[:idx])
		statsPart := strings.TrimSpace(tail[idx+2 : len(tail)-1])
		additions, deletions, ok := parseMarkdownStats(statsPart)
		if ok {
			stat = fileDiffStat{Additions: additions, Deletions: deletions}
			hasStats = true
		}
	}

	status := normalizeFileChangeStatus(statusPart)
	if status == fileChangeStatusUnknown {
		return parsedFileChangeLine{}, false
	}

	return parsedFileChangeLine{
		Path:     path,
		Status:   status,
		Stat:     stat,
		HasStats: hasStats,
	}, true
}

func parseMarkdownStats(statsPart string) (additions int, deletions int, ok bool) {
	statsPart = strings.TrimSpace(statsPart)
	if !strings.HasPrefix(statsPart, "+") {
		return 0, 0, false
	}
	trimmed := strings.TrimPrefix(statsPart, "+")
	parts := strings.SplitN(trimmed, "/-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	add, addErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	del, delErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if addErr != nil || delErr != nil {
		return 0, 0, false
	}
	return add, del, true
}

func parseLegacyFileChangeLine(line string) (parsedFileChangeLine, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return parsedFileChangeLine{}, false
	}

	legacyMarkers := []struct {
		Marker string
		Status fileChangeStatus
	}{
		{Marker: "已新增，+", Status: fileChangeStatusAdded},
		{Marker: "已删除，+", Status: fileChangeStatusDeleted},
		{Marker: "已更改，+", Status: fileChangeStatusModified},
	}
	for _, marker := range legacyMarkers {
		idx := strings.LastIndex(line, marker.Marker)
		if idx < 0 {
			continue
		}
		path := strings.TrimSpace(line[:idx])
		if path == "" {
			return parsedFileChangeLine{}, false
		}

		statsPart := strings.TrimSpace(line[idx+len(marker.Marker):])
		parts := strings.SplitN(statsPart, "-", 2)
		if len(parts) != 2 {
			return parsedFileChangeLine{}, false
		}
		add, addErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		del, delErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if addErr != nil || delErr != nil {
			return parsedFileChangeLine{}, false
		}

		return parsedFileChangeLine{
			Path:     path,
			Status:   marker.Status,
			Stat:     fileDiffStat{Additions: add, Deletions: del},
			HasStats: true,
		}, true
	}

	legacyNoStatMarkers := []struct {
		Suffix string
		Status fileChangeStatus
	}{
		{Suffix: "已新增", Status: fileChangeStatusAdded},
		{Suffix: "已删除", Status: fileChangeStatusDeleted},
		{Suffix: "已更改", Status: fileChangeStatusModified},
	}
	for _, marker := range legacyNoStatMarkers {
		if !strings.HasSuffix(line, marker.Suffix) {
			continue
		}
		path := strings.TrimSpace(strings.TrimSuffix(line, marker.Suffix))
		if path == "" {
			return parsedFileChangeLine{}, false
		}
		return parsedFileChangeLine{
			Path:   path,
			Status: marker.Status,
		}, true
	}

	return parsedFileChangeLine{}, false
}

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
