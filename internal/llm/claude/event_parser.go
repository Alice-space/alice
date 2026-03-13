package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"
)

type parsedEvent struct {
	SessionID      string
	AssistantText  string
	ToolCall       string
	ResultText     string
	ResultErrors   []string
	ResultIsError  bool
	HasResultEvent bool
}

func ParseFinalMessage(jsonlOutput string) (string, error) {
	var lastAssistant string
	var lastResult string

	scanner := bufio.NewScanner(strings.NewReader(jsonlOutput))
	scanner.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)

	for scanner.Scan() {
		event := parseEventLine(scanner.Text())
		if strings.TrimSpace(event.AssistantText) != "" {
			lastAssistant = strings.TrimSpace(event.AssistantText)
		}
		if strings.TrimSpace(event.ResultText) != "" {
			lastResult = strings.TrimSpace(event.ResultText)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(lastAssistant) != "" {
		return lastAssistant, nil
	}
	if strings.TrimSpace(lastResult) != "" {
		return lastResult, nil
	}
	return "", errors.New("claude returned no final assistant message")
}

func parseEventLine(line string) parsedEvent {
	line = strings.TrimSpace(line)
	if line == "" {
		return parsedEvent{}
	}

	var event map[string]any
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return parsedEvent{}
	}

	eventType := extractString(event, "type")
	sessionID := extractString(event, "session_id")

	switch eventType {
	case "system":
		subtype := strings.ToLower(extractString(event, "subtype"))
		if subtype == "init" {
			return parsedEvent{SessionID: sessionID}
		}
		return parsedEvent{}
	case "assistant":
		message, _ := event["message"].(map[string]any)
		return parsedEvent{
			SessionID:     sessionID,
			AssistantText: extractAssistantText(message),
			ToolCall:      extractAssistantToolUse(message),
		}
	case "result":
		resultText := extractString(event, "result")
		resultErrors := extractStringSlice(event, "errors")
		resultIsError := extractBool(event["is_error"])
		if strings.TrimSpace(resultText) == "" && len(resultErrors) > 0 {
			resultText = strings.Join(resultErrors, "\n")
		}
		return parsedEvent{
			SessionID:      sessionID,
			ResultText:     strings.TrimSpace(resultText),
			ResultErrors:   resultErrors,
			ResultIsError:  resultIsError,
			HasResultEvent: true,
		}
	default:
		return parsedEvent{}
	}
}

func extractAssistantToolUse(message map[string]any) string {
	if len(message) == 0 {
		return ""
	}
	content, ok := message["content"].([]any)
	if !ok {
		return ""
	}
	tools := make([]string, 0, len(content))
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.ToLower(extractString(block, "type")) != "tool_use" {
			continue
		}
		name := extractString(block, "name")
		id := extractString(block, "id")
		line := "tool_use"
		if name != "" {
			line += " name=`" + name + "`"
		}
		if id != "" {
			line += " id=`" + id + "`"
		}
		tools = append(tools, line)
	}
	return strings.Join(tools, "; ")
}

func extractAssistantText(message map[string]any) string {
	if len(message) == 0 {
		return ""
	}
	content, ok := message["content"].([]any)
	if !ok {
		return extractString(message, "text")
	}
	parts := make([]string, 0, len(content))
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.ToLower(extractString(block, "type")) != "text" {
			continue
		}
		text := strings.TrimSpace(extractString(block, "text"))
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func extractString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func extractStringSlice(payload map[string]any, key string) []string {
	raw, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func extractBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		return lower == "true" || lower == "1" || lower == "yes"
	default:
		return false
	}
}
