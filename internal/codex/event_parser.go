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
