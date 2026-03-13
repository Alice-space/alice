package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

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

func parseToolCallLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return ""
	}
	eventType, _ := event["type"].(string)
	if eventType != "item.completed" && eventType != "item.started" {
		return ""
	}
	item, ok := event["item"].(map[string]any)
	if !ok {
		return ""
	}
	itemType, _ := item["type"].(string)
	if itemType != "command_execution" {
		return ""
	}

	command := extractString(item, "command")
	status := extractString(item, "status")
	exitCode := extractInt(item, "exit_code")
	parts := make([]string, 0, 3)
	if command != "" {
		parts = append(parts, "command=`"+command+"`")
	}
	if status != "" {
		parts = append(parts, "status=`"+status+"`")
	}
	if eventType == "item.completed" {
		parts = append(parts, "exit_code=`"+strconv.Itoa(exitCode)+"`")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func parseFileChangeMessage(item map[string]any) string {
	if item == nil {
		return ""
	}

	entries := collectFileChangeEntries(item)
	if len(entries) == 0 {
		return ""
	}

	messages := make([]string, 0, len(entries))
	for _, entry := range entries {
		message := formatFileChangeMessageWithStatus(entry.Path, entry.Status, entry.Stat)
		if strings.TrimSpace(message) == "" {
			continue
		}
		messages = append(messages, message)
	}
	return strings.Join(messages, "\n")
}

type fileChangeEntry struct {
	Path     string
	Status   fileChangeStatus
	Stat     fileDiffStat
	HasStats bool
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

func collectFileChangeEntries(item map[string]any) []fileChangeEntry {
	if item == nil {
		return nil
	}

	defaultStatus := extractFileChangeStatus(item)
	defaultStat, defaultHasStats := extractDiffStat(item)
	entriesByPath := make(map[string]fileChangeEntry, 4)

	mergeEntry := func(rawPath string, status fileChangeStatus, stat fileDiffStat, hasStats bool) {
		path := normalizeFileChangePath(rawPath)
		if path == "" {
			return
		}
		if status == fileChangeStatusUnknown {
			status = defaultStatus
		}
		if status == fileChangeStatusUnknown {
			status = fileChangeStatusModified
		}
		if !hasStats && defaultHasStats {
			stat = defaultStat
			hasStats = true
		}

		incoming := fileChangeEntry{
			Path:     path,
			Status:   status,
			Stat:     stat,
			HasStats: hasStats,
		}

		existing, exists := entriesByPath[path]
		if !exists {
			entriesByPath[path] = incoming
			return
		}

		if existing.Status == fileChangeStatusUnknown {
			existing.Status = incoming.Status
		} else if existing.Status == fileChangeStatusModified &&
			(incoming.Status == fileChangeStatusAdded || incoming.Status == fileChangeStatusDeleted) {
			existing.Status = incoming.Status
		}

		existingHasNumbers := existing.Stat.Additions != 0 || existing.Stat.Deletions != 0
		incomingHasNumbers := incoming.Stat.Additions != 0 || incoming.Stat.Deletions != 0
		if !existing.HasStats && incoming.HasStats {
			existing.Stat = incoming.Stat
			existing.HasStats = true
		} else if incoming.HasStats && incomingHasNumbers && !existingHasNumbers {
			existing.Stat = incoming.Stat
			existing.HasStats = true
		}

		entriesByPath[path] = existing
	}

	mergeEntry(extractString(item, "path", "file_path", "filename", "file"), defaultStatus, defaultStat, defaultHasStats)

	if changed, ok := item["changed_file"].(map[string]any); ok {
		status := extractFileChangeStatus(changed)
		stat, hasStats := extractDiffStat(changed)
		mergeEntry(extractString(changed, "path", "file_path", "filename", "file"), status, stat, hasStats)
	}

	if changes, ok := item["changes"].([]any); ok {
		for _, change := range changes {
			entry, ok := change.(map[string]any)
			if !ok {
				continue
			}
			status := extractFileChangeStatus(entry)
			stat, hasStats := extractDiffStat(entry)
			mergeEntry(extractString(entry, "path", "file_path", "filename", "file"), status, stat, hasStats)
		}
	}

	if len(entriesByPath) == 0 {
		for _, path := range collectFileChangePaths(item) {
			mergeEntry(path, defaultStatus, defaultStat, defaultHasStats)
		}
	}

	paths := make([]string, 0, len(entriesByPath))
	for path := range entriesByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	entries := make([]fileChangeEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, entriesByPath[path])
	}
	return entries
}

func extractDiffStat(payload map[string]any) (fileDiffStat, bool) {
	if payload == nil {
		return fileDiffStat{}, false
	}

	additions, hasAdditions := extractIntWithPresence(payload, "added_lines", "additions", "added", "insertions", "plus")
	deletions, hasDeletions := extractIntWithPresence(payload, "removed_lines", "deletions", "removed", "minus")

	if stats, ok := payload["diff_stats"].(map[string]any); ok {
		if !hasAdditions {
			if value, found := extractIntWithPresence(stats, "added_lines", "additions", "added", "insertions", "plus"); found {
				additions = value
				hasAdditions = true
			}
		}
		if !hasDeletions {
			if value, found := extractIntWithPresence(stats, "removed_lines", "deletions", "removed", "minus"); found {
				deletions = value
				hasDeletions = true
			}
		}
	}

	if stats, ok := payload["stats"].(map[string]any); ok {
		if !hasAdditions {
			if value, found := extractIntWithPresence(stats, "added_lines", "additions", "added", "insertions", "plus"); found {
				additions = value
				hasAdditions = true
			}
		}
		if !hasDeletions {
			if value, found := extractIntWithPresence(stats, "removed_lines", "deletions", "removed", "minus"); found {
				deletions = value
				hasDeletions = true
			}
		}
	}

	return fileDiffStat{
		Additions: additions,
		Deletions: deletions,
	}, hasAdditions || hasDeletions
}

func extractIntWithPresence(payload map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			return int(v), true
		case float32:
			return int(v), true
		case int:
			return v, true
		case int64:
			return int(v), true
		case int32:
			return int(v), true
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				return 0, true
			}
			parsed, err := strconv.Atoi(trimmed)
			if err != nil {
				return 0, true
			}
			return parsed, true
		default:
			return 0, true
		}
	}
	return 0, false
}

func extractFileChangeStatus(payload map[string]any) fileChangeStatus {
	status := extractString(
		payload,
		"kind",
		"change_type",
		"change_kind",
		"change",
		"operation",
		"op",
		"action",
		"file_change_type",
		"status",
	)
	return normalizeFileChangeStatus(status)
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
