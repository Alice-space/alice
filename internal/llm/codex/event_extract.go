package codex

import (
	"encoding/json"
	"strconv"
	"strings"
)

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
