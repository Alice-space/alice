package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitee.com/alicespace/alice/internal/logging"
)

type Runner struct {
	Command      string
	Timeout      time.Duration
	Env          map[string]string
	PromptPrefix string
	WorkspaceDir string
}

const fileChangeCallbackPrefix = "[file_change] "

type fileDiffStat struct {
	Additions int
	Deletions int
}

type repoDiffSnapshot map[string]fileDiffStat

func (r Runner) Run(ctx context.Context, userText string) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", userText, nil)
	return reply, err
}

func (r Runner) RunWithProgress(
	ctx context.Context,
	userText string,
	onThinking func(step string),
) (string, error) {
	reply, _, err := r.RunWithThreadAndProgress(ctx, "", userText, onThinking)
	return reply, err
}

func (r Runner) RunWithThread(
	ctx context.Context,
	threadID string,
	userText string,
) (string, string, error) {
	return r.RunWithThreadAndProgress(ctx, threadID, userText, nil)
}

func (r Runner) RunWithThreadAndProgress(
	ctx context.Context,
	threadID string,
	userText string,
	onThinking func(step string),
) (string, string, error) {
	prompt := buildPrompt(threadID, r.PromptPrefix, userText)
	logging.Debugf(
		"codex prompt assemble thread_id=%s prefix=%q user_prompt=%q final_prompt=%q",
		threadID,
		r.PromptPrefix,
		userText,
		prompt,
	)
	if strings.TrimSpace(prompt) == "" {
		return "", "", errors.New("empty prompt")
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmdArgs := buildExecArgs(threadID, prompt)
	cmd := exec.CommandContext(tctx, r.Command, cmdArgs...)
	if strings.TrimSpace(r.WorkspaceDir) != "" {
		cmd.Dir = r.WorkspaceDir
	}
	cmd.Env = mergeEnv(os.Environ(), r.Env)
	logging.Debugf(
		"run codex command command=%q thread_id=%s args=%q cwd=%q timeout=%s",
		r.Command,
		threadID,
		cmdArgs,
		cmd.Dir,
		timeout,
	)
	watchedRepos := discoverWatchRepos(cmd.Dir)
	repoSnapshots := captureRepoSnapshots(tctx, watchedRepos)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("create stdout pipe failed: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("create stderr pipe failed: %w", err)
	}

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		logging.Debugf("codex start failed err=%v", err)
		return "", "", fmt.Errorf("start codex process failed: %w", err)
	}
	logging.Debugf("codex process started pid=%d", cmd.Process.Pid)

	var stderr bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderr, stderrPipe)
		close(stderrDone)
	}()

	var stdout bytes.Buffer
	var finalMessage string
	activeThreadID := strings.TrimSpace(threadID)
	sawNativeFileChange := false
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		stdout.WriteString(line)
		stdout.WriteByte('\n')
		logging.Debugf("codex stdout line=%s", line)

		reasoning, agentMessage, fileChangeMessage, parsedThreadID := parseEventLine(line)
		if strings.TrimSpace(parsedThreadID) != "" {
			activeThreadID = strings.TrimSpace(parsedThreadID)
			logging.Debugf("codex thread started thread_id=%s", activeThreadID)
		}
		if strings.TrimSpace(reasoning) != "" {
			logging.Debugf("codex reasoning=%q", strings.TrimSpace(reasoning))
		}
		if strings.TrimSpace(fileChangeMessage) != "" {
			resolvedFileChangeMessage := enrichFileChangeMessageStats(tctx, fileChangeMessage, watchedRepos)
			if strings.TrimSpace(resolvedFileChangeMessage) == "" {
				resolvedFileChangeMessage = strings.TrimSpace(fileChangeMessage)
			}
			sawNativeFileChange = true
			logging.Debugf("codex file_change=%q", strings.TrimSpace(resolvedFileChangeMessage))
			if onThinking != nil {
				onThinking(fileChangeCallbackPrefix + strings.TrimSpace(resolvedFileChangeMessage))
			}
		}
		if strings.TrimSpace(agentMessage) != "" {
			finalMessage = strings.TrimSpace(agentMessage)
			if onThinking != nil {
				onThinking(finalMessage)
			}
			logging.Debugf("codex agent_message=%q", finalMessage)
		}

		if onThinking != nil && !sawNativeFileChange && isSuccessfulCommandExecutionCompleted(line) {
			diffMessages, nextSnapshots := collectRepoDiffMessages(tctx, watchedRepos, repoSnapshots)
			repoSnapshots = nextSnapshots
			for _, message := range diffMessages {
				logging.Debugf("codex synthetic file_change=%q", strings.TrimSpace(message))
				onThinking(fileChangeCallbackPrefix + strings.TrimSpace(message))
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-stderrDone
		if errors.Is(tctx.Err(), context.DeadlineExceeded) {
			logging.Debugf("codex run timeout while scanning elapsed=%s", time.Since(startedAt))
			return "", activeThreadID, errors.New("codex timeout")
		}
		if errors.Is(tctx.Err(), context.Canceled) {
			logging.Debugf("codex run canceled while scanning elapsed=%s", time.Since(startedAt))
			return "", activeThreadID, context.Canceled
		}
		logging.Debugf("codex scan failed elapsed=%s err=%v", time.Since(startedAt), scanErr)
		return "", activeThreadID, fmt.Errorf("read codex output failed: %w", scanErr)
	}

	err = cmd.Wait()
	<-stderrDone
	stderrText := strings.TrimSpace(stderr.String())
	if stderrText != "" {
		logging.Debugf("codex stderr=%s", stderrText)
	}
	if errors.Is(tctx.Err(), context.DeadlineExceeded) {
		logging.Debugf("codex run timeout elapsed=%s", time.Since(startedAt))
		return "", activeThreadID, errors.New("codex timeout")
	}
	if errors.Is(tctx.Err(), context.Canceled) {
		logging.Debugf("codex run canceled elapsed=%s", time.Since(startedAt))
		return "", activeThreadID, context.Canceled
	}
	if err != nil {
		detail := stderrText
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 400 {
			detail = detail[:400]
		}
		logging.Debugf("codex run failed elapsed=%s err=%v detail=%s", time.Since(startedAt), err, detail)
		return "", activeThreadID, fmt.Errorf("codex exec failed: %w (%s)", err, detail)
	}

	if onThinking != nil && !sawNativeFileChange {
		diffMessages, nextSnapshots := collectRepoDiffMessages(tctx, watchedRepos, repoSnapshots)
		repoSnapshots = nextSnapshots
		for _, message := range diffMessages {
			logging.Debugf("codex synthetic file_change=%q", strings.TrimSpace(message))
			onThinking(fileChangeCallbackPrefix + strings.TrimSpace(message))
		}
	}
	_ = repoSnapshots

	if finalMessage == "" {
		message, parseErr := ParseFinalMessage(stdout.String())
		if parseErr != nil {
			logging.Debugf("codex final message parse failed elapsed=%s err=%v", time.Since(startedAt), parseErr)
			return "", activeThreadID, parseErr
		}
		finalMessage = strings.TrimSpace(message)
	}
	logging.Debugf(
		"codex run completed elapsed=%s thread_id=%s final_message=%q",
		time.Since(startedAt),
		activeThreadID,
		finalMessage,
	)
	return finalMessage, activeThreadID, nil
}

func ParseFinalMessage(jsonlOutput string) (string, error) {
	var lastMessage string
	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		_, text, _, _ := parseEventLine(line)
		if strings.TrimSpace(text) != "" {
			lastMessage = text
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(lastMessage) == "" {
		return "", errors.New("codex returned no final agent message")
	}
	return lastMessage, nil
}

func parseEventLine(line string) (reasoning string, agentMessage string, fileChangeMessage string, threadID string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", "", ""
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return "", "", "", ""
	}

	eventType, _ := event["type"].(string)
	if eventType == "thread.started" {
		id, _ := event["thread_id"].(string)
		return "", "", "", strings.TrimSpace(id)
	}
	if eventType != "item.completed" {
		return "", "", "", ""
	}

	item, ok := event["item"].(map[string]any)
	if !ok {
		return "", "", "", ""
	}
	itemType, _ := item["type"].(string)
	text, _ := item["text"].(string)
	switch itemType {
	case "reasoning":
		return text, "", "", ""
	case "agent_message":
		return "", text, "", ""
	case "file_change", "filechange":
		return "", "", parseFileChangeMessage(item), ""
	default:
		return "", "", "", ""
	}
}

func parseFileChangeMessage(item map[string]any) string {
	if item == nil {
		return ""
	}

	paths := collectFileChangePaths(item)
	if len(paths) == 0 {
		return ""
	}

	additions := extractInt(item, "added_lines", "additions", "added", "insertions", "plus")
	deletions := extractInt(item, "removed_lines", "deletions", "removed", "minus")
	if stats, ok := item["diff_stats"].(map[string]any); ok {
		if additions == 0 {
			additions = extractInt(stats, "added_lines", "additions", "added", "insertions", "plus")
		}
		if deletions == 0 {
			deletions = extractInt(stats, "removed_lines", "deletions", "removed", "minus")
		}
	}

	messages := make([]string, 0, len(paths))
	for _, path := range paths {
		normalizedPath := normalizeFileChangePath(path)
		if normalizedPath == "" {
			continue
		}
		messages = append(messages, formatFileChangeMessage(normalizedPath, fileDiffStat{
			Additions: additions,
			Deletions: deletions,
		}))
	}
	return strings.Join(messages, "\n")
}

func extractString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			trimmed := strings.TrimSpace(text)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func extractInt(payload map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			return int(v)
		case float32:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		case int32:
			return int(v)
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				continue
			}
			parsed, err := strconv.Atoi(trimmed)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func isSuccessfulCommandExecutionCompleted(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return false
	}
	eventType, _ := event["type"].(string)
	if eventType != "item.completed" {
		return false
	}
	item, ok := event["item"].(map[string]any)
	if !ok {
		return false
	}
	itemType, _ := item["type"].(string)
	if itemType != "command_execution" {
		return false
	}
	status, _ := item["status"].(string)
	if strings.TrimSpace(status) != "" && strings.TrimSpace(status) != "completed" {
		return false
	}

	exitCode := 0
	switch v := item["exit_code"].(type) {
	case float64:
		exitCode = int(v)
	case float32:
		exitCode = int(v)
	case int:
		exitCode = v
	case int64:
		exitCode = int(v)
	case int32:
		exitCode = int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			exitCode = parsed
		}
	}
	return exitCode == 0
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

func collectFileChangePaths(item map[string]any) []string {
	if item == nil {
		return nil
	}

	seen := make(map[string]struct{}, 4)
	addPath := func(raw string) {
		path := strings.TrimSpace(raw)
		if path == "" {
			return
		}
		seen[path] = struct{}{}
	}

	addPath(extractString(item, "path", "file_path", "filename", "file"))
	if changed, ok := item["changed_file"].(map[string]any); ok {
		addPath(extractString(changed, "path", "file_path", "filename", "file"))
	}
	if changes, ok := item["changes"].([]any); ok {
		for _, change := range changes {
			entry, ok := change.(map[string]any)
			if !ok {
				continue
			}
			addPath(extractString(entry, "path", "file_path", "filename", "file"))
		}
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func normalizeFileChangePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(path)
	path = strings.TrimPrefix(path, "./")
	const aliceRepoPrefix = "/home/codexbot/alice/"
	path = strings.TrimPrefix(path, aliceRepoPrefix)
	return strings.TrimSpace(path)
}

func buildExecArgs(threadID string, prompt string) []string {
	threadID = strings.TrimSpace(threadID)
	if threadID != "" {
		return []string{
			"exec",
			"resume",
			"--json",
			"--skip-git-repo-check",
			"--dangerously-bypass-approvals-and-sandbox",
			"--",
			threadID,
			prompt,
		}
	}
	return []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"--",
		prompt,
	}
}

func buildPrompt(threadID string, promptPrefix string, userText string) string {
	trimmedThreadID := strings.TrimSpace(threadID)
	trimmedPrefix := strings.TrimSpace(promptPrefix)
	trimmedUserText := strings.TrimSpace(userText)
	if trimmedThreadID != "" {
		return trimmedUserText
	}
	if trimmedPrefix == "" {
		return trimmedUserText
	}
	return trimmedPrefix + "\n\n" + trimmedUserText
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	env := make([]string, len(base))
	copy(env, base)

	indexByKey := make(map[string]int, len(env))
	for i, item := range env {
		key := envKey(item)
		if key == "" {
			continue
		}
		indexByKey[key] = i
	}

	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		pair := key + "=" + overrides[key]
		if idx, ok := indexByKey[key]; ok {
			env[idx] = pair
			continue
		}
		env = append(env, pair)
	}
	return env
}

func envKey(item string) string {
	idx := strings.Index(item, "=")
	if idx <= 0 {
		return ""
	}
	return item[:idx]
}
